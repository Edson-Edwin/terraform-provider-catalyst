// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func gatewayConfig(project, targetID, targetEndpoint string, mode int) string {
	rules := fmt.Sprintf(`first = { source_endpoint = "/first", target_endpoint = %q, function_id = %s }
second = { source_endpoint = "/second", target_endpoint = %q, function_id = %s, method = "POST", authentication = [] }`, targetEndpoint, targetID, targetEndpoint, targetID)
	if mode == 1 {
		rules = fmt.Sprintf(`updated = { source_endpoint = "/updated", target_endpoint = %q, function_id = %s, method = "ANY" }`, targetEndpoint, targetID)
	}
	if mode == 2 {
		rules = ""
	}
	if mode == 3 {
		rules = fmt.Sprintf(`advanced = { source_endpoint = "/advanced", target_endpoint = "/server/example/path", function_id = %s, target = "advancedio", method = "ANY" }`, targetID)
	}
	if mode == 4 {
		rules = `website = { source_endpoint = "/website", target_endpoint = "/app/index.html", target = "client", method = "GET", authentication = [] }`
	}
	if mode == 5 {
		rules = fmt.Sprintf(`secured = { source_endpoint = "/secured", target_endpoint = %q, function_id = %s, authentication = ["APIKey", "CatalystUserManagement", "OAuth"] }`, targetEndpoint, targetID)
	}
	return fmt.Sprintf(`resource "catalyst_api_gateway" "test" { project_id = %q }
resource "catalyst_api_gateway_ruleset" "test" {
 project_id = catalyst_api_gateway.test.id
 rules = { %s }
}`, project, rules)
}
func TestGatewayLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	var mu sync.Mutex
	enabled := false
	rules := []catalyst.GatewayRule{}
	writes, toggles := 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		var data any
		status := 200
		path := "/baas/v1/project/123/api-gateway"
		switch {
		case req.URL.Path == "/baas/v1/project/123":
			data = map[string]string{"id": "123", "project_name": "Test"}
		case req.URL.Path == path && req.Method == "GET":
			data = map[string]bool{"status": enabled, "scheduled": false}
		case req.URL.Path == path && req.Method == "PUT":
			var input struct {
				Enabled bool `json:"status"`
			}
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				return nil, err
			}
			enabled = input.Enabled
			toggles++
			data = map[string]bool{"status": enabled, "scheduled": false}
		case req.URL.Path == path+"/api" && req.Method == "GET":
			if !enabled {
				status = 403
			} else {
				data = rules
			}
		case req.URL.Path == path+"/api" && req.Method == "PUT":
			if !enabled {
				return nil, errors.New("disabled rules write")
			}
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				return nil, err
			}
			defer func() { _ = req.MultipartForm.RemoveAll() }()
			file, _, err := req.FormFile("config_file")
			if err != nil {
				return nil, err
			}
			defer file.Close()
			if err = json.NewDecoder(file).Decode(&rules); err != nil {
				return nil, err
			}
			for i := range rules {
				rules[i].ID = catalyst.ID(fmt.Sprint(9000 + i))
			}
			writes++
			data = "scheduled"
		default:
			return nil, errors.New("unexpected Gateway request")
		}
		raw, err := json.Marshal(map[string]any{"status": "success", "data": data})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})}}
	initial := "provider \"catalyst\" {}\n" + gatewayConfig("123", `"456"`, "/server/target/execute", 0)
	updated := "provider \"catalyst\" {}\n" + gatewayConfig("123", `"456"`, "/server/target/execute", 1)
	empty := "provider \"catalyst\" {}\n" + gatewayConfig("123", `"456"`, "/server/target/execute", 2)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if enabled || len(rules) != 0 {
			return errors.New("Gateway baseline not restored")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_api_gateway.test", "enabled", "true"), resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.first.authentication.0", "OAuth"), resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.second.authentication.#", "0"))},
		{ResourceName: "catalyst_api_gateway.test", ImportState: true, ImportStateId: "123", ImportStateVerify: true},
		{ResourceName: "catalyst_api_gateway_ruleset.test", ImportState: true, ImportStateId: "123", ImportStateVerify: true},
		{Config: updated, Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.%", "1")},
		{PreConfig: func() { mu.Lock(); rules = []catalyst.GatewayRule{}; mu.Unlock() }, Config: updated},
		{Config: gatewayConfig("123", "456", "/server/example/execute", 3), Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.advanced.target", "advancedio")},
		{Config: gatewayConfig("123", "456", "/server/example/execute", 4), Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.website.target", "client")},
		{Config: gatewayConfig("123", "456", "/server/example/execute", 5), Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.secured.authentication.#", "3")},
		{Config: empty, Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.%", "0")},
		{Config: empty, PlanOnly: true},
	}})
	if writes != 7 || toggles != 2 {
		t.Fatalf("unexpected mutations: rules=%d toggles=%d", writes, toggles)
	}
}
func TestGatewayRulesetPartialWriteRetainsState(t *testing.T) {
	clearEnvironment(t)
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"status":"success","data":{"status":true,"scheduled":false}}`
		status := 200
		if strings.HasSuffix(req.URL.Path, "/api") {
			body = `{"status":"success","data":[]}`
			if req.Method == "PUT" {
				status = 503
				body = "server-secret"
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	server := providerserver.NewProtocol6(p)()
	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, configured.Diagnostics)
	values := map[string]any{"project_id": "123", "rules": map[string]any{"test": map[string]any{"source_endpoint": "/test", "target_endpoint": "/server/target/execute", "function_id": "456", "method": "GET", "authentication": []string{"OAuth"}}}}
	applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_api_gateway_ruleset", PriorState: jsonConfig(t, nil), PlannedState: jsonConfig(t, values), Config: jsonConfig(t, values)})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(applied.Diagnostics) || applied.NewState == nil {
		t.Fatal("expected error with recovery state")
	}
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := applied.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_api_gateway_ruleset"].ValueType())
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err = state.As(&attrs); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = attrs["id"].As(&id); err != nil || id != "123" {
		t.Fatal("ruleset recovery identity lost")
	}
	for _, d := range applied.Diagnostics {
		if strings.Contains(d.Detail, "server-secret") {
			t.Fatal("response body leaked")
		}
	}
}
func TestGatewayImportValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, kind := range []string{"catalyst_api_gateway", "catalyst_api_gateway_ruleset"} {
		for _, id := range []string{"0", "../123", "123/456", "123"} {
			result, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: kind, ID: id})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(result.Diagnostics) != (id != "123") {
				t.Fatal("invalid import validation")
			}
		}
	}
}
