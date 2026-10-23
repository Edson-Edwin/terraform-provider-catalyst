// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
					if detail, ok := body["job_detail"].(map[string]any); !ok || detail["timezone"] != "GMT" {
						t.Error("UTC must use the console-supported GMT API timezone")
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

func TestCronCreateRetainsOnlyTrustedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		retain     bool
	}{
		{"invalid_end_time", strings.Replace(cronJSON, `"end_time":"1893456000"`, `"end_time":{}`, 1), true},
		{"invalid_job", strings.Replace(cronJSON, `"jobpool_id":"123"`, `"jobpool_id":{}`, 1), true},
		{"failed_envelope", strings.Replace(cronJSON, `"success"`, `"failure"`, 1), false},
		{"invalid_id", strings.Replace(cronJSON, cronID, `"not-an-id"`, 1), false},
		{"null_data", `{"status":"success","data":null}`, false},
		{"invalid_json", `{"status":"success","data":{"id":"123",`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeResponse(t, w, tc.body)
			})
			got, err := c.CreateCron(t.Context(), projectID, testCron())
			if err == nil || (got != nil) != tc.retain {
				t.Fatalf("unexpected recovery result: resource=%v error=%v", got, err)
			}
			if got != nil && string(got.ID) != cronID {
				t.Fatal("created ID was not preserved exactly")
			}
		})
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

func TestWaitCronDeleted(t *testing.T) {
	for _, tc := range []struct {
		name         string
		parentStatus int
		cancel       bool
	}{
		{"eventual_deletion", 200, false}, {"lost_parent_access", 403, false}, {"cancellation", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reads, waits := 0, 0
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/baas/v1/project/"+projectID {
					w.WriteHeader(tc.parentStatus)
					writeResponse(t, w, projectJSON)
					return
				}
				reads++
				if reads < 3 {
					writeResponse(t, w, cronJSON)
					return
				}
				w.WriteHeader(404)
				writeResponse(t, w, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
			})
			c.wait = func(ctx context.Context, d time.Duration) error {
				waits++
				if tc.cancel {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			err := c.WaitCronDeleted(ctx, projectID, cronID)
			if tc.cancel {
				if !errors.Is(err, context.Canceled) || reads != 1 {
					t.Fatalf("cancellation ignored: %v", err)
				}
			} else if tc.parentStatus == 403 {
				if err == nil || errors.Is(err, ErrCronNotFound) {
					t.Fatal("lost access treated as deletion")
				}
			} else if err != nil || reads != 3 || waits != 2 {
				t.Fatalf("deletion not awaited: reads=%d waits=%d err=%v", reads, waits, err)
			}
		})
	}
}

func TestWaitCronDeletedDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { writeResponse(t, w, cronJSON) })
	if err := c.WaitCronDeleted(ctx, projectID, cronID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline ignored: %v", err)
	}
}
