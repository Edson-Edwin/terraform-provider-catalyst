// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Invokes only generated constant-response apps, never account application code.
// No OAuth, database values or credentials are sent to the application origin.
func TestAccAppSailWebsiteHealth(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CREATE") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_INVOKE") != "1" {
		t.Skip("requires temporary app deployment and invocation opt-ins")
	}
	if os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development")
	}
	for _, stack := range []string{"node24", "python_3_13", "java25"} {
		t.Run(stack, func(t *testing.T) {
			project := os.Getenv("CATALYST_TEST_PROJECT_ID")
			if !catalyst.ValidID(project) {
				t.Fatal("requires isolated project")
			}
			client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := client.ListAppSails(t.Context(), project)
			if err != nil {
				t.Fatal(err)
			}
			registerAppSailBaselineCheck(t, client, project, baseline)
			name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
			dir := t.TempDir()
			config := func(message string, updated bool) string {
				source := writeAppSailLanguageArchive(t, dir, stack, message)
				raw := appSailLanguageConfig(project, name, source, stack, updated)
				raw = strings.Replace(raw, "catalyst_auth = true", "catalyst_auth = false", 1)
				raw = strings.Replace(raw, `login_redirect = "/index.html"`, "", 1)
				return raw
			}
			initial, updated := config("first", false), config("second", true)
			check := func(want string) resource.TestCheckFunc {
				return func(s *terraform.State) error {
					r := s.RootModule().Resources["catalyst_appsail.test"]
					if r == nil {
						return errors.New("temporary website missing from state")
					}
					endpoint, err := url.Parse(r.Primary.Attributes["url"])
					if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Port() != "" || !strings.HasSuffix(endpoint.Hostname(), ".catalystappsail.in") || endpoint.RawQuery != "" || endpoint.Fragment != "" {
						return errors.New("unexpected application origin; no request made")
					}
					httpClient := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
					ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
					defer cancel()
					stable := 0
					for {
						req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
						if err != nil {
							return errors.New("health request construction failed")
						}
						req.Header.Set("Cache-Control", "no-cache")
						res, err := httpClient.Do(req)
						matched := false
						if err == nil {
							body, readErr := io.ReadAll(io.LimitReader(res.Body, 128))
							_ = res.Body.Close()
							matched = readErr == nil && res.StatusCode == 200 && string(body) == want
						}
						if matched {
							stable++
						} else {
							stable = 0
						}
						if stable == 3 {
							t.Log("HTTP 200 and expected deployed response verified:", stack, want)
							return nil
						}
						timer := time.NewTimer(2 * time.Second)
						select {
						case <-ctx.Done():
							timer.Stop()
							return errors.New("temporary website did not serve the expected response before the health timeout")
						case <-timer.C:
						}
					}
				}
			}
			resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, Steps: []resource.TestStep{{Config: initial, Check: check("first")}, {Config: updated, Check: check("second")}, {Config: updated, PlanOnly: true}}})
		})
	}
}
