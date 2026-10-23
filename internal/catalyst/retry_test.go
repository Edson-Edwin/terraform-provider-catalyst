// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReadRetryPolicy(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504, 400, 401, 403, 404, 409} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls, waits := 0, 0
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls < 3 {
					w.WriteHeader(status)
					return
				}
				writeResponse(t, w, projectJSON)
			})
			c.wait = func(ctx context.Context, d time.Duration) error { waits++; return nil }
			_, err := c.GetProject(t.Context(), projectID)
			retry := retryableReadStatus(status)
			if retry && (err != nil || calls != 3 || waits != 2) {
				t.Fatalf("read did not recover: calls=%d waits=%d err=%v", calls, waits, err)
			}
			if !retry && (err == nil || calls != 1 || waits != 0) {
				t.Fatal("permanent failure retried")
			}
		})
	}
}

func TestReadRetriesAreBounded(t *testing.T) {
	calls, waits := 0, 0
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) })
	c.wait = func(ctx context.Context, d time.Duration) error { waits++; return nil }
	if _, err := c.GetProject(t.Context(), projectID); err == nil || calls != 4 || waits != 3 {
		t.Fatalf("unbounded retries: calls=%d waits=%d err=%v", calls, waits, err)
	}
}

func TestReadRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		header  string
		minimum time.Duration
		retry   bool
	}{
		{"2", 2 * time.Second, true}, {now.Add(5 * time.Second).Format(http.TimeFormat), 5 * time.Second, true},
		{"31", 0, false}, {now.Add(time.Minute).Format(http.TimeFormat), 0, false},
		{"999999999999999999999999", 0, false}, {"invalid", 250 * time.Millisecond, true}, {"-1", 250 * time.Millisecond, true}, {"0", 250 * time.Millisecond, true},
	} {
		delay, retry := readRetryDelay(0, tc.header, now)
		if retry != tc.retry || (retry && delay < tc.minimum) {
			t.Fatalf("wrong Retry-After handling for %q: %s %v", tc.header, delay, retry)
		}
	}
	for attempt := 0; attempt < maxReadRetries; attempt++ {
		delay, _ := readRetryDelay(attempt, "", now)
		base := 500 * time.Millisecond * time.Duration(1<<attempt)
		if delay < base/2 || delay >= base {
			t.Fatalf("backoff outside jitter range: %s", delay)
		}
	}
	calls := 0
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "31")
		w.WriteHeader(429)
	})
	c.wait = func(ctx context.Context, d time.Duration) error {
		t.Fatal("long server delay retried early")
		return nil
	}
	if _, err := c.GetProject(t.Context(), projectID); err == nil || calls != 1 {
		t.Fatal("long Retry-After not respected")
	}
}

func TestReadRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) })
	c.wait = func(ctx context.Context, d time.Duration) error { cancel(); return waitContext(ctx, d) }
	if _, err := c.GetProject(ctx, projectID); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancellation lost: %v calls=%d", err, calls)
	}
}

type retryTransport func(*http.Request) (*http.Response, error)

func (f retryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReadTransportFailureAndWriteSafety(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		for _, transportFailure := range []bool{true, false} {
			t.Run(method+"/"+map[bool]string{true: "transport", false: "server"}[transportFailure], func(t *testing.T) {
				calls := 0
				c, err := New(Config{AccessToken: "secret"}, &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if transportFailure {
						return nil, errors.New("secret-transport-url")
					}
					return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("secret-response"))}, nil
				})})
				if err != nil {
					t.Fatal(err)
				}
				c.wait = func(ctx context.Context, d time.Duration) error { return nil }
				res, err := c.request(t.Context(), method, "/test", nil)
				if res != nil {
					_ = res.Body.Close()
				}
				expected := 1
				if method == http.MethodGet {
					expected = 4
				}
				if calls != expected {
					t.Fatalf("wrong retry count: %d", calls)
				}
				if err != nil && strings.Contains(err.Error(), "secret") {
					t.Fatal("transport secret leaked")
				}
			})
		}
	}
}

func TestSafeAPIErrorDetails(t *testing.T) {
	for _, code := range []string{"INVALID_ID", "PROJECT_CREATION_NOT_ALLOWED", "secret-code"} {
		res := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"status":"failure","data":{"error_code":"` + code + `","message":"secret-body"}}`))}
		err := apiResponseError("test", res)
		_ = res.Body.Close()
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("untrusted detail leaked")
		}
		if code != "secret-code" && !strings.Contains(err.Error(), code) {
			t.Fatal("known error code omitted")
		}
	}
}

func TestReadRefreshAndTransientRetries(t *testing.T) {
	reads, refreshes, waits := 0, 0, 0
	c := testClient(t, refreshConfig(), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/v2/token" {
			refreshes++
			writeResponse(t, w, `{"access_token":"fresh","expires_in":3600}`)
			return
		}
		reads++
		switch reads {
		case 1, 3:
			w.WriteHeader(503)
		case 2:
			w.WriteHeader(401)
		default:
			writeResponse(t, w, projectJSON)
		}
	})
	c.wait = func(context.Context, time.Duration) error { waits++; return nil }
	if _, err := c.GetProject(t.Context(), projectID); err != nil {
		t.Fatal(err)
	}
	if reads != 4 || refreshes != 2 || waits != 2 {
		t.Fatalf("retry budgets interfered: reads=%d refreshes=%d waits=%d", reads, refreshes, waits)
	}
}
