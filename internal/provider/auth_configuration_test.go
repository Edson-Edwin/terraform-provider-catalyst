// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProviderOrganizationGuardrails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    map[string]any
		env       map[string]string
		wantError bool
	}{
		{"allow", map[string]any{"organization_id": "123", "allowed_organization_ids": []string{"123"}}, nil, false},
		{"reject-allow", map[string]any{"organization_id": "456", "allowed_organization_ids": []string{"123"}}, nil, true},
		{"reject-deny", map[string]any{"organization_id": "123", "forbidden_organization_ids": []string{"123"}}, nil, true},
		{"reject-default-org", map[string]any{"forbidden_organization_ids": []string{"123"}}, nil, true},
		{"reject-conflict", map[string]any{"organization_id": "123", "allowed_organization_ids": []string{"123"}, "forbidden_organization_ids": []string{"456"}}, nil, true},
		{"reject-null-id", map[string]any{"organization_id": "123", "allowed_organization_ids": []any{nil}}, nil, true},
		{"environment", nil, map[string]string{"CATALYST_ORGANIZATION_ID": "123", "CATALYST_ALLOWED_ORGANIZATION_IDS": "123, 456"}, false},
		{"deny-environment", nil, map[string]string{"CATALYST_ORGANIZATION_ID": "123", "CATALYST_FORBIDDEN_ORGANIZATION_IDS": "123"}, true},
		{"explicit-empty-suppresses-guard", map[string]any{"allowed_organization_ids": []string{}}, map[string]string{"CATALYST_ALLOWED_ORGANIZATION_IDS": "123"}, false},
		{"explicit-overrides-env", map[string]any{"organization_id": "456", "allowed_organization_ids": []string{"456"}}, map[string]string{"CATALYST_ALLOWED_ORGANIZATION_IDS": "123"}, false},
		{"reject-empty-env-element", nil, map[string]string{"CATALYST_ORGANIZATION_ID": "123", "CATALYST_ALLOWED_ORGANIZATION_IDS": "123,"}, true},
		{"production-needs-region", map[string]any{"environment": "Production"}, nil, true},
		{"production-env-region", map[string]any{"environment": "Production"}, map[string]string{"CATALYST_DATA_CENTER": "in"}, false},
		{"production-empty-region", map[string]any{"environment": "Production", "data_center": ""}, map[string]string{"CATALYST_DATA_CENTER": "in"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvironment(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			config := tc.config
			if config == nil {
				config = map[string]any{}
			}
			config["access_token"] = "secret-token"
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("guardrail validation must not contact the API")
				return nil, fmt.Errorf("unexpected request")
			})}}
			server := providerserver.NewProtocol6(p)()
			result, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, config)})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(result.Diagnostics) != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
			}
		})
	}
}

func TestUnknownConfigurationPoliciesDoNotFallBack(t *testing.T) {
	for _, name := range []string{"allowed_organization_ids", "forbidden_organization_ids", "allowed_data_centers", "allowed_environments"} {
		for _, unknownElement := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/element=%t", name, unknownElement), func(t *testing.T) {
				clearEnvironment(t)
				t.Setenv("CATALYST_ACCESS_TOKEN", "token")
				t.Setenv("CATALYST_ORGANIZATION_ID", "123")
				t.Setenv("CATALYST_ALLOWED_ORGANIZATION_IDS", "123")
				server := providerserver.NewProtocol6(New("test")())()
				schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
				if err != nil {
					t.Fatal(err)
				}
				typ, ok := schemas.Provider.ValueType().(tftypes.Object)
				if !ok {
					t.Fatal("provider schema must be an object")
				}
				values := map[string]tftypes.Value{}
				for field, fieldType := range typ.AttributeTypes {
					values[field] = tftypes.NewValue(fieldType, nil)
				}
				if unknownElement {
					values[name] = tftypes.NewValue(typ.AttributeTypes[name], []tftypes.Value{tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
				} else {
					values[name] = tftypes.NewValue(typ.AttributeTypes[name], tftypes.UnknownValue)
				}
				config, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, values))
				if err != nil {
					t.Fatal(err)
				}
				result, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: &config})
				if err != nil {
					t.Fatal(err)
				}
				if !hasError(result.Diagnostics) {
					t.Fatal("unknown guardrail was silently ignored")
				}
			})
		}
	}
}

func TestProviderAccessPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		status, wantCalls int
		config            map[string]any
		env               string
		wantError         bool
	}{
		{"disabled", "", 200, 0, nil, "", false},
		{"success", `{"status":"success","data":{"id":"123","project_name":"Test"}}`, 200, 1, map[string]any{"validation_project_id": "123"}, "", false},
		{"environment", `{"status":"success","data":{"id":"123","project_name":"Test"}}`, 200, 1, nil, "123", false},
		{"explicit-empty", "", 200, 0, map[string]any{"validation_project_id": ""}, "123", false},
		{"invalid-id", "", 200, 0, map[string]any{"validation_project_id": "../secret"}, "", true},
		{"unauthorized", "secret-body", 401, 1, map[string]any{"validation_project_id": "123"}, "", true},
		{"forbidden", "secret-body", 403, 1, map[string]any{"validation_project_id": "123"}, "", true},
		{"mismatch", `{"status":"success","data":{"id":"456","project_name":"secret-name"}}`, 200, 1, map[string]any{"validation_project_id": "123"}, "", true},
		{"malformed", "secret-body", 200, 1, map[string]any{"validation_project_id": "123"}, "", true},
		{"guard-before-preflight", "", 200, 0, map[string]any{"validation_project_id": "123", "allowed_organization_ids": []string{"456"}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvironment(t)
			t.Setenv("CATALYST_VALIDATION_PROJECT_ID", tc.env)
			calls := 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != "https://api.catalyst.zoho.in/baas/v1/project/123" || r.Header.Get("Environment") != "Production" || r.Header.Get("CATALYST-ORG") != "123" || r.Header.Get("Authorization") != "Zoho-oauthtoken secret-token" {
					t.Error("preflight did not use selected routing and credentials")
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			config := map[string]any{"access_token": "secret-token", "data_center": "in", "environment": "Production", "organization_id": "123"}
			for k, v := range tc.config {
				config[k] = v
			}
			server := providerserver.NewProtocol6(p)()
			result, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, config)})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(result.Diagnostics) != tc.wantError || calls != tc.wantCalls {
				t.Fatalf("unexpected preflight result: calls=%d diagnostics=%v", calls, result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if strings.Contains(d.Detail, "secret") {
					t.Fatal("preflight leaked credentials or response metadata")
				}
			}
		})
	}
}

// Separate RPC servers model provider aliases sharing one transport and reading
// concurrently. Two instances deliberately use the same region.
func TestProviderInstancesIsolateCredentialsAndRouting(t *testing.T) {
	clearEnvironment(t)
	var mu sync.Mutex
	refreshes, reads := map[string]int{}, map[string]int{}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			id := r.PostForm.Get("client_id")
			wantHost := "accounts.zoho.in"
			if id == "eu" {
				wantHost = "accounts.zoho.eu"
			}
			if r.URL.Host != wantHost || r.PostForm.Get("client_secret") != id+"-secret" || r.PostForm.Get("refresh_token") != id+"-refresh" {
				t.Error("alias refresh credentials crossed instances")
			}
			refreshes[id]++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"access_token":%q,"expires_in":3600}`, id+"-token")))}, nil
		}
		id := r.Header.Get("CATALYST-ORG")
		wantHost, wantEnvironment := "api.catalyst.zoho.in", "Development"
		clientID := map[string]string{"123": "dev", "456": "prod", "789": "eu"}[id]
		if clientID == "prod" {
			wantEnvironment = "Production"
		}
		if clientID == "eu" {
			wantHost = "api.catalyst.zoho.eu"
		}
		if clientID == "" || r.URL.Host != wantHost || r.Header.Get("Environment") != wantEnvironment || r.Header.Get("Authorization") != "Zoho-oauthtoken "+clientID+"-token" {
			t.Error("alias API request crossed credentials or routing")
		}
		reads[clientID]++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"id":"123","project_name":"Test"}}`))}, nil
	})
	var servers []tfprotov6.ProviderServer
	for _, tc := range []struct{ id, dc, environment, org string }{{"dev", "in", "Development", "123"}, {"prod", "in", "Production", "456"}, {"eu", "eu", "Development", "789"}} {
		p := &CatalystProvider{httpClient: &http.Client{Transport: transport}}
		server := providerserver.NewProtocol6(p)()
		result, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"data_center": tc.dc, "environment": tc.environment, "organization_id": tc.org, "client_id": tc.id, "client_secret": tc.id + "-secret", "refresh_token": tc.id + "-refresh", "validation_project_id": "123"})})
		if err != nil {
			t.Fatal(err)
		}
		checkDiagnostics(t, result.Diagnostics)
		servers = append(servers, server)
	}
	var wg sync.WaitGroup
	for _, server := range servers {
		for range 8 {
			wg.Go(func() {
				read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: "catalyst_project", Config: jsonConfig(t, map[string]any{"id": "123"})})
				if err != nil {
					t.Error(err)
					return
				}
				checkDiagnostics(t, read.Diagnostics)
			})
		}
	}
	wg.Wait()
	for _, id := range []string{"dev", "prod", "eu"} {
		if refreshes[id] != 1 || reads[id] != 9 {
			t.Fatalf("cache not isolated for %s: refreshes=%d reads=%d", id, refreshes[id], reads[id])
		}
	}
}
