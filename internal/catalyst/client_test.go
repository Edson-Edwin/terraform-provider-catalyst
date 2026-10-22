// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const projectID = "9007199254740993123"
const projectJSON = `{"status":"success","data":{"id":9007199254740993123,"project_name":"Example","project_type":"Live","timezone":"Asia/Kolkata","project_domain_details":{"project_domain_name":"example.development","project_domain":"https://example.development.catalystserverless.com"}}}`

func testClient(t *testing.T, config Config, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.apiURL, c.accountsURL = server.URL, server.URL
	return c
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := io.WriteString(w, body); err != nil {
		t.Error(err)
	}
}

func TestGetProject(t *testing.T) {
	for _, quoted := range []bool{false, true} {
		t.Run(fmt.Sprint(quoted), func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret", DataCenter: "in", Environment: "Production", OrganizationID: "123"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/baas/v1/project/"+projectID {
					t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Zoho-oauthtoken secret" || r.Header.Get("CATALYST-ORG") != "123" || r.Header.Get("Environment") != "Production" {
					t.Error("incorrect request headers")
				}
				body := projectJSON
				if quoted {
					body = strings.Replace(body, projectID, `"`+projectID+`"`, 1)
				}
				writeResponse(t, w, body)
			})
			p, err := c.GetProject(t.Context(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if string(p.ID) != projectID || p.Name != "Example" || p.Timezone != "Asia/Kolkata" || p.DomainDetails.Name != "example.development" {
				t.Fatalf("incorrect project mapping: %+v", p)
			}
		})
	}
}

func refreshConfig() Config {
	return Config{ClientID: "client", ClientSecret: "secret", RefreshToken: "refresh", RedirectURI: "https://example.com/callback"}
}

