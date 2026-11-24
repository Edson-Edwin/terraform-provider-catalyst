// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestCircuitCronLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ORGANIZATION_ID", "123")
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	const cronPath = "/baas/v1/project/123/job_scheduling/cron"
	const address = "catalyst_circuit_cron.test"
	var mu sync.Mutex
	var stored *catalyst.Cron
	var creates, updates, deletes int
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status, body := 200, ""
		switch req.Method + " " + req.URL.Path {
		case "GET /baas/v1/project/123":
			body = `{"status":"success","data":{"id":"123","project_name":"test"}}`
		case "GET /baas/v1/project/123/job_scheduling/jobpool/456":
			body = `{"status":"success","data":{"id":"456","name":"pool","type":"Circuit"}}`
		case "POST " + cronPath, "PUT " + cronPath + "/900":
			var desired catalyst.Cron
			if err := json.NewDecoder(req.Body).Decode(&desired); err != nil {
				return nil, err
			}
			if desired.Job.TargetType != "Circuit" || desired.Job.PoolID != "456" || desired.Job.URL != "" ||
				desired.Job.SourceType != "Cron" || desired.Job.Params == nil || len(desired.Job.Params) != 0 ||
				desired.Schedule.Timezone != "GMT" || desired.Enabled == nil || *desired.Enabled {
				t.Errorf("unexpected Circuit cron request: %+v", desired)
			}
			if req.Method == "POST" {
				creates++
				if desired.ID != "" || desired.Job.TargetID != "9007199254740993123" {
					t.Error("invalid create identity")
				}
				desired.ID = "900"
			} else {
				updates++
				if desired.ID != "900" || desired.Job.TargetID != "9007199254740993124" || desired.Expression != "30 9 * * *" {
					t.Error("invalid update identity or expression")
				}
			}
			desired.Detail = desired.Schedule
			desired.Job.TargetDetails = &struct {
				ID catalyst.ID `json:"id"`
			}{ID: desired.Job.TargetID}
			desired.Job.TargetID = ""
			stored = &desired
		case "GET " + cronPath + "/900":
		case "DELETE " + cronPath + "/900":
			deletes++
			stored = nil
			body = `{"status":"success","data":{"id":"900"}}`
		default:
			return nil, fmt.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		if body == "" && stored == nil {
			status, body = 404, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`
		} else if body == "" {
			payload, err := json.Marshal(map[string]any{"status": "success", "data": stored})
			if err != nil {
				return nil, err
			}
			body = string(payload)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	initial := circuitCronConfig("123", "456", "9007199254740993123", "tf_circuit_schedule", "0 9 * * *", 1893456000)
	updated := circuitCronConfig("123", "456", "9007199254740993124", "tf_circuit_schedule", "30 9 * * *", 1893456000)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)},
		Steps: []resource.TestStep{
			{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "enabled", "false"), resource.TestCheckResourceAttr(address, "circuit_id", "9007199254740993123"))},
			{ResourceName: address, ImportState: true, ImportStateId: "123/900", ImportStateVerify: true},
			{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "id", "900"), resource.TestCheckResourceAttr(address, "expression", "30 9 * * *"))},
			{Config: updated, PlanOnly: true},
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if stored != nil || creates != 1 || updates != 1 || deletes != 1 {
		t.Fatalf("unexpected lifecycle: created %d, updated %d, deleted %d, remaining %t", creates, updates, deletes, stored != nil)
	}
}

func circuitCronConfig(project, pool, target, name, expression string, endTime int64) string {
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_circuit_cron" "test" {
  project_id  = %q
  job_pool_id = %q
  circuit_id  = %q
  name        = %q
  job_name    = "tf_circuit"
  expression  = %q
  end_time    = %d
}`, project, pool, target, name, expression, endTime)
}
