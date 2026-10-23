// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Exercise actual OAuth/API requests, token renewal, organization and environment
// headers for every DC without overriding the client's production routing table.
func TestRegionalAuthenticationRouting(t *testing.T) {
	for _, tc := range []struct{ dc, api, accounts string }{
		{"us", "api.catalyst.zoho.com", "accounts.zoho.com"},
		{"eu", "api.catalyst.zoho.eu", "accounts.zoho.eu"},
		{"in", "api.catalyst.zoho.in", "accounts.zoho.in"},
		{"au", "api.catalyst.zoho.com.au", "accounts.zoho.com.au"},
		{"ca", "api.catalyst.zohocloud.ca", "accounts.zohocloud.ca"},
		{"jp", "api.catalyst.zoho.jp", "accounts.zoho.jp"},
		{"sa", "api.catalyst.zoho.sa", "accounts.zoho.sa"},
	} {
		t.Run(tc.dc, func(t *testing.T) {
			t.Parallel()
			for _, environment := range []string{"Development", "Production"} {
				t.Run(environment, func(t *testing.T) {
					refreshes, reads := 0, 0
					config := refreshConfig()
					config.DataCenter, config.Environment, config.OrganizationID = tc.dc, environment, "123"
					c, err := New(config, &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
						if r.URL.Scheme != "https" {
							t.Error("non-HTTPS request")
						}
						if r.Method == http.MethodPost {
							refreshes++
							if r.URL.Host != tc.accounts || r.URL.Path != "/oauth/v2/token" || r.Header.Get("Authorization") != "" || r.Header.Get("CATALYST-ORG") != "" {
								t.Error("credentials routed to the wrong OAuth origin or headers")
							}
							if err := r.ParseForm(); err != nil || r.PostForm.Get("refresh_token") != config.RefreshToken {
								t.Error("incorrect refresh credentials")
							}
							if refreshes == 1 {
								return tokenResponse("rejected"), nil
							}
							return tokenResponse("fresh"), nil
						}
						reads++
						if r.URL.Host != tc.api || r.URL.Path != "/baas/v1/project/"+projectID || r.Header.Get("Environment") != environment || r.Header.Get("CATALYST-ORG") != "123" {
							t.Error("API request crossed region, environment or organization")
						}
						if reads == 1 {
							return oauthTestResponse(401, "", ""), nil
						}
						if r.Header.Get("Authorization") != "Zoho-oauthtoken fresh" {
							t.Error("renewed token not used")
						}
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(projectJSON))}, nil
					})})
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						if _, err := c.GetProject(t.Context(), projectID); err != nil {
							t.Fatal(err)
						}
					}
					if refreshes != 2 || reads != 3 {
						t.Fatalf("unexpected refresh/cache behavior: refreshes=%d reads=%d", refreshes, reads)
					}
				})
			}
		})
	}
}

func TestOrganizationAndProductionGuardrails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    Config
		wantError bool
	}{
		{"development-default", Config{AccessToken: "secret"}, false},
		{"production-needs-region", Config{AccessToken: "secret", Environment: "Production"}, true},
		{"production-explicit-us", Config{AccessToken: "secret", Environment: "Production", DataCenter: "us"}, false},
		{"allow-match", Config{AccessToken: "secret", OrganizationID: "123", AllowedOrganizationIDs: []string{"123", "456"}}, false},
		{"allow-mismatch", Config{AccessToken: "secret", OrganizationID: "789", AllowedOrganizationIDs: []string{"123"}}, true},
		{"deny-match", Config{AccessToken: "secret", OrganizationID: "123", ForbiddenOrganizationIDs: []string{"123"}}, true},
		{"deny-other", Config{AccessToken: "secret", OrganizationID: "123", ForbiddenOrganizationIDs: []string{"456"}}, false},
		{"allow-no-default-org", Config{AccessToken: "secret", AllowedOrganizationIDs: []string{"123"}}, true},
		{"deny-no-default-org", Config{AccessToken: "secret", ForbiddenOrganizationIDs: []string{"123"}}, true},
		{"conflicting-guards", Config{AccessToken: "secret", OrganizationID: "123", AllowedOrganizationIDs: []string{"123"}, ForbiddenOrganizationIDs: []string{"456"}}, true},
		{"invalid-id", Config{AccessToken: "secret", OrganizationID: "123", AllowedOrganizationIDs: []string{"secret-invalid-id"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.config, nil)
			if (err != nil) != tc.wantError || (err != nil && strings.Contains(err.Error(), "secret")) {
				t.Fatalf("invalid guardrail result: %v", err)
			}
		})
	}
}

func TestRegionalStratusOrigins(t *testing.T) {
	expected := map[string]string{"us": "com", "eu": "eu", "in": "in", "au": "com.au", "ca": "ca", "jp": "jp", "sa": "sa"}
	if len(expected) != len(regions) {
		t.Fatal("add an independent Stratus expectation for every supported data center")
	}
	for dc, suffix := range expected {
		for _, environment := range []string{"Development", "Production"} {
			t.Run(dc+"/"+environment, func(t *testing.T) {
				c, err := New(Config{DataCenter: dc, Environment: environment, AccessToken: "secret"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				bucket := "test-bucket"
				if environment == "Development" {
					bucket += "-development"
				}
				want := "https://" + bucket + ".zohostratus." + suffix
				if got := c.stratusOrigin("test-bucket"); got != want {
					t.Fatalf("wrong regional object origin: got %s want %s", got, want)
				}
			})
		}
	}
}
