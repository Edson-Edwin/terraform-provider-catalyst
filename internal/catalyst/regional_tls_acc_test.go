// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestAccRegionalTLS verifies public endpoint identity without OAuth credentials
// or HTTP requests. This is transport evidence, not authenticated API coverage.
func TestAccRegionalTLS(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_REGIONAL_TLS") != "1" {
		t.Skip("set TF_ACC=1 and CATALYST_TEST_REGIONAL_TLS=1 for public regional TLS checks")
	}
	client, err := secureHTTPClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("live TLS checks require a standard provider transport")
	}
	for dc, endpoints := range regions {
		for service, endpoint := range map[string]string{"api": endpoints.api, "oauth": endpoints.accounts} {
			t.Run(dc+"/"+service, func(t *testing.T) {
				t.Parallel()
				u, err := url.Parse(endpoint)
				if err != nil || u.Scheme != "https" || u.Hostname() == "" {
					t.Fatal("invalid regional HTTPS endpoint")
				}
				config := transport.TLSClientConfig.Clone()
				config.ServerName = u.Hostname()
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: config}
				connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), "443"))
				if err != nil {
					t.Fatalf("regional TLS verification failed (%T); check reachability and the system trust store", err)
				}
				defer connection.Close()
				secured, ok := connection.(*tls.Conn)
				if !ok {
					t.Fatal("endpoint did not establish a TLS connection")
				}
				state := secured.ConnectionState()
				if !state.HandshakeComplete || state.Version < tls.VersionTLS12 || len(state.VerifiedChains) == 0 {
					t.Fatal("endpoint did not satisfy the provider TLS policy")
				}
				t.Logf("Verified %s using %s; no credentials or HTTP request sent", u.Hostname(), tls.VersionName(state.Version))
			})
		}
	}
}
