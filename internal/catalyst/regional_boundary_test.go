// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRegionalBucketOriginsFailClosed(t *testing.T) {
	for dc, endpoints := range regions {
		for _, environment := range []string{"Development", "Production"} {
			t.Run(dc+"/"+environment, func(t *testing.T) {
				c, err := New(Config{DataCenter: dc, Environment: environment, AccessToken: "oauth-secret"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				origin := c.stratusOrigin("test-bucket")
				otherDC := "us"
				if dc == "us" {
					otherDC = "in"
				}
				other, err := New(Config{DataCenter: otherDC, Environment: environment, AccessToken: "oauth-secret"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				otherEnvironment := "Production"
				if environment == "Production" {
					otherEnvironment = "Development"
				}
				otherEnv, err := New(Config{DataCenter: dc, Environment: otherEnvironment, AccessToken: "oauth-secret"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				for name, forged := range map[string]string{
					"other-region":      other.stratusOrigin("test-bucket"),
					"other-environment": otherEnv.stratusOrigin("test-bucket"),
					"downgrade":         strings.Replace(origin, "https://", "http://", 1),
					"lookalike-host":    origin + ".attacker.invalid",
					"userinfo":          "https://oauth-secret@" + strings.TrimPrefix(origin, "https://"),
					"unexpected-port":   origin + ":8443",
					"path":              origin + "/untrusted",
					"query":             origin + "?token=signature-secret",
				} {
					t.Run(name, func(t *testing.T) {
						calls := 0
						c, err := New(Config{DataCenter: dc, Environment: environment, AccessToken: "oauth-secret"}, &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
							calls++
							if r.URL.String() != endpoints.api+"/baas/v1/project/123/bucket?bucket_name=test-bucket" {
								t.Error("untrusted metadata triggered an unexpected request")
							}
							body, err := json.Marshal(map[string]any{"status": "success", "data": []any{map[string]any{"bucket_name": "test-bucket", "bucket_url": forged, "bucket_meta": map[string]bool{"versioning": false}}}})
							if err != nil {
								return nil, err
							}
							return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
						})})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := c.GetStratusBucket(t.Context(), "123", "test-bucket"); err == nil || strings.Contains(err.Error(), "secret") || calls != 1 {
							t.Fatalf("unsafe regional origin accepted or exposed: err=%v calls=%d", err, calls)
						}
					})
				}
			})
		}
	}
}

func TestRegionalRedirectsNeverForwardCredentials(t *testing.T) {
	for dc := range regions {
		for _, environment := range []string{"Development", "Production"} {
			for _, operation := range []string{"oauth", "api", "stratus"} {
				for _, status := range []int{301, 302, 303, 307, 308} {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", dc, environment, operation, status), func(t *testing.T) {
						calls := 0
						config := Config{DataCenter: dc, Environment: environment, OrganizationID: "123", AccessToken: "oauth-secret"}
						if operation == "oauth" {
							config.AccessToken = ""
							config.ClientID, config.ClientSecret, config.RefreshToken = "client", "client-secret", "refresh-secret"
						}
						c, err := New(config, &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
							calls++
							if r.URL.Host == "attacker.invalid" {
								t.Error("redirect forwarded a request to an untrusted host")
							}
							if operation == "stratus" && (r.Header.Get("Authorization") != "" || r.Header.Get("CATALYST-ORG") != "" || r.Header.Get("Environment") != "") {
								t.Error("OAuth context leaked to a signed data transfer")
							}
							header := make(http.Header)
							header.Set("Location", "https://attacker.invalid/collect")
							return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("secret-body"))}, nil
						})})
						if err != nil {
							t.Fatal(err)
						}
						switch operation {
						case "oauth":
							_, err = c.accessToken(t.Context())
							if err == nil {
								t.Error("OAuth redirect was accepted")
							}
						case "api":
							_, err = c.GetProject(t.Context(), "123")
							if err == nil {
								t.Error("API redirect was accepted")
							}
						case "stratus":
							c.stratusSignatures = map[string]stratusSignature{"123/test-bucket": {Query: "token=signature-secret", Until: time.Now().Add(time.Hour)}}
							bucket := &StratusBucket{Name: "test-bucket", URL: c.stratusOrigin("test-bucket")}
							var res *http.Response
							res, err = c.signedObjectRequest(t.Context(), "123", bucket, "key", http.MethodGet, nil, "", false)
							if res != nil {
								if res.StatusCode != status {
									t.Error("signed transfer followed a redirect")
								}
								_ = res.Body.Close()
							} else {
								t.Errorf("missing redirect response: %v", err)
							}
						}
						if calls != 1 || (err != nil && strings.Contains(err.Error(), "secret")) {
							t.Fatalf("redirect isolation failed: calls=%d err=%v", calls, err)
						}
					})
				}
			}
		}
	}
}
