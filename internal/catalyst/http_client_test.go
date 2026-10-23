// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPTransportSecurityAndIsolation(t *testing.T) {
	base := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS10}}
	source := &http.Client{Transport: base, CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	c, err := New(Config{AccessToken: "secret"}, source)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected a standard transport")
	}
	if transport == base || transport.TLSClientConfig == base.TLSClientConfig {
		t.Fatal("shared TLS state was not cloned")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("minimum TLS policy was not enforced")
	}
	if base.TLSClientConfig.MinVersion != tls.VersionTLS10 || source.CheckRedirect(nil, nil) != nil {
		t.Fatal("source client was mutated")
	}
	if !errors.Is(c.http.CheckRedirect(nil, nil), http.ErrUseLastResponse) {
		t.Fatal("redirects must remain blocked")
	}
	for _, config := range []*tls.Config{
		{InsecureSkipVerify: true},
		{MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11},
		{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS12},
	} {
		if _, err := New(Config{AccessToken: "secret"}, &http.Client{Transport: &http.Transport{TLSClientConfig: config}}); err == nil {
			t.Fatal("unsafe TLS configuration was accepted")
		}
	}
	c, err = New(Config{AccessToken: "secret"}, &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok = c.http.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Fatal("stronger TLS policy was downgraded")
	}
}

func TestTLSConnectionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		trust     bool
		maximum   uint16
		wantError bool
		wrongHost bool
	}{
		{"trusted-modern", true, tls.VersionTLS12, false, false},
		{"untrusted-certificate", false, tls.VersionTLS13, true, false},
		{"legacy-protocol", true, tls.VersionTLS11, true, false},
		{"wrong-hostname", true, tls.VersionTLS13, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Zoho-oauthtoken secret" {
					t.Error("authentication missing on verified connection")
				}
				_, err := io.WriteString(w, projectJSON)
				if err != nil {
					t.Error(err)
				}
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS10, MaxVersion: tc.maximum}
			server.StartTLS()
			defer server.Close()
			var source *http.Client
			if tc.trust {
				source = server.Client()
			}
			endpoint := server.URL
			if tc.wrongHost {
				parsed, err := url.Parse(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				parsed.Host = net.JoinHostPort("wrong.example.invalid", parsed.Port())
				endpoint = parsed.String()
				transport, ok := source.Transport.(*http.Transport)
				if !ok {
					t.Fatal("expected a standard test transport")
				}
				transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
				}
			}
			c, err := New(Config{AccessToken: "secret"}, source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.http.CloseIdleConnections()
			c.apiURL = endpoint
			// Do not repeat handshake failures: the behavior under test is TLS.
			res, err := c.request(t.Context(), http.MethodPost, "/baas/v1/project/"+projectID, nil)
			if res != nil {
				_ = res.Body.Close()
			}
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected TLS result: %v", err)
			}
			if tc.wantError && (calls.Load() != 0 || strings.Contains(err.Error(), "secret")) {
				t.Fatal("unverified TLS delivered credentials or exposed diagnostics")
			}
			if !tc.wantError && calls.Load() != 1 {
				t.Fatal("verified TLS connection was not used")
			}
		})
	}
}
