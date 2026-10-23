// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func projectResourceConfig(name string, protected bool) string {
	return fmt.Sprintf("provider \"catalyst\" {}\nresource \"catalyst_project\" \"test\" {\n name=%q\n deletion_protection=%t\n}\n", name, protected)
}
func TestProjectResourceLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "test")
	var mu sync.Mutex
	var current *catalyst.Project
	creates, deletes := 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		var body any
		switch {
		case req.Method == "POST":
			var desired struct {
				Name string `json:"project_name"`
			}
			if json.NewDecoder(req.Body).Decode(&desired) != nil {
				return nil, errors.New("invalid project request")
			}
			if current != nil {
				t.Error("project overwritten")
			}
			creates++
			current = &catalyst.Project{ID: catalyst.ID(fmt.Sprint(900 + creates)), Name: desired.Name, Type: "Live", Timezone: "Asia/Kolkata"}
			body = current
		case req.Method == "DELETE":
			if current == nil {
				t.Error("unexpected delete")
			} else {
				deletes++
				body = map[string]any{"id": current.ID}
				current = nil
			}
		case req.URL.Path == "/baas/v1/project":
			list := []catalyst.Project{}
			if current != nil {
				list = append(list, *current)
			}
			body = list
		default:
			if current != nil {
				body = current
			} else {
				status = 404
				body = map[string]string{"error_code": "INVALID_ID"}
			}
		}
		envelopeStatus := "success"
		if status == 404 {
			envelopeStatus = "failure"
		}
		raw, _ := json.Marshal(map[string]any{"status": envelopeStatus, "data": body})
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}}
	initial := projectResourceConfig("fixture", false)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if current != nil {
			return errors.New("project survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_project.test", "id", "901")},
		{ResourceName: "catalyst_project.test", ImportState: true, ImportStateId: "901", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"deletion_protection"}},
		{Config: projectResourceConfig("fixture", true), Check: resource.TestCheckResourceAttr("catalyst_project.test", "deletion_protection", "true")},
		{Config: initial},
		{Config: projectResourceConfig("renamed", false), Check: resource.TestCheckResourceAttr("catalyst_project.test", "id", "902")},
		{PreConfig: func() { mu.Lock(); current = nil; mu.Unlock() }, Config: projectResourceConfig("renamed", false), Check: resource.TestCheckResourceAttr("catalyst_project.test", "id", "903")},
		{Config: projectResourceConfig("renamed", false), PlanOnly: true},
	}})
	if creates != 3 || deletes != 2 {
		t.Fatalf("unexpected mutation counts %d/%d", creates, deletes)
	}
}
func TestProjectProtectionAndPartialState(t *testing.T) {
	for _, mode := range []string{"protected", "partial"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `{"status":"success","data":[]}`
				if req.Method == "POST" {
					writes++
					body = `{"status":"success","data":{"id":"901","project_name":{}}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			values := map[string]any{"name": "fixture", "deletion_protection": true}
			prior, planned := jsonConfig(t, nil), jsonConfig(t, values)
			if mode == "protected" {
				values["id"] = "901"
				prior, planned = jsonConfig(t, values), jsonConfig(t, nil)
			}
			result, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_project", PriorState: prior, PlannedState: planned, Config: planned})
			if err != nil {
				t.Fatal(err)
			}
			if !hasError(result.Diagnostics) || result.NewState == nil {
				t.Fatal("expected recovery state and diagnostic")
			}
			schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			state, err := result.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_project"].ValueType())
			if err != nil || state.IsNull() {
				t.Fatal("lost project state")
			}
			attrs := map[string]tftypes.Value{}
			if err = state.As(&attrs); err != nil {
				t.Fatal(err)
			}
			var id string
			if err = attrs["id"].As(&id); err != nil || id != "901" {
				t.Fatal("project identity lost")
			}
			if (mode == "protected" && writes != 0) || (mode == "partial" && writes != 1) {
				t.Fatal("unsafe mutation")
			}
		})
	}
}
