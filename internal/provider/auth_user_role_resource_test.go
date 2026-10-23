// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func authRoleConfig(role, fallback string) string {
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_auth_user_role" "test" {
  project_id = "123"
  user_id = "9007199254740993123"
  role_id = %q
  destroy_role_id = %q
}
data "catalyst_auth_user" "test" {
  project_id = catalyst_auth_user_role.test.project_id
  user_id = catalyst_auth_user_role.test.user_id
  depends_on = [catalyst_auth_user_role.test]
}`, role, fallback)
}

func TestAuthUserRoleTerraformLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline")
	var mu sync.Mutex
	role := "700"
	writes := 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path != "/baas/v1/project/123/project-user/9007199254740993123" {
			return nil, errors.New("unexpected auth path")
		}
		if req.Method == "PUT" {
			var body map[string]string
			if json.NewDecoder(req.Body).Decode(&body) != nil || len(body) != 2 || body["email_id"] != "private@example.test" {
				return nil, errors.New("profile update not restricted to role")
			}
			role = body["role_id"]
			writes++
		} else if req.Method != "GET" {
			return nil, errors.New("user invitation/deletion is forbidden")
		}
		body := fmt.Sprintf(`{"status":"success","data":{"user_id":9007199254740993123,"email_id":"private@example.test","first_name":"Private profile","user_type":"App User","status":"ACTIVE","role_details":{"role_id":%q,"role_name":%q}}}`, role, "Role "+role)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	address := "catalyst_auth_user_role.test"
	config := authRoleConfig("701", "700")
	noProfile := func(s *terraform.State) error {
		for _, r := range s.RootModule().Resources {
			for k, v := range r.Primary.Attributes {
				if strings.Contains(k, "email") || strings.Contains(v, "private@example.test") || strings.Contains(v, "Private profile") {
					return errors.New("profile leaked into state")
				}
			}
		}
		return nil
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)},
		CheckDestroy: func(_ *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if role != "700" {
				return errors.New("destroy did not restore fallback role")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "id", "123/9007199254740993123"), resource.TestCheckResourceAttr(address, "role_id", "701"), resource.TestCheckResourceAttr("data.catalyst_auth_user.test", "role_id", "701"), noProfile)},
			{ResourceName: address, ImportState: true, ImportStateId: "123/9007199254740993123/700", ImportStateVerify: true},
			{Config: authRoleConfig("702", "700"), Check: resource.TestCheckResourceAttr(address, "role_name", "Role 702")},
			{PreConfig: func() { mu.Lock(); role = "703"; mu.Unlock() }, Config: authRoleConfig("702", "700"), Check: resource.TestCheckResourceAttr(address, "role_id", "702")},
			{Config: authRoleConfig("702", "704"), Check: resource.TestCheckResourceAttr(address, "destroy_role_id", "704")},
			{Config: authRoleConfig("702", "700"), Check: noProfile},
			{Config: authRoleConfig("702", "700"), PlanOnly: true},
		},
	})
	if writes != 4 {
		t.Fatalf("unexpected writes (including fallback-only updates): %d", writes)
	}
}

func TestAuthUserRoleRejectsMalformedConfiguration(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline")
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, Steps: []resource.TestStep{
		{Config: authRoleConfig("../701", "700"), ExpectError: regexp.MustCompile("Invalid Catalyst ID")},
		{Config: authRoleConfig("701", "0"), ExpectError: regexp.MustCompile("Invalid Catalyst ID")},
	}})
}

func TestAuthUserRoleFailureRecovery(t *testing.T) {
	for _, scenario := range []string{"administrator", "ambiguous_create", "denied_read", "missing_user", "missing_parent", "failed_destroy"} {
		t.Run(scenario, func(t *testing.T) {
			clearEnvironment(t)
			writes := 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status := 200
				body := `{"status":"success","data":{"user_id":42,"email_id":"private@example.test","user_type":"App User","status":"ACTIVE","role_details":{"role_id":700,"role_name":"Reader"}}}`
				if scenario == "failed_destroy" {
					body = strings.Replace(body, `"role_id":700`, `"role_id":701`, 1)
				}
				if req.Method == "PUT" {
					writes++
					status = 403
					body = "private@example.test"
				}
				if scenario == "administrator" {
					body = strings.Replace(body, "App User", "App Administrator", 1)
				}
				if scenario == "denied_read" {
					status = 403
					body = "private@example.test"
				}
				if scenario == "ambiguous_create" && req.Method == "PUT" {
					status = 503
					body = "private@example.test"
				}
				if scenario == "missing_user" || scenario == "missing_parent" {
					status = 404
					body = "private@example.test"
					if scenario == "missing_user" && req.URL.Path == "/baas/v1/project/123" {
						status = 200
						body = `{"status":"success","data":{"id":"123","project_name":"test"}}`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "offline"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			values := map[string]any{"id": "123/42", "project_id": "123", "user_id": "42", "role_id": "701", "destroy_role_id": "700", "role_name": "Reader"}
			prior := jsonConfig(t, values)
			switch scenario {
			case "denied_read", "missing_user", "missing_parent":
				got, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_auth_user_role", CurrentState: prior})
				if err != nil {
					t.Fatal(err)
				}
				if hasError(got.Diagnostics) != (scenario != "missing_user") {
					t.Fatal("unsafe missing-user handling")
				}
				if scenario == "missing_user" {
					schemas, _ := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
					v, err := got.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_auth_user_role"].ValueType())
					if err != nil || !v.IsNull() {
						t.Fatal("confirmed absence retained")
					}
				}
			default:
				planned := jsonConfig(t, values)
				if scenario == "failed_destroy" {
					planned = jsonConfig(t, nil)
				} else {
					prior = jsonConfig(t, nil)
				}
				got, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_auth_user_role", PriorState: prior, PlannedState: planned, Config: planned})
				if err != nil {
					t.Fatal(err)
				}
				if !hasError(got.Diagnostics) {
					t.Fatal("unsafe assignment accepted")
				}
				for _, d := range got.Diagnostics {
					if strings.Contains(d.Detail, "private@example.test") {
						t.Fatal("profile leaked in diagnostic")
					}
				}
				if scenario == "ambiguous_create" || scenario == "failed_destroy" {
					schemas, _ := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
					v, err := got.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_auth_user_role"].ValueType())
					if err != nil || v.IsNull() {
						t.Fatal("failed write or destroy lost recovery state")
					}
				}
			}
			if writes != 0 && scenario != "ambiguous_create" && scenario != "failed_destroy" {
				t.Fatal("unexpected access change")
			}
			if (scenario == "ambiguous_create" || scenario == "failed_destroy") && writes != 1 {
				t.Fatal("ambiguous write replayed")
			}
		})
	}
}
