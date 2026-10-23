// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type oauthTestClock struct{ value atomic.Pointer[time.Time] }

func newOAuthTestClock() *oauthTestClock {
	c := &oauthTestClock{}
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	c.value.Store(&now)
	return c
}

func (c *oauthTestClock) Now() time.Time { return *c.value.Load() }

func (c *oauthTestClock) Advance(delay time.Duration) {
	now := c.Now().Add(delay)
	c.value.Store(&now)
}

func TestOAuthCooldownSurvivesFailedRefresh(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		wantCalls    int32
		minimum      time.Duration
	}{
		{"seconds", "31", 1, 31 * time.Second},
		{"date", "Sat, 24 Oct 2026 01:00:00 GMT", 1, time.Hour},
		{"overflow", "999999999999999999999999", 1, time.Duration(1<<63 - 1)},
		{"duration-overflow", "18446744073709551615", 1, time.Duration(1<<63 - 1)},
		{"exhausted", "", 4, 2 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newOAuthTestClock()
			var calls atomic.Int32
			c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return oauthTestResponse(429, "secret-body", tc.header), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			c.now = clock.Now
			c.wait = func(context.Context, time.Duration) error { return nil }
			if _, err := c.accessToken(t.Context()); err == nil {
				t.Fatal("throttled refresh succeeded")
			}
			c.mu.Lock()
			until := c.cooldown.until
			c.mu.Unlock()
			if until.Sub(clock.Now()) < tc.minimum {
				t.Fatalf("server cooldown was shortened: %s", until.Sub(clock.Now()))
			}
			var wg sync.WaitGroup
			for range 32 {
				wg.Go(func() {
					_, err := c.accessToken(t.Context())
					if err == nil || !strings.Contains(err.Error(), "cooling down") || strings.Contains(err.Error(), "secret") {
						t.Errorf("unsafe cooldown result: %v", err)
					}
				})
			}
			wg.Wait()
			if calls.Load() != tc.wantCalls {
				t.Fatalf("new callers bypassed cooldown: calls=%d", calls.Load())
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := c.accessToken(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("cooldown hid caller cancellation: %v", err)
			}
		})
	}
}

