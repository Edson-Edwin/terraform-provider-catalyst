// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

// Clone standard transports so TLS policy does not mutate shared clients or
// global defaults. Custom RoundTrippers are trusted Go integration/test hooks;
// Terraform does not expose transport injection or a TLS verification bypass.
func secureHTTPClient(source *http.Client) (*http.Client, error) {
	client := http.Client{Timeout: 30 * time.Second}
	if source != nil {
		client = *source
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if standard, ok := transport.(*http.Transport); ok {
		cloned := standard.Clone()
		config := &tls.Config{MinVersion: tls.VersionTLS12}
		if cloned.TLSClientConfig != nil {
			config = cloned.TLSClientConfig.Clone()
		}
		if config.InsecureSkipVerify {
			return nil, errors.New("TLS certificate verification must remain enabled")
		}
		config.MinVersion = max(config.MinVersion, tls.VersionTLS12)
		if config.MaxVersion != 0 && config.MaxVersion < config.MinVersion {
			return nil, errors.New("TLS transport must support TLS 1.2 or newer")
		}
		cloned.TLSClientConfig = config
		client.Transport = cloned
	} else {
		client.Transport = transport
	}
	// Never forward credentials, request bodies or temporary signatures on redirects.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &client, nil
}
