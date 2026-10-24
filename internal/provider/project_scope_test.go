// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProjectScopeGuards(t *testing.T) {
	for _, scenario := range []string{"missing_org", "changed_region", "changed_org", "unbound"} {
		operations := []string{"read", "update", "delete"}
		if scenario == "missing_org" {
			operations = append(operations, "create")
		}
		for _, operation := range operations {
			t.Run(scenario+"/"+operation, func(t *testing.T) {
				clearEnvironment(t)
				requests := 0
				p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"id":"901","project_name":"fixture"}}`))}, nil
				})}}
				server := providerserver.NewProtocol6(p)()
				config := map[string]any{"access_token": "test", "organization_id": "123", "data_center": "in"}
				if scenario == "missing_org" {
					delete(config, "organization_id")
				}
				configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, config)})
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(t, configured.Diagnostics)
				values := map[string]any{"id": "901", "name": "fixture", "deletion_protection": false, "data_center": "in", "organization_id": "123"}
				switch scenario {
				case "changed_region":
					values["data_center"] = "eu"
				case "changed_org":
					values["organization_id"] = "456"
				case "unbound":
					delete(values, "data_center")
					delete(values, "organization_id")
				}
				prior := jsonConfig(t, values)
				var state *tfprotov6.DynamicValue
				var diagnostics []*tfprotov6.Diagnostic
				if operation == "read" {
					result, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_project", CurrentState: prior})
					if err != nil {
						t.Fatal(err)
					}
					state, diagnostics = result.NewState, result.Diagnostics
				} else {
					plan := jsonConfig(t, nil)
					if operation == "update" {
						values["deletion_protection"] = true
						plan = jsonConfig(t, values)
					}
					if operation == "create" {
						prior = jsonConfig(t, nil)
						plan = jsonConfig(t, map[string]any{"name": "fixture", "deletion_protection": false})
					}
					result, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_project", PriorState: prior, PlannedState: plan, Config: plan})
					if err != nil {
						t.Fatal(err)
					}
					state, diagnostics = result.NewState, result.Diagnostics
				}
				legacyRefresh := scenario == "unbound" && operation == "read"
				if hasError(diagnostics) == legacyRefresh {
					t.Fatalf("unexpected diagnostics: %v", diagnostics)
				}
				wantRequests := 0
				if legacyRefresh {
					wantRequests = 1
				}
				if requests != wantRequests {
					t.Fatalf("scope violation reached the API: %d requests", requests)
				}
				if operation == "create" {
					return
				}
				schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
				if err != nil || state == nil {
					t.Fatal("missing recovery state", err)
				}
				decoded, err := state.Unmarshal(schemas.ResourceSchemas["catalyst_project"].ValueType())
				if err != nil || decoded.IsNull() {
					t.Fatal("project state erased", err)
				}
				var attributes map[string]tftypes.Value
				if err = decoded.As(&attributes); err != nil {
					t.Fatal(err)
				}
				var id string
				if attributes["id"].As(&id) != nil || id != "901" {
					t.Fatal("project identity lost")
				}
				if legacyRefresh {
					var dc, org string
					if attributes["data_center"].As(&dc) != nil || attributes["organization_id"].As(&org) != nil || dc != "in" || org != "123" {
						t.Fatal("successful legacy refresh did not bind routing")
					}
				}
			})
		}
	}
}
