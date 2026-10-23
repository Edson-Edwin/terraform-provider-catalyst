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

func appSailCronConfig(project, pool, appsail, name string, updated bool) string {
	params, count, interval := "{}", 0, 0
	route, method, headers, body := "/", "get", "{}", ""
	if updated {
		params = `{ test = "value" }`
		count, interval = 2, 600
		route, method, headers, body = "/health", "post", `{ "X-Test" = "fixture" }`, "fixture payload"
	}
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_appsail_cron" "test" {
 project_id = %q
 job_pool_id = %q
 appsail_id = %q
 name = %q
 job_name = "terraform_test_job"
 expression = "0 0 * * *"
 timezone = "Asia/Kolkata"
 enabled = false
 end_time = 1893456000
 path = %q
 method = %q
 headers = %s
 body = %q
 params = %s
 retry_count = %d
 retry_interval = %d
}
`, project, pool, appsail, name, route, method, headers, body, params, count, interval)
}
func TestAppSailCronLifecycle(t *testing.T) { testAppSailCronLifecycle(t, false) }

func TestAppSailCronSchedulesLifecycle(t *testing.T) { testAppSailCronLifecycle(t, true) }

func testAppSailCronLifecycle(t *testing.T, schedules bool) {
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
			body = `{"status":"success","data":[{"id":"456","name":"test","type":"AppSail"}]}`
		case strings.Contains(req.URL.Path, "/appsail/"):
			id := strings.TrimPrefix(req.URL.Path, "/baas/v1/project/123/appsail/")
			body = fmt.Sprintf(`{"status":"success","data":{"id":%q,"name":"target","stack":"node22","url":"https://target%s.catalystappsail.in"}}`, id, id)
		case req.Method == "POST" || req.Method == "PUT":
			var desired catalyst.Cron
			if err := json.NewDecoder(req.Body).Decode(&desired); err != nil {
				return nil, err
			}
			if desired.Job.TargetType != "AppSail" || !strings.HasPrefix(desired.Job.URL, "https://target"+string(desired.Job.TargetID)+".catalystappsail.in/") {
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
	initial := appSailCronConfig("123", "456", "789", "test", false)
	updated := appSailCronConfig("123", "456", "790", "test-updated", true)
	testCase := resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if stored != nil {
			return errors.New("cron survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "appsail_id", "789")},
		{ResourceName: "catalyst_appsail_cron.test", ImportState: true, ImportStateId: "123/901", ImportStateVerify: true},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "appsail_id", "790"), resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "retry_interval", "600"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "params.%", "0")},
		{PreConfig: func() { mu.Lock(); stored.Job.Params = map[string]string{"external": "drift"}; mu.Unlock() }, Config: initial},
		{PreConfig: func() { mu.Lock(); stored = nil; mu.Unlock() }, Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "id", "902")},
		{Config: initial, PlanOnly: true},
	}}
	if schedules {
		testCase.Steps = cronScheduleTestSteps(initial, func(_ *terraform.State) error { return nil }, "123", "catalyst_appsail_cron")
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

func TestAppSailCronTargetValidation(t *testing.T) {
	for _, mode := range []string{"valid", "wrong_pool_type", "invalid_origin", "denied", "missing_target"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			client, err := catalyst.New(catalyst.Config{AccessToken: "test"}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					writes++
				}
				status, body := 200, `{"status":"success","data":[{"id":"456","name":"pool","type":"AppSail"}]}`
				if mode == "denied" {
					status, body = 403, "server-secret"
				} else if strings.Contains(req.URL.Path, "/appsail/") {
					body = `{"status":"success","data":{"id":"789","name":"target","stack":"node22","url":"https://target789.catalystappsail.in"}}`
					if mode == "invalid_origin" {
						body = strings.Replace(body, `"https://target789.catalystappsail.in"`, `"http://unsafe.invalid"`, 1)
					}
					if mode == "missing_target" {
						body = strings.Replace(body, `"id":"789"`, `"id":"790"`, 1)
					}
				} else if mode == "wrong_pool_type" {
					body = strings.Replace(body, `"type":"AppSail"`, `"type":"Webhook"`, 1)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			r := AppSailCronResource{WebhookCronResource: WebhookCronResource{client: client}}
			_, err = r.verifyTarget(t.Context(), appSailCronModel{ProjectID: types.StringValue("123"), JobPoolID: types.StringValue("456"), AppSailID: types.StringValue("789")})
			if (err == nil) != (mode == "valid") || writes != 0 {
				t.Fatalf("unsafe target validation: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "server-secret") {
				t.Fatal("response leaked")
			}
		})
	}
}

func TestAppSailCronRecoveryState(t *testing.T) {
	for _, mode := range []string{"partial_create", "foreign_read", "denied_read", "foreign_delete", "denied_delete", "denied_confirmation"} {
		t.Run(mode, func(t *testing.T) {
			clearEnvironment(t)
			deleted := false
			writes := 0
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status, body := 200, `{"status":"success","data":[{"id":"456","name":"pool","type":"AppSail"}]}`
				switch {
				case strings.Contains(req.URL.Path, "/appsail/"):
					body = `{"status":"success","data":{"id":"789","name":"target","stack":"node22","url":"https://target789.catalystappsail.in"}}`
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
					body = `{"status":"success","data":{"id":"901","cron_name":"test","cron_type":"CronExpression","cron_execution_type":"pre-defined","cron_expression":"0 0 * * *","cron_status":false,"cron_detail":{"timezone":"UTC"},"job_meta":{"job_name":"job","jobpool_id":"456","target_type":"AppSail","target_id":"789","url":"https://target789.catalystappsail.in/","request_method":"get"}}}`
					if strings.HasPrefix(mode, "foreign") {
						body = strings.Replace(body, `"target_type":"AppSail"`, `"target_type":"Webhook"`, 1)
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
			values := map[string]any{"project_id": "123", "job_pool_id": "456", "appsail_id": "789", "name": "test", "description": "", "expression": "0 0 * * *", "timezone": "UTC", "enabled": false, "end_time": 1893456000, "job_name": "job", "path": "/", "method": "get", "headers": map[string]string{}, "body": "", "params": map[string]string{}, "retry_count": 0, "retry_interval": 0}
			var state *tfprotov6.DynamicValue
			var diagnostics []*tfprotov6.Diagnostic
			if strings.HasSuffix(mode, "read") {
				values["id"] = "901"
				read, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_appsail_cron", CurrentState: jsonConfig(t, values)})
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
				applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_appsail_cron", PriorState: prior, PlannedState: planned, Config: planned})
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
			value, err := state.Unmarshal(schemas.ResourceSchemas["catalyst_appsail_cron"].ValueType())
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

func TestAppSailCronOriginAndPath(t *testing.T) {
	const origin = "https://target.catalystappsail.in"
	for _, route := range []string{"/", "/health", "/some%20path"} {
		got, err := appSailCronRoute(origin, origin+route)
		if err != nil || got != route {
			t.Fatalf("valid route %q failed: %v", route, err)
		}
	}
	for _, route := range []string{"", "health", "//evil.invalid/", "https://evil.invalid/", "/health?secret=value", "/health#fragment", "/bad\\path", "/bad\npath"} {
		if err := validAppSailCronPath(route); err == nil {
			t.Fatalf("unsafe path accepted %q", route)
		}
	}
	for _, target := range []string{"https://evil.invalid/health", "http://target.catalystappsail.in/", "https://user:secret@target.catalystappsail.in/", "https://target.catalystappsail.in/?secret=value", "//target.catalystappsail.in/"} {
		if _, err := appSailCronRoute(origin, target); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("foreign or malformed origin accepted or leaked")
		}
	}
}
