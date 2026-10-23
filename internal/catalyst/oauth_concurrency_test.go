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

func awaitRefreshWaiters(t *testing.T, c *Client, count int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		c.mu.Lock()
		ready := c.refresh != nil && c.refresh.waiters == count
		c.mu.Unlock()
		if ready {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("callers did not join shared refresh")
		case <-tick.C:
		}
	}
}

func tokenResponse(token string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"` + token + `","expires_in":3600}`))}
}

func TestRefreshCallerCancellationIsIndependent(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "initiator", false: "follower"}[cancelFirst], func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var calls atomic.Int32
			c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				select {
				case <-release:
					return tokenResponse("fresh"), nil
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			firstCtx, cancel1 := context.WithCancel(t.Context())
			defer cancel1()
			secondCtx, cancel2 := context.WithCancel(t.Context())
			defer cancel2()
			first, second := make(chan error, 1), make(chan error, 1)
			go func() { _, err := c.accessToken(firstCtx); first <- err }()
			awaitRefreshWaiters(t, c, 1)
			go func() { _, err := c.accessToken(secondCtx); second <- err }()
			awaitRefreshWaiters(t, c, 2)
			canceled, active := second, first
			if cancelFirst {
				cancel1()
				canceled, active = first, second
			} else {
				cancel2()
			}
			select {
			case err := <-canceled:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("wrong cancellation: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled caller blocked behind refresh")
			}
			select {
			case err := <-active:
				t.Fatalf("active caller interrupted: %v", err)
			default:
			}
			unblock()
			if err := <-active; err != nil {
				t.Fatal(err)
			}
			token, err := c.accessToken(t.Context())
			if err != nil || token != "fresh" || calls.Load() != 1 {
				t.Fatalf("refresh not shared and cached: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestAbandonedRefreshCannotOverwriteNewToken(t *testing.T) {
	canceled, releaseOld := make(chan struct{}), make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(releaseOld) }) }
	defer unblock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			<-releaseOld
			return tokenResponse("stale"), nil
		}
		return tokenResponse("current"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(ctx); done <- err }()
	awaitRefreshWaiters(t, c, 1)
	c.mu.Lock()
	old := c.refresh
	c.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh transport did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("last caller did not cancel HTTP refresh")
	}
	token, err := c.accessToken(t.Context())
	if err != nil || token != "current" {
		t.Fatalf("new refresh failed: %v", err)
	}
	unblock()
	<-old.done
	token, err = c.accessToken(t.Context())
	if err != nil || token != "current" || calls.Load() != 2 {
		t.Fatal("abandoned refresh overwrote the token cache")
	}
}

func TestRefreshFailureSharedAndRecoverable(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	c, err := New(refreshConfig(), &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			<-release
			return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("secret-body"))}, nil
		}
		return tokenResponse("recovered"), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 12)
	for range 12 {
		go func() { _, err := c.accessToken(t.Context()); done <- err }()
	}
	awaitRefreshWaiters(t, c, 12)
	unblock()
	for range 12 {
		if err := <-done; err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unexpected refresh failure: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent failed refresh was repeated")
	}
	token, err := c.accessToken(t.Context())
	if err != nil || token != "recovered" || calls.Load() != 2 {
		t.Fatal("later request could not recover")
	}
}

func TestSharedRefreshHasDeadline(t *testing.T) {
	c, err := New(refreshConfig(), &http.Client{Timeout: 20 * time.Millisecond, Transport: retryTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.accessToken(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh did not respect deadline: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refresh != nil || c.token != "" {
		t.Fatal("failed refresh retained active state or a token")
	}
}
