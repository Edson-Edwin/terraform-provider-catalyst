// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxReadRetries = 3
const maxRetryAfter = 30 * time.Second

func retryableReadStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// readRetryDelay uses exponential backoff with jitter, never retrying before Retry-After.
// Long server delays are returned to the caller as errors instead of retrying early.
func readRetryDelay(attempt int, retryAfter string, now time.Time) (time.Duration, bool) {
	base := 500 * time.Millisecond * time.Duration(1<<attempt)
	delay := base/2 + time.Duration(rand.Int64N(int64(base/2)))
	value := strings.TrimSpace(retryAfter)
	var serverDelay time.Duration
	if value != "" {
		if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
			if seconds > uint64(maxRetryAfter/time.Second) {
				return 0, false
			}
			serverDelay = time.Duration(seconds) * time.Second
		} else if at, err := http.ParseTime(value); err == nil {
			serverDelay = at.Sub(now)
		} else if strings.Trim(value, "0123456789") == "" {
			// A positive integer too large for uint64 must not fall back to an early retry.
			return 0, false
		}
	}
	if serverDelay > maxRetryAfter {
		return 0, false
	}
	return max(delay, serverDelay), true
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
