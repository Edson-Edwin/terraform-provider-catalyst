// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProjectTimeoutValidation(t *testing.T) {
	clearEnvironment(t)
	server := providerserver.NewProtocol6(New("test")())()
	for _, operation := range []string{"create", "read", "update", "delete"} {
		for _, value := range []string{"5m", "30s", "", "0s", "-1m", "tomorrow", "999999999999999999999h"} {
			result, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{
				TypeName: "catalyst_project", Config: jsonConfig(t, map[string]any{"name": "fixture", "timeouts": map[string]any{operation: value}}),
			})
			if err != nil {
				t.Fatal(err)
			}
			valid := value == "5m" || value == "30s"
			if hasError(result.Diagnostics) == valid {
				t.Fatalf("incorrect validation for %s=%q: %v", operation, value, result.Diagnostics)
			}
		}
	}
	for _, id := range []string{"0", "-1", "../123", "1/2", "", "9007199254740993"} {
		result, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "catalyst_project", ID: id})
		if err != nil || hasError(result.Diagnostics) != (id != "9007199254740993") {
			t.Fatalf("incorrect import validation for %q: %v", id, err)
		}
	}
}

func TestProjectOperationTimeoutBudget(t *testing.T) {
	attributeTypes := map[string]attr.Type{"create": types.StringType}
	for _, scenario := range []string{"default", "configured", "unknown_object", "unknown_duration", "parent"} {
		t.Run(scenario, func(t *testing.T) {
			value := types.ObjectNull(attributeTypes)
			parent := t.Context()
			var diags diag.Diagnostics
			want := 5 * time.Minute
			switch scenario {
			case "configured":
				value = types.ObjectValueMust(attributeTypes, map[string]attr.Value{"create": types.StringValue("10m")})
				want = 10 * time.Minute
			case "unknown_object":
				value = types.ObjectUnknown(attributeTypes)
			case "unknown_duration":
				value = types.ObjectValueMust(attributeTypes, map[string]attr.Value{"create": types.StringUnknown()})
			case "parent":
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, time.Minute)
				defer cancel()
				want = time.Minute
			}
			ctx, cancel := projectOperationContext(parent, value, "create", 5*time.Minute, &diags)
			defer cancel()
			if diags.HasError() != strings.HasPrefix(scenario, "unknown") {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			deadline, ok := ctx.Deadline()
			if remaining := time.Until(deadline); !ok || remaining > want || remaining < want-time.Second {
				t.Fatal("operation did not respect its time budget")
			}
		})
	}
}

func TestProjectFailureRetainsState(t *testing.T) {
	for _, scenario := range []string{"partial_recovers", "create_timeout", "delete_timeout", "delete_permission", "delete_ambiguous", "delete_wrong_id", "read_permission", "read_unknown_404", "read_null_listing", "read_missing", "delete_missing", "update_permission"} {
		t.Run(scenario, func(t *testing.T) {
			clearEnvironment(t)
			writes, reads, recorded := 0, 0, 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status := 200
				body := `{"status":"success","data":{"id":"901","project_name":"fixture"}}`
				switch req.Method {
				case http.MethodPost:
					writes++
					body = `{"status":"success","data":{"id":"901"}}`
				case http.MethodDelete:
					writes++
					body = `{"status":"success","data":{"id":"901"}}`
					switch scenario {
					case "delete_wrong_id":
						body = `{"status":"success","data":{"id":"902"}}`
					case "delete_ambiguous":
						status, body = 503, "server-secret"
					}
				case http.MethodGet:
					reads++
					if req.URL.Path == "/baas/v1/project" {
						body = `{"status":"success","data":[]}`
						if scenario == "read_null_listing" {
							body = `{"status":"success","data":null}`
						}
					} else if scenario == "create_timeout" || scenario == "delete_timeout" && writes > 0 {
						<-req.Context().Done()
						return nil, req.Context().Err()
					} else if strings.HasSuffix(scenario, "permission") {
						status, body = 403, "server-secret"
					} else if scenario == "read_unknown_404" {
						status, body = 404, `{"status":"failure","data":{"error_code":"NO_ACCESS"}}`
					} else if scenario == "read_null_listing" || scenario == "read_missing" || scenario == "delete_missing" {
						status, body = 404, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			baseServer := providerserver.NewProtocol6(p)()
			schemas, err := baseServer.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			projectType := schemas.ResourceSchemas["catalyst_project"].ValueType()
			server := &projectRecordingServer{ProviderServer: baseServer, projectType: projectType, record: func(id, name string) {
				if id != "901" || name != "fixture" {
					t.Error("recorded unexpected fixture identity")
				}
				recorded++
			}}
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test", "organization_id": "123"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			values := map[string]any{"id": "901", "name": "fixture", "data_center": "us", "organization_id": "123", "deletion_protection": false, "timeouts": map[string]any{"create": "100ms", "delete": "100ms"}}
			prior := jsonConfig(t, values)
			var state *tfprotov6.DynamicValue
			var diagnostics []*tfprotov6.Diagnostic
			if strings.HasPrefix(scenario, "read_") {
				result, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_project", CurrentState: prior})
				if err != nil {
					t.Fatal(err)
				}
				state, diagnostics = result.NewState, result.Diagnostics
			} else {
				planned := jsonConfig(t, nil)
				switch scenario {
				case "partial_recovers", "create_timeout":
					delete(values, "id")
					prior, planned = jsonConfig(t, nil), jsonConfig(t, values)
				case "update_permission":
					values["deletion_protection"] = true
					planned = jsonConfig(t, values)
				}
				result, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_project", PriorState: prior, PlannedState: planned, Config: planned})
				if err != nil {
					t.Fatal(err)
				}
				state, diagnostics = result.NewState, result.Diagnostics
			}
			if hasError(diagnostics) != (scenario != "partial_recovers") || state == nil {
				t.Fatalf("unexpected result: %v", diagnostics)
			}
			for _, d := range diagnostics {
				if strings.Contains(d.Detail, "server-secret") {
					t.Fatal("response leaked")
				}
			}
			decoded, err := state.Unmarshal(projectType)
			if err != nil || decoded.IsNull() {
				t.Fatal("project state erased")
			}
			var attributes map[string]tftypes.Value
			if err = decoded.As(&attributes); err != nil {
				t.Fatal(err)
			}
			var id string
			if attributes["id"].As(&id) != nil || id != "901" {
				t.Fatal("project identity lost")
			}
			wantWrites := 0
			if scenario == "partial_recovers" || scenario == "create_timeout" || scenario == "delete_timeout" || scenario == "delete_ambiguous" || scenario == "delete_wrong_id" {
				wantWrites = 1
			}
			if writes != wantWrites || reads == 0 {
				t.Fatalf("unsafe mutation or missing verification: writes=%d reads=%d", writes, reads)
			}
			wantRecorded := 0
			if scenario == "partial_recovers" || scenario == "create_timeout" {
				wantRecorded = 1
			}
			if recorded != wantRecorded {
				t.Fatal("live recovery recorder failed to track only created state")
			}
		})
	}
}