func TestRefreshCacheAndExpiry(t *testing.T) {
	var refreshes atomic.Int32
	c := testClient(t, refreshConfig(), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/v2/token" {
			refreshes.Add(1)
			if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("OAuth credentials must be sent in a POST form body")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			for k, v := range map[string]string{"grant_type": "refresh_token", "client_id": "client", "client_secret": "secret", "refresh_token": "refresh", "redirect_uri": "https://example.com/callback"} {
				if r.PostForm.Get(k) != v {
					t.Errorf("incorrect OAuth form field %s", k)
				}
			}
			writeResponse(t, w, `{"access_token":"fresh","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Zoho-oauthtoken fresh" {
			t.Error("fresh token not used")
		}
		writeResponse(t, w, projectJSON)
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.GetProject(t.Context(), projectID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("concurrent reads refreshed %d times", refreshes.Load())
	}
	c.mu.Lock()
	c.expiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if _, err := c.GetProject(t.Context(), projectID); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 2 {
		t.Fatal("expired token was not refreshed")
	}
}

func TestUnauthorizedRetry(t *testing.T) {
	for _, alwaysReject := range []bool{false, true} {
		t.Run(fmt.Sprint(alwaysReject), func(t *testing.T) {
			var refreshes, reads int
			c := testClient(t, refreshConfig(), func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/v2/token" {
					refreshes++
					writeResponse(t, w, fmt.Sprintf(`{"access_token":"token%d","expires_in":3600}`, refreshes))
					return
				}
				reads++
				if alwaysReject || r.Header.Get("Authorization") == "Zoho-oauthtoken token1" {
					w.WriteHeader(401)
					return
				}
				writeResponse(t, w, projectJSON)
			})
			_, err := c.GetProject(t.Context(), projectID)
			if (err != nil) != alwaysReject {
				t.Fatalf("unexpected result: %v", err)
			}
			if reads != 2 || refreshes != 2 {
				t.Fatalf("want bounded single retry, reads=%d refreshes=%d", reads, refreshes)
			}
		})
	}
}

func TestProjectFailuresDoNotExposeResponse(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "sensitive-body"}, {"forbidden", 403, "sensitive-body"},
		{"missing", 404, "sensitive-body"}, {"throttled", 429, "sensitive-body"}, {"server", 500, "sensitive-body"},
		{"bad-json", 200, "sensitive-body"}, {"no-data", 200, `{"status":"success","data":null}`},
		{"api-error", 200, `{"status":"error","message":"sensitive-body"}`},
		{"wrong-id", 200, `{"status":"success","data":{"id":"1","project_name":"sensitive-body"}}`},
		{"missing-name", 200, `{"status":"success","data":{"id":"9007199254740993123"}}`},
		{"oversize", 200, strings.Repeat("s", 1024*1024+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			c := testClient(t, Config{AccessToken: "sensitive-token"}, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				writeResponse(t, w, tc.body)
			})
			_, err := c.GetProject(t.Context(), projectID)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatal("response or secret leaked in error")
			}
			if calls != 1 {
				t.Fatal("static credentials must not retry")
			}
		})
	}
}

func TestOAuthFailures(t *testing.T) {
	for _, body := range []string{`{"error":"invalid_client"}`, `{"error":"sensitive-token"}`, `{}`, `{"access_token":"sensitive-token","expires_in":0}`, `{"access_token":"sensitive-token","expires_in":999999999}`, `not-json`} {
		c := testClient(t, refreshConfig(), func(w http.ResponseWriter, r *http.Request) { writeResponse(t, w, body) })
		_, err := c.GetProject(t.Context(), projectID)
		if err == nil || strings.Contains(err.Error(), "sensitive-token") {
			t.Fatalf("unsafe OAuth result: %v", err)
		}
	}
}

func TestRedirectAndCancellation(t *testing.T) {
	var leaked atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer destination.Close()
	for _, config := range []Config{{AccessToken: "secret"}, refreshConfig()} {
		c := testClient(t, config, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		})
		if _, err := c.GetProject(t.Context(), projectID); err == nil {
			t.Fatal("redirect should be rejected")
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("credentials were forwarded to redirect target")
	}
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled request reached server") })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.GetProject(ctx, projectID); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestInvalidConfigurationAndIDs(t *testing.T) {
	for _, config := range []Config{
		{}, {AccessToken: "a", DataCenter: "unknown"}, {AccessToken: "a", Environment: "prod"},
		{AccessToken: "a", OrganizationID: "../x"}, {ClientID: "id"},
		{AccessToken: "a", RefreshToken: "refresh"}, {AccessToken: "bad\nheader"},
		{ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh", RedirectURI: "not-a-url"},
	} {
		if _, err := New(config, nil); err == nil {
			t.Fatal("expected invalid configuration")
		}
	}
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ID reached server") })
	for _, id := range []string{"", "0", "-1", "1.5", "1e3", "../123", "123?x=1", " 123"} {
		if _, err := c.GetProject(t.Context(), id); err == nil {
			t.Fatalf("invalid ID accepted: %s", id)
		}
	}
}

func TestRegionRouting(t *testing.T) {
	for dc, hosts := range map[string]endpoints{
		"us": {"https://api.catalyst.zoho.com", "https://accounts.zoho.com"},
		"eu": {"https://api.catalyst.zoho.eu", "https://accounts.zoho.eu"},
		"in": {"https://api.catalyst.zoho.in", "https://accounts.zoho.in"},
		"au": {"https://api.catalyst.zoho.com.au", "https://accounts.zoho.com.au"},
		"ca": {"https://api.catalyst.zohocloud.ca", "https://accounts.zohocloud.ca"},
		"jp": {"https://api.catalyst.zoho.jp", "https://accounts.zoho.jp"},
		"sa": {"https://api.catalyst.zoho.sa", "https://accounts.zoho.sa"},
	} {
		c, err := New(Config{DataCenter: dc, AccessToken: "token"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.apiURL != hosts.api || c.accountsURL != hosts.accounts {
			t.Errorf("wrong endpoint routing for %s", dc)
		}
	}
}
