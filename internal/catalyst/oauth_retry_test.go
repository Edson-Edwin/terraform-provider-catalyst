// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func oauthTestResponse(status int, body, retryAfter string) *http.Response {
	h := make(http.Header)
	if retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestOAuthRetryPolicy(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504, 400, 401, 403, 404, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls, waits := 0, 0
			c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" {
					t.Error("OAuth credentials must stay in the POST body")
				}
				if err := r.ParseForm(); err != nil || r.PostForm.Get("refresh_token") != "refresh" {
					t.Error("retry did not reconstruct the form body")
				}
				if calls <= 2 {
					return oauthTestResponse(status, "secret-response", "2"), nil
				}
				return tokenResponse("fresh"), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			c.wait = func(ctx context.Context, delay time.Duration) error {
				waits++
				if delay < 2*time.Second {
					t.Error("OAuth retry ignored Retry-After")
				}
				return nil
			}
			token, err := c.accessToken(t.Context())
			if retryableReadStatus(status) {
				if err != nil || token != "fresh" || calls != 3 || waits != 2 {
					t.Fatalf("refresh failed to recover: calls=%d waits=%d err=%v", calls, waits, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "secret") || calls != 1 || waits != 0 {
				t.Fatalf("permanent response was retried or exposed: calls=%d waits=%d err=%v", calls, waits, err)
			}
		})
	}
}

func TestOAuthRetryLimits(t *testing.T) {
	for _, tc := range []struct {
		name, retryAfter string
		wantCalls        int
	}{
		{"bounded", "", 4},
		{"long-delay", "31", 1},
		{"overflow-delay", "999999999999999999999999", 1},
		{"future-date", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return oauthTestResponse(429, "secret-response", tc.retryAfter), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			c.wait = func(context.Context, time.Duration) error { return nil }
			if _, err := c.accessToken(t.Context()); err == nil || strings.Contains(err.Error(), "secret") || calls != tc.wantCalls {
				t.Fatalf("invalid retry limit: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestOAuthDoesNotReplayAmbiguousOrInvalidResponses(t *testing.T) {
	for _, body := range []string{"transport", `{"error":"invalid_grant"}`, `{"error":"secret-error"}`, `{"access_token":"secret-token","expires_in":0}`, "invalid-json"} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if body == "transport" {
					return nil, errors.New("secret-transport")
				}
				return oauthTestResponse(200, body, ""), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			c.wait = func(context.Context, time.Duration) error { t.Error("unsafe OAuth retry"); return nil }
			if _, err := c.accessToken(t.Context()); err == nil || strings.Contains(err.Error(), "secret") || calls != 1 {
				t.Fatalf("invalid OAuth failure: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestOAuthRetrySharedByConcurrentCallers(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls, waits atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			<-release
			return oauthTestResponse(503, "secret-response", ""), nil
		}
		return tokenResponse("fresh"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.wait = func(context.Context, time.Duration) error { waits.Add(1); return nil }
	done := make(chan error, 12)
	for range 12 {
		go func() { _, err := c.accessToken(t.Context()); done <- err }()
	}
	awaitRefreshWaiters(t, c, 12)
	unblock()
	for range 12 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 || waits.Load() != 1 {
		t.Fatal("callers did not share one retry sequence")
	}
}

func TestOAuthBackoffCancellation(t *testing.T) {
	waiting := make(chan struct{})
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return oauthTestResponse(429, "secret-response", "30"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.wait = func(ctx context.Context, delay time.Duration) error {
		close(waiting)
		return waitContext(ctx, delay)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(ctx); done <- err }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not enter backoff")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation blocked during backoff")
	}
	if calls.Load() != 1 {
		t.Fatal("refresh continued after cancellation")
	}
}
