// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func webhookCronConfig(projectID, poolID, name string, updated bool) string {
	fields := `method = "get"`
	if updated {
		fields = `method = "post"
 description = "Updated test cron"
 headers = { "X-Test" = "fixture" }
 params = { "test" = "fixture" }
 body = "fixture payload"
 retry_count = 1
 retry_interval = 60`
	}
	expression := "0 0 1 1 *"
	if updated {
		expression = "0 1 1 1 *"
	}
	return fmt.Sprintf(`
provider "catalyst" {}
resource "catalyst_webhook_cron" "test" {
 project_id = %q
 job_pool_id = %q
 name = %q
 job_name = %q
 expression = %q
 timezone = "Asia/Kolkata"
 enabled = false
 end_time = 1893456000
 url = "https://example.com/terraform-provider-test"
 %s
}
`, projectID, poolID, name, name, expression, fields)
}

// This exercises real Terraform plan/apply/import/destroy with a local fake API.
func TestWebhookCronLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	var mu sync.Mutex
	var stored map[string]any
	creates, updates, deletes := 0, 0, 0
	const id = "9007199254740993124"
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		var response any
		if req.URL.Path == "/baas/v1/project/123" {
			response = map[string]any{"status": "success", "data": map[string]any{"id": "123", "project_name": "Test"}}
		} else {
			switch req.Method {
			case "POST", "PUT":
				var data map[string]any
				if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
					return nil, err
				}
				data["id"] = id
				data["cron_detail"] = data["job_detail"]
				delete(data, "job_detail")
				stored = data
				if req.Method == "POST" {
					creates++
				} else {
					updates++
				}
				response = map[string]any{"status": "success", "data": stored}
			case "GET", "DELETE":
				if stored == nil {
					status = 404
					response = map[string]any{"status": "failure", "data": map[string]any{"error_code": "INVALID_ID"}}
				} else {
					response = map[string]any{"status": "success", "data": stored}
					if req.Method == "DELETE" {
						stored = nil
						deletes++
					}
				}
			default:
				return nil, fmt.Errorf("unexpected request method")
			}
		}
		body, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}}
	config := webhookCronConfig("123", "456", "tf_test", false)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)},
		CheckDestroy: func(_ *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if stored != nil {
				return fmt.Errorf("cron remains after destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "id", id), resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "enabled", "false"))},
			{ResourceName: "catalyst_webhook_cron.test", ImportState: true, ImportStateId: "123/" + id, ImportStateVerify: true},
			{Config: webhookCronConfig("123", "456", "tf_test_renamed", true), Check: resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "method", "post")},
			{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "headers.%", "0"), resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "body", ""))},
			{Config: config, PlanOnly: true},
			{Config: strings.Replace(config, "enabled = false", "enabled = true", 1), Check: resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "enabled", "true")},
			{Config: config},
			{PreConfig: func() { mu.Lock(); stored = nil; mu.Unlock() }, Config: config, Check: resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "id", id)},
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if creates != 2 || updates != 4 || deletes != 1 {
		t.Fatalf("unexpected lifecycle requests: creates=%d updates=%d deletes=%d", creates, updates, deletes)
	}
}

func TestWebhookCronValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	base := map[string]any{"project_id": "123", "job_pool_id": "456", "name": "test", "job_name": "test", "expression": "0 0 1 1 *", "timezone": "UTC", "url": "https://example.com", "end_time": 1893456000}
	for _, tc := range []struct {
		key   string
		value any
	}{{"project_id", "../123"}, {"job_pool_id", "0"}, {"expression", "* * *"}, {"timezone", "Local"}, {"url", "https://user:password@example.com"}, {"url", "ftp://example.com"}, {"name", " "}, {"method", "GET"}, {"end_time", 0}, {"retry_count", -1}, {"retry_count", 1}, {"job_name", strings.Repeat("a", 21)}} {
		t.Run(fmt.Sprint(tc.key, tc.value), func(t *testing.T) {
			values := map[string]any{}
			for k, v := range base {
				values[k] = v
			}
			values[tc.key] = tc.value
			validated, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "catalyst_webhook_cron", Config: jsonConfig(t, values)})
			if err != nil {
				t.Fatal(err)
			}
			if !hasError(validated.Diagnostics) {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestAccWebhookCronResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_CRON_CREATE") != "1" {
		t.Skip("set TF_ACC=1 and CATALYST_TEST_ALLOW_CRON_CREATE=1 to create disabled test crons")
	}
	runLiveWebhookCron(t)
}

func TestWebhookCronReadFailurePreservesState(t *testing.T) {
	for _, status := range []int{401, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			clearEnvironment(t)
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret-from-server")), Header: make(http.Header)}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			current := jsonConfig(t, map[string]any{"id": "789", "project_id": "123"})
			read, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_webhook_cron", CurrentState: current})
			if err != nil {
				t.Fatal(err)
			}
			if !hasError(read.Diagnostics) {
				t.Fatal("API failure must produce diagnostics")
			}
			for _, d := range read.Diagnostics {
				if strings.Contains(d.Detail, "secret-from-server") {
					t.Fatal("response leaked")
				}
			}
			schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if read.NewState == nil {
				t.Fatal("state was discarded")
			}
			state, err := read.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_webhook_cron"].ValueType())
			if err != nil {
				t.Fatal(err)
			}
			if state.IsNull() {
				t.Fatal("API failure removed resource")
			}
		})
	}
}

func TestWebhookCronImportValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, id := range []string{"123", "123/0", "123/456/789", "../456", "123/456"} {
		imported, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "catalyst_webhook_cron", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if hasError(imported.Diagnostics) != (id != "123/456") {
			t.Errorf("unexpected import result for %s", id)
		}
	}
}