func TestOAuthCooldownExpirySharesRecoveryAndClearsState(t *testing.T) {
	clock := newOAuthTestClock()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		switch calls.Add(1) {
		case 1:
			return oauthTestResponse(503, "secret-body", "31"), nil
		case 2:
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return tokenResponse("recovered"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.now = clock.Now
	if _, err := c.accessToken(t.Context()); err == nil {
		t.Fatal("expected initial failure")
	}
	clock.Advance(31 * time.Second)
	done := make(chan error, 12)
	for range 12 {
		go func() { _, err := c.accessToken(t.Context()); done <- err }()
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not resume at deadline")
	}
	awaitRefreshWaiters(t, c, 12)
	unblock()
	for range 12 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	cleared := c.cooldown.until.IsZero() && c.cooldown.err == nil
	c.mu.Unlock()
	if !cleared || calls.Load() != 2 {
		t.Fatal("successful recovery did not clear cooldown or share refresh")
	}
	c.invalidate("recovered")
	if _, err := c.accessToken(t.Context()); err != nil || calls.Load() != 3 {
		t.Fatalf("old cooldown blocked a later refresh: %v", err)
	}
}

func TestOAuthCooldownPersistsWhenAllCallersCancelBackoff(t *testing.T) {
	clock := newOAuthTestClock()
	waiting := make(chan struct{})
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return oauthTestResponse(429, "secret-body", "20"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.now = clock.Now
	c.wait = func(ctx context.Context, delay time.Duration) error { close(waiting); return waitContext(ctx, delay) }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(ctx); done <- err }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not enter backoff")
	}
	c.mu.Lock()
	flight := c.refresh
	c.mu.Unlock()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-flight.done
	if _, err := c.accessToken(t.Context()); err == nil || !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("abandoned backoff lost cooldown: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("abandoned refresh was replayed before retry deadline")
	}
}

func TestOAuthCooldownPersistsAfterRefreshDeadline(t *testing.T) {
	clock := newOAuthTestClock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Timeout: 20 * time.Millisecond, Transport: retryTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return oauthTestResponse(429, "secret-body", "20"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.now = clock.Now
	if _, err := c.accessToken(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shared deadline lost: %v", err)
	}
	if _, err := c.accessToken(t.Context()); err == nil || !strings.Contains(err.Error(), "cooling down") || calls.Load() != 1 {
		t.Fatalf("deadline cleared server cooldown: %v calls=%d", err, calls.Load())
	}
}

func TestOAuthStaleRefreshCannotInstallCooldown(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return oauthTestResponse(429, "secret-body", "3600"), nil
		}
		return tokenResponse("fresh"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not start")
	}
	c.mu.Lock()
	old := c.refresh
	c.mu.Unlock()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.accessToken(t.Context()); err != nil {
		t.Fatal(err)
	}
	unblock()
	<-old.done
	c.mu.Lock()
	clear := c.cooldown.until.IsZero()
	c.mu.Unlock()
	if !clear {
		t.Fatal("late response throttled a newer refresh")
	}
	c.invalidate("fresh")
	if _, err := c.accessToken(t.Context()); err != nil || calls.Load() != 3 {
		t.Fatalf("stale response blocked future refresh: %v", err)
	}
}

func TestOAuthCooldownIsPerClient(t *testing.T) {
	clock := newOAuthTestClock()
	var calls atomic.Int32
	transport := retryTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		if r.PostForm.Get("client_id") == "throttled" {
			return oauthTestResponse(429, "secret-body", "31"), nil
		}
		return tokenResponse("fresh"), nil
	})
	config := refreshConfig()
	config.ClientID = "throttled"
	a, err := New(config, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(refreshConfig(), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	a.now, b.now = clock.Now, clock.Now
	if _, err := a.accessToken(t.Context()); err == nil {
		t.Fatal("expected throttling")
	}
	if token, err := b.accessToken(t.Context()); err != nil || token != "fresh" {
		t.Fatalf("one alias throttled another: %v", err)
	}
	if _, err := a.accessToken(t.Context()); err == nil || calls.Load() != 2 {
		t.Fatal("cooldown isolation failed")
	}
}

func TestOAuthCooldownKeepsActiveRefreshJoinable(t *testing.T) {
	clock := newOAuthTestClock()
	waiting, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return oauthTestResponse(429, "secret-body", "1"), nil
		}
		return tokenResponse("fresh"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.now = clock.Now
	c.wait = func(ctx context.Context, delay time.Duration) error {
		close(waiting)
		select {
		case <-release:
			clock.Advance(delay)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first, follower := make(chan error, 1), make(chan error, 1)
	go func() { _, err := c.accessToken(ctx); first <- err }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not enter backoff")
	}
	go func() { _, err := c.accessToken(t.Context()); follower <- err }()
	awaitRefreshWaiters(t, c, 2)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case err := <-follower:
		t.Fatalf("active follower was rejected during shared backoff: %v", err)
	default:
	}
	unblock()
	if err := <-follower; err != nil {
		t.Fatal(err)
	}
	if token, err := c.accessToken(t.Context()); err != nil || token != "fresh" || calls.Load() != 2 {
		t.Fatalf("shared retry failed after one caller cancelled: %v calls=%d", err, calls.Load())
	}
}

func TestOAuthAbandonedResponsePreservesCooldownWithoutNewerRefresh(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release // Model a transport returning a response after cancellation.
		return oauthTestResponse(429, "secret-body", "31"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not start")
	}
	c.mu.Lock()
	flight := c.refresh
	c.mu.Unlock()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unblock()
	<-flight.done
	if _, err := c.accessToken(t.Context()); err == nil || !strings.Contains(err.Error(), "cooling down") || calls.Load() != 1 {
		t.Fatalf("last abandoned response lost its cooldown: %v calls=%d", err, calls.Load())
	}
}
