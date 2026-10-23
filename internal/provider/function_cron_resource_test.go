// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func functionCronConfig(project, pool, function, name string, updated bool) string {
	params, count, interval := "{}", 0, 0
	if updated {
		params = `{ test = "value" }`
		count, interval = 2, 600
	}
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_function_cron" "test" {
 project_id = %q
 job_pool_id = %q
 function_id = %q
 name = %q
 job_name = "terraform_test_job"
 expression = "0 0 * * *"
 timezone = "Asia/Kolkata"
 enabled = false
 end_time = 1893456000
 params = %s
 retry_count = %d
 retry_interval = %d
}
`, project, pool, function, name, params, count, interval)
}
func TestFunctionCronLifecycle(t *testing.T) { testFunctionCronLifecycle(t, false) }

func TestFunctionCronSchedulesLifecycle(t *testing.T) { testFunctionCronLifecycle(t, true) }

func testFunctionCronLifecycle(t *testing.T, schedules bool) {
	t.Helper()
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	var mu sync.Mutex
	var stored *catalyst.Cron
	creates, updates, deletes := 0, 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		body := ""
		switch {
		case req.URL.Path == "/baas/v1/project/123":
			body = `{"status":"success","data":{"id":"123","project_name":"test"}}`
		case strings.HasSuffix(req.URL.Path, "/jobpool"):
			body = `{"status":"success","data":[{"id":"456","name":"test","type":"Function"}]}`
		case strings.Contains(req.URL.Path, "/function/"):
			id := strings.TrimPrefix(req.URL.Path, "/baas/v1/project/123/function/")
			body = fmt.Sprintf(`{"status":"success","data":{"id":%q,"name":"target","stack":"node24","type":"job"}}`, id)
		case req.Method == "POST" || req.Method == "PUT":
			var desired catalyst.Cron
			if err := json.NewDecoder(req.Body).Decode(&desired); err != nil {
				return nil, err
			}
			if desired.Job.TargetType != "Function" || desired.Job.URL != "" {
				t.Error("wrong target")
			}
			if desired.Type == "Calendar" && desired.Schedule.RepetitionType == "yearly" {
				expected := []catalyst.Integer{0, 11} // Terraform January/December -> Catalyst's zero-based months.
				if len(desired.Schedule.Days) == 0 {
					expected = []catalyst.Integer{2, 8}
				}
				if !slices.Equal(desired.Schedule.Months, expected) {
					t.Error("calendar months do not match the verified console numbering")
				}
			}
			if req.Method == "POST" {
				creates++
				desired.ID = catalyst.ID(fmt.Sprint(900 + creates))
			} else {
				updates++
				if stored == nil || desired.ID != stored.ID {
					t.Error("wrong update identity")
				}
			}
			desired.Detail = desired.Schedule
			stored = &desired
		case req.Method == "DELETE":
			if stored == nil {
				t.Error("unexpected deletion")
			}
			body = fmt.Sprintf(`{"status":"success","data":{"id":%q}}`, stored.ID)
			stored = nil
			deletes++
		}
		if body == "" {
			if stored == nil {
				status = 404
				body = `{"status":"failure","data":{"error_code":"INVALID_ID"}}`
			} else {
				b, _ := json.Marshal(map[string]any{"status": "success", "data": stored})
				body = string(b)
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	initial := functionCronConfig("123", "456", "789", "test", false)
	updated := functionCronConfig("123", "456", "790", "test-updated", true)
	testCase := resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if stored != nil {
			return errors.New("cron survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "function_id", "789")},
		{ResourceName: "catalyst_function_cron.test", ImportState: true, ImportStateId: "123/901", ImportStateVerify: true},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_function_cron.test", "function_id", "790"), resource.TestCheckResourceAttr("catalyst_function_cron.test", "retry_interval", "600"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "params.%", "0")},
		{PreConfig: func() { mu.Lock(); stored.Job.Params = map[string]string{"external": "drift"}; mu.Unlock() }, Config: initial},
		{PreConfig: func() { mu.Lock(); stored = nil; mu.Unlock() }, Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "id", "902")},
		{Config: initial, PlanOnly: true},
	}}
	if schedules {
		testCase.Steps = functionCronScheduleSteps(initial, func(_ *terraform.State) error { return nil }, "123")
	}
	resource.UnitTest(t, testCase)
	if schedules {
		if creates != 1 || updates != 14 || deletes != 1 {
			t.Fatalf("unexpected schedule transitions: %d/%d/%d", creates, updates, deletes)
		}
		return
	}
	if creates != 2 || updates != 3 || deletes != 1 {
		t.Fatalf("unexpected mutations: %d/%d/%d", creates, updates, deletes)
	}
}

func TestFunctionCronTargetValidation(t *testing.T) {
	for _, mode := range []string{"valid", "wrong_pool_type", "legacy_cron_function", "denied", "missing_target"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			client, err := catalyst.New(catalyst.Config{AccessToken: "test"}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					writes++
				}
				status, body := 200, `{"status":"success","data":[{"id":"456","name":"pool","type":"Function"}]}`
				if mode == "denied" {
					status, body = 403, "server-secret"
				} else if strings.Contains(req.URL.Path, "/function/") {
					body = `{"status":"success","data":{"id":"789","name":"target","stack":"node24","type":"job"}}`
					if mode == "legacy_cron_function" {
						body = strings.Replace(body, `"type":"job"`, `"type":"cron"`, 1)
					}
					if mode == "missing_target" {
						body = strings.Replace(body, `"id":"789"`, `"id":"790"`, 1)
					}
				} else if mode == "wrong_pool_type" {
					body = strings.Replace(body, `"type":"Function"`, `"type":"Webhook"`, 1)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			r := FunctionCronResource{WebhookCronResource: WebhookCronResource{client: client}}
			err = r.verifyTarget(t.Context(), functionCronModel{ProjectID: types.StringValue("123"), JobPoolID: types.StringValue("456"), FunctionID: types.StringValue("789")})
			if (err == nil) != (mode == "valid") || writes != 0 {
				t.Fatalf("unsafe target validation: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "server-secret") {
				t.Fatal("response leaked")
			}
		})
	}
}

func TestFunctionCronRecoveryState(t *testing.T) {
	for _, mode := range []string{"partial_create", "foreign_read", "denied_read", "foreign_delete", "denied_delete", "denied_confirmation"} {
		t.Run(mode, func(t *testing.T) {
			clearEnvironment(t)
			deleted := false
			writes := 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status, body := 200, `{"status":"success","data":[{"id":"456","name":"pool","type":"Function"}]}`
				switch {
				case strings.Contains(req.URL.Path, "/function/"):
					body = `{"status":"success","data":{"id":"789","name":"target","stack":"node24","type":"job"}}`
				case strings.Contains(req.URL.Path, "/cron"):
					if req.Method == "POST" {
						writes++
						body = `{"status":"success","data":{"id":"901","cron_status":{}}}`
						break
					}
					if req.Method == "DELETE" {
						writes++
						if mode == "denied_delete" {
							status, body = 403, "server-secret"
						} else {
							deleted = true
							body = `{"status":"success","data":{"id":"901"}}`
						}
						break
					}
					if mode == "denied_read" || deleted {
						status, body = 403, "server-secret"
						break
					}
					body = `{"status":"success","data":{"id":"901","cron_name":"test","cron_type":"CronExpression","cron_execution_type":"pre-defined","cron_expression":"0 0 * * *","cron_status":false,"cron_detail":{"timezone":"UTC"},"job_meta":{"job_name":"job","jobpool_id":"456","target_type":"Function","target_id":"789"}}}`
					if strings.HasPrefix(mode, "foreign") {
						body = strings.Replace(body, `"target_type":"Function"`, `"target_type":"Webhook"`, 1)
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
			values := map[string]any{"project_id": "123", "job_pool_id": "456", "function_id": "789", "name": "test", "description": "", "expression": "0 0 * * *", "timezone": "UTC", "enabled": false, "end_time": 1893456000, "job_name": "job", "params": map[string]string{}, "retry_count": 0, "retry_interval": 0}
			var state *tfprotov6.DynamicValue
			var diagnostics []*tfprotov6.Diagnostic
			if strings.HasSuffix(mode, "read") {
				values["id"] = "901"
				read, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_function_cron", CurrentState: jsonConfig(t, values)})
				if err != nil {
					t.Fatal(err)
				}
				state, diagnostics = read.NewState, read.Diagnostics
			} else {
				prior, planned := jsonConfig(t, nil), jsonConfig(t, values)
				if mode != "partial_create" {
					values["id"] = "901"
					prior, planned = jsonConfig(t, values), jsonConfig(t, nil)
				}
				applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_function_cron", PriorState: prior, PlannedState: planned, Config: planned})
				if err != nil {
					t.Fatal(err)
				}
				state, diagnostics = applied.NewState, applied.Diagnostics
			}
			if state == nil || !hasError(diagnostics) {
				t.Fatal("failure lost recovery state or diagnostic")
			}
			for _, d := range diagnostics {
				if strings.Contains(d.Detail, "server-secret") {
					t.Fatal("response body leaked")
				}
			}
			schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			value, err := state.Unmarshal(schemas.ResourceSchemas["catalyst_function_cron"].ValueType())
			if err != nil || value.IsNull() {
				t.Fatal("state lost")
			}
			attrs := map[string]tftypes.Value{}
			if err = value.As(&attrs); err != nil {
				t.Fatal(err)
			}
			var id string
			if err = attrs["id"].As(&id); err != nil || id != "901" {
				t.Fatal("cron identity lost")
			}
			if strings.HasPrefix(mode, "foreign") && writes != 0 {
				t.Fatal("foreign target was modified")
			}
		})
	}
}
