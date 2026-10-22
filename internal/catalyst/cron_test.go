// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const cronID = "9007199254740993124"
const cronJSON = `{"status":"success","data":{"id":9007199254740993124,"cron_name":"test","cron_type":"CronExpression","cron_execution_type":"pre-defined","cron_expression":"0 0 1 1 *","cron_status":false,"end_time":"1893456000","cron_detail":{"timezone":"UTC"},"job_meta":{"job_name":"test","jobpool_id":"123","target_type":"Webhook","source_type":"Cron","url":"https://example.com","request_method":"get","job_config":{"number_of_retries":"0"}}}}`

func testCron() Cron {
	enabled := false
	return Cron{Name: "test", Type: "CronExpression", ExecutionType: "pre-defined", Expression: "0 0 1 1 *", Enabled: &enabled, EndTime: 1893456000, Schedule: CronSchedule{Timezone: "UTC"}, Job: CronJob{Name: "test", PoolID: "123", TargetType: "Webhook", SourceType: "Cron", URL: "https://example.com", Method: "get", Headers: map[string]string{}, Params: map[string]string{}}}
}
func TestCronRequests(t *testing.T) {
	for _, method := range []string{"POST", "GET", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret", OrganizationID: "456"}, func(w http.ResponseWriter, r *http.Request) {
				suffix := "/" + cronID
				if method == "POST" {
					suffix = ""
				}
				if r.Method != method || r.URL.Path != "/baas/v1/project/"+projectID+"/job_scheduling/cron"+suffix {
					t.Errorf("wrong method/path %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Zoho-oauthtoken secret" || r.Header.Get("Environment") != "Development" || r.Header.Get("CATALYST-ORG") != "456" {
					t.Error("missing credentials or routing")
				}
				if method == "POST" || method == "PUT" {
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON content type")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["cron_status"] != false || body["cron_type"] != "CronExpression" {
						t.Error("wrong cron payload")
					}
					if _, ok := body["cron_detail"]; ok {
						t.Error("response-only field sent")
					}
					job, ok := body["job_meta"].(map[string]any)
					if !ok {
						t.Fatal("missing job")
					}
					if job["jobpool_id"] != "123" || job["request_body"] != "" {
						t.Error("IDs or clearing body incorrect")
					}
				}
				writeResponse(t, w, cronJSON)
			})
			var got *Cron
			var err error
			switch method {
			case "POST":
				got, err = c.CreateCron(t.Context(), projectID, testCron())
			case "PUT":
				got, err = c.UpdateCron(t.Context(), projectID, cronID, testCron())
			case "GET":
				got, err = c.GetCron(t.Context(), projectID, cronID)
			case "DELETE":
				err = c.DeleteCron(t.Context(), projectID, cronID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != nil && (string(got.ID) != cronID || got.EndTime != 1893456000 || *got.Enabled) {
				t.Fatal("incorrect response mapping")
			}
		})
	}
}
func TestCronAbsenceRequiresParentAccess(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status, parentStatus int
		body                 string
		absent               bool
	}{
		{"absent", 404, 200, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`, true},
		{"parent_denied", 404, 403, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`, false},
		{"parent_missing", 404, 404, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`, false},
		{"generic_404", 404, 200, `{"status":"failure","data":{"error_code":"OTHER"}}`, false},
		{"forbidden", 403, 200, "secret-from-server", false},
		{"unauthorized", 401, 200, "secret-from-server", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parents := 0
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/baas/v1/project/"+projectID {
					parents++
					w.WriteHeader(tc.parentStatus)
					writeResponse(t, w, projectJSON)
					return
				}
				w.WriteHeader(tc.status)
				writeResponse(t, w, tc.body)
			})
			_, err := c.GetCron(t.Context(), projectID, cronID)
			if err == nil || errors.Is(err, ErrCronNotFound) != tc.absent {
				t.Fatalf("unexpected absence classification: %v", err)
			}
			if strings.Contains(err.Error(), "secret-from-server") {
				t.Fatal("response leaked")
			}
			err = c.DeleteCron(t.Context(), projectID, cronID)
			if err == nil || errors.Is(err, ErrCronNotFound) != tc.absent {
				t.Fatalf("unexpected delete classification: %v", err)
			}
			if tc.status != 404 && parents != 0 {
				t.Fatal("permission failure must not check absence")
			}
		})
	}
}
func TestCronDoesNotRetryAmbiguousWrites(t *testing.T) {
	calls := 0
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		writeResponse(t, w, "secret-from-server")
	})
	if _, err := c.CreateCron(t.Context(), projectID, testCron()); err == nil {
		t.Fatal("accepted server failure")
	}
	if calls != 1 {
		t.Fatal("ambiguous create was retried")
	}
}
func TestCronMetadataValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		retain     bool
	}{
		{"unsupported", strings.Replace(cronJSON, "CronExpression", "Calendar", 1), true},
		{"incomplete", strings.Replace(cronJSON, `"cron_status":false,`, "", 1), true},
		{"mismatched", strings.ReplaceAll(cronJSON, cronID, "999"), false},
		{"invalid", "not-json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { writeResponse(t, w, tc.body) })
			cron, err := c.GetCron(t.Context(), projectID, cronID)
			if err == nil {
				t.Fatal("accepted invalid metadata")
			}
			if (cron != nil) != tc.retain {
				t.Fatal("unexpected ID recovery behavior")
			}
		})
	}
}

func TestCronWriteRefreshReplaysJSON(t *testing.T) {
	refreshes, writes := 0, 0
	var firstBody string
	c := testClient(t, refreshConfig(), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/v2/token" {
			refreshes++
			writeResponse(t, w, fmt.Sprintf(`{"access_token":"token%d","expires_in":3600}`, refreshes))
			return
		}
		writes++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if writes == 1 {
			firstBody = string(body)
			w.WriteHeader(401)
			return
		}
		if string(body) != firstBody || len(body) == 0 {
			t.Error("JSON body was not replayed exactly")
		}
		if r.Header.Get("Authorization") != "Zoho-oauthtoken token2" {
			t.Error("refreshed token not used")
		}
		writeResponse(t, w, cronJSON)
	})
	if _, err := c.CreateCron(t.Context(), projectID, testCron()); err != nil {
		t.Fatal(err)
	}
	if refreshes != 2 || writes != 2 {
		t.Fatalf("unexpected request counts: %d/%d", refreshes, writes)
	}
}
