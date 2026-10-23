// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func validateCredentials(config Config) error {
	hasRefresh := config.ClientID != "" || config.ClientSecret != "" || config.RefreshToken != "" || config.RedirectURI != ""
	if config.AccessToken != "" && hasRefresh {
		return errors.New("use access_token OR client_id, client_secret and refresh_token; unset the unused credentials, including their CATALYST_ environment variables")
	}
	if config.AccessToken == "" && (config.ClientID == "" || config.ClientSecret == "" || config.RefreshToken == "") {
		return errors.New("set CATALYST_ACCESS_TOKEN, or all of CATALYST_CLIENT_ID, CATALYST_CLIENT_SECRET and CATALYST_REFRESH_TOKEN (equivalent provider attributes are also accepted)")
	}
	for _, value := range []string{config.AccessToken, config.ClientID, config.ClientSecret, config.RefreshToken} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("credentials must not contain line breaks")
		}
	}
	if config.RedirectURI != "" {
		uri, err := url.Parse(config.RedirectURI)
		if err != nil || uri.Host == "" || (uri.Scheme != "https" && uri.Scheme != "http") {
			return errors.New("redirect_uri must be an absolute HTTP or HTTPS URL")
		}
	}
	return nil
}

type oauthToken struct {
	value     string
	expiresAt time.Time
}

type oauthAttemptResult struct {
	token      oauthToken
	retryAfter string
	retryable  bool
}

// Protected by Client.mu. The safe HTTP error never includes server bodies,
// credentials or raw Retry-After values.
type oauthCooldown struct {
	until time.Time
	err   error
}

// tokenRefresh is shared by callers but has a cancellation lifetime independent of any one caller.
// Mutable fields are protected by Client.mu; results are read only after done closes.
type tokenRefresh struct {
	generation uint64
	done       chan struct{}
	cancel     context.CancelFunc
	waiters    int
	token      string
	err        error
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.config.AccessToken != "" {
		return c.config.AccessToken, nil
	}
	c.mu.Lock()
	if c.token != "" && c.now().Before(c.expiresAt) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	flight := c.refresh
	if flight == nil {
		if c.now().Before(c.cooldown.until) {
			err := fmt.Errorf("OAuth refresh is cooling down until %s: %w", c.cooldown.until.UTC().Format(time.RFC3339), c.cooldown.err)
			c.mu.Unlock()
			return "", err
		}
		// A single caller leaving must not cancel refresh for other active callers.
		// The HTTP deadline bounds refresh even if all callers remain active.
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.http.Timeout)
		c.refreshGeneration++
		flight = &tokenRefresh{generation: c.refreshGeneration, done: make(chan struct{}), cancel: cancel}
		c.refresh = flight
		go c.runRefresh(refreshCtx, flight)
	}
	flight.waiters++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		flight.waiters--
		if flight.waiters == 0 && c.refresh == flight {
			c.refresh = nil
			flight.cancel()
		}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-flight.done:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return flight.token, flight.err
	}
}

func (c *Client) runRefresh(ctx context.Context, flight *tokenRefresh) {
	defer flight.cancel()
	token, err := c.fetchToken(ctx, flight)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// An abandoned request must not overwrite a newer refresh or populate the cache.
	if c.refresh == flight {
		if err == nil {
			c.token, c.expiresAt = token.value, token.expiresAt
			c.cooldown = oauthCooldown{}
		}
		c.refresh = nil
	}
	flight.token, flight.err = token.value, err
	close(flight.done)
}

func (c *Client) fetchToken(ctx context.Context, flight *tokenRefresh) (oauthToken, error) {
	form := url.Values{
		"grant_type": {"refresh_token"}, "client_id": {c.config.ClientID},
		"client_secret": {c.config.ClientSecret}, "refresh_token": {c.config.RefreshToken},
	}
	if c.config.RedirectURI != "" {
		form.Set("redirect_uri", c.config.RedirectURI)
	}
	for attempt := 0; ; attempt++ {
		result, err := c.fetchTokenAttempt(ctx, form)
		if !result.retryable {
			return result.token, err
		}
		now := c.now()
		delay, allowed := readRetryDelay(attempt, result.retryAfter, now)
		if !allowed {
			delay = retryAfterDelay(result.retryAfter, now)
		}
		// Record the delay as soon as it is observed, not only at flight
		// completion: all callers may cancel while backoff is in progress.
		c.recordOAuthCooldown(flight, now.Add(delay), err)
		if !allowed || attempt >= maxOAuthRetries {
			return oauthToken{}, err
		}
		if err := c.wait(ctx, delay); err != nil {
			return oauthToken{}, requestError(ctx, "OAuth token request")
		}
	}
}

func (c *Client) recordOAuthCooldown(flight *tokenRefresh, until time.Time, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// The latest abandoned flight can still observe server throttling after
	// its callers leave. Only a newer generation makes its result stale.
	if c.refreshGeneration == flight.generation && until.After(c.cooldown.until) {
		c.cooldown = oauthCooldown{until: until, err: err}
	}
}

// Only explicit transient HTTP responses are retried. A transport failure can
// occur after a token was issued, so it must not automatically mint another one.
func (c *Client) fetchTokenAttempt(ctx context.Context, form url.Values) (oauthAttemptResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return oauthAttemptResult{}, errors.New("could not construct OAuth request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return oauthAttemptResult{}, requestError(ctx, "OAuth token request")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return oauthAttemptResult{retryAfter: res.Header.Get("Retry-After"), retryable: retryableReadStatus(res.StatusCode)}, statusError("OAuth token request", res.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := decode(res.Body, &token); err != nil {
		return oauthAttemptResult{}, errors.New("OAuth server returned an invalid JSON response")
	}
	if token.Error != "" {
		switch token.Error {
		case "invalid_client", "invalid_client_secret", "invalid_code", "invalid_grant":
			return oauthAttemptResult{}, fmt.Errorf("OAuth refresh failed (%s); check the credentials and data center", token.Error)
		default:
			return oauthAttemptResult{}, errors.New("OAuth refresh was rejected; check credentials, data center and token limits")
		}
	}
	if token.AccessToken == "" || strings.ContainsAny(token.AccessToken, "\r\n") || token.ExpiresIn <= 0 || token.ExpiresIn > 86400 {
		return oauthAttemptResult{}, errors.New("OAuth response is missing a valid access_token or expires_in")
	}
	lifetime := time.Duration(token.ExpiresIn) * time.Second
	margin := min(30*time.Second, lifetime/10)
	return oauthAttemptResult{token: oauthToken{value: token.AccessToken, expiresAt: c.now().Add(lifetime - margin)}}, nil
}

func (c *Client) invalidate(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A parallel request may already have refreshed the rejected token.
	if c.token == token {
		c.expiresAt = time.Time{}
	}
}
