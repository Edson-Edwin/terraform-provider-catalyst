// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func testRoutingScope(dc, env, org string) map[string]any {
	return map[string]any{"data_center": dc, "environment": env, "organization_id": org}
}

// Existing protocol fixtures represent already-managed resources. Clone their
// values so adding the binding cannot alter the configuration or planned values.
func boundResourceState(t *testing.T, values map[string]any, dataCenter ...string) *tfprotov6.DynamicValue {
	t.Helper()
	copy := make(map[string]any, len(values)+1)
	for k, v := range values {
		copy[k] = v
	}
	dc := "us"
	if len(dataCenter) > 0 {
		dc = dataCenter[0]
	}
	copy["routing_scope"] = testRoutingScope(dc, "Development", "123")
	return jsonConfig(t, copy)
}

func configureScopeTest(t *testing.T, server tfprotov6.ProviderServer) {
	t.Helper()
	clearEnvironment(t)
	out, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "offline", "organization_id": "123"})})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, out.Diagnostics)
}

func TestEnvironmentScopeGuardsAllResources(t *testing.T) {
	clearEnvironment(t)
	probe := providerserver.NewProtocol6(&CatalystProvider{})()
	schemas, err := probe.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, schemas.Diagnostics)
	count := 0
	for name, schema := range schemas.ResourceSchemas {
		if name == "catalyst_project" {
			continue
		}
		count++
		for _, scenario := range []string{"environment", "data_center", "organization_id", "missing_org", "unbound", "partial"} {
			for _, operation := range []string{"read", "plan", "destroy_plan", "create", "update", "delete"} {
				if scenario == "unbound" && operation == "read" {
					continue
				}
				t.Run(name+"/"+scenario+"/"+operation, func(t *testing.T) {
					calls := 0
					p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected API request") })}}
					server := providerserver.NewProtocol6(p)()
					config := map[string]any{"access_token": "test", "data_center": "in", "environment": "Production", "organization_id": "123"}
					if scenario == "missing_org" {
						delete(config, "organization_id")
					}
					configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, config)})
					if err != nil {
						t.Fatal(err)
					}
					checkDiagnostics(t, configured.Diagnostics)
					scope := testRoutingScope("in", "Production", "123")
					switch scenario {
					case "environment":
						scope[scenario] = "Development"
					case "data_center":
						scope[scenario] = "eu"
					case "organization_id":
						scope[scenario] = "456"
					case "partial":
						scope["environment"] = nil
					}
					values := map[string]any{"id": "901", "routing_scope": scope}
					if scenario == "unbound" {
						delete(values, "routing_scope")
					}
					prior := jsonConfig(t, values)
					var diagnostics []*tfprotov6.Diagnostic
					var state *tfprotov6.DynamicValue
					switch operation {
					case "read":
						out, e := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: prior})
						err = e
						diagnostics = out.Diagnostics
						state = out.NewState
					case "plan", "destroy_plan":
						proposed := prior
						if operation == "destroy_plan" {
							proposed = jsonConfig(t, nil)
						}
						out, e := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: name, PriorState: prior, ProposedNewState: proposed, Config: proposed})
						err = e
						diagnostics = out.Diagnostics
					default:
						plan := prior
						if operation == "create" {
							prior = jsonConfig(t, nil)
						}
						if operation == "delete" {
							plan = jsonConfig(t, nil)
						}
						out, e := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: name, PriorState: prior, PlannedState: plan, Config: plan})
						err = e
						diagnostics = out.Diagnostics
						state = out.NewState
					}
					if err != nil {
						t.Fatal(err)
					}
					if !hasError(diagnostics) || calls != 0 {
						t.Fatalf("guard failed: requests=%d diagnostics=%v", calls, diagnostics)
					}
					found := false
					for _, d := range diagnostics {
						if strings.Contains(d.Summary, "routing") || strings.Contains(d.Summary, "organization") {
							found = true
						}
					}
					if !found {
						t.Fatalf("failed for the wrong reason: %v", diagnostics)
					}
					if state != nil {
						before, e := prior.Unmarshal(schema.ValueType())
						if e != nil {
							t.Fatal(e)
						}
						after, e := state.Unmarshal(schema.ValueType())
						if e != nil {
							t.Fatal(e)
						}
						if !before.Equal(after) {
							t.Fatal("blocked operation changed state")
						}
					}
				})
			}
		}
	}
	if count != 12 {
		t.Fatalf("review scope coverage when resource registrations change: %d", count)
	}
}

func TestEnvironmentScopeLegacyRefresh(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			clearEnvironment(t)
			calls := 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Environment") != "Production" || r.Header.Get("CATALYST-ORG") != "123" {
					t.Fatal("incorrect migration routing")
				}
				status, body := 200, `{"status":"success","data":{"status":true,"scheduled":false}}`
				if failed {
					status = 403
					body = `{"status":"failure"}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			cfg, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test", "data_center": "in", "environment": "Production", "organization_id": "123"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, cfg.Diagnostics)
			out, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_api_gateway", CurrentState: jsonConfig(t, map[string]any{"id": "901", "project_id": "901", "enabled": true})})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(out.Diagnostics) != failed || calls != 1 {
				t.Fatalf("unexpected refresh: %v calls=%d", out.Diagnostics, calls)
			}
			schemas, _ := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			value, err := out.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_api_gateway"].ValueType())
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]tftypes.Value
			if err = value.As(&fields); err != nil {
				t.Fatal(err)
			}
			if fields["routing_scope"].IsNull() != failed {
				t.Fatal("scope must bind only on a successful refresh")
			}
		})
	}
}
