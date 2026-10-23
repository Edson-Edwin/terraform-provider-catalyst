// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFunctionCronTargetIdentityAndSerialization(t *testing.T) {
	enabled := false
	var stored *Cron
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" {
			var request map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&request) != nil {
				t.Error("invalid request")
			}
			var job map[string]json.RawMessage
			if json.Unmarshal(request["job_meta"], &job) != nil {
				t.Error("invalid job")
			}
			for _, key := range []string{"url", "request_method", "request_body", "headers", "target_details"} {
				if _, ok := job[key]; ok {
					t.Errorf("unexpected Function job field %s", key)
				}
			}
			if string(job["target_id"]) != `"789"` {
				t.Error("Function target identity omitted")
			}
			raw, _ := json.Marshal(request)
			if json.Unmarshal(raw, &stored) != nil {
				t.Error("invalid cron")
			}
			stored.ID = "123"
			stored.Detail = stored.Schedule
			// Catalyst resolves the request's target_id into response target_details.
			stored.Job.TargetDetails = &struct {
				ID ID `json:"id"`
			}{ID: stored.Job.TargetID}
			stored.Job.TargetID = ""
			value, _ := json.Marshal(map[string]any{"status": "success", "data": stored})
			writeResponse(t, w, string(value))
			return
		}
		if r.Method == "DELETE" {
			stored = nil
			writeResponse(t, w, `{"status":"success","data":{"id":"123"}}`)
			return
		}
		if r.URL.Path == "/baas/v1/project/456" {
			writeResponse(t, w, `{"status":"success","data":{"id":"456","project_name":"test"}}`)
			return
		}
		if stored == nil {
			w.WriteHeader(404)
			writeResponse(t, w, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
			return
		}
		body, _ := json.Marshal(map[string]any{"status": "success", "data": stored})
		writeResponse(t, w, string(body))
	})
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	desired := Cron{Name: "test", Type: "CronExpression", ExecutionType: "pre-defined", Expression: "0 0 * * *", Enabled: &enabled, EndTime: 1893456000, Schedule: CronSchedule{Timezone: "UTC"}, Job: CronJob{Name: "job", PoolID: "999", TargetID: "789", TargetType: "Function", SourceType: "Cron", Params: map[string]string{}}}
	for _, id := range []string{"", "123"} {
		var value *Cron
		var err error
		if id == "" {
			value, err = c.CreateCron(t.Context(), "456", desired)
		} else {
			value, err = c.UpdateCron(t.Context(), "456", id, desired)
		}
		if err != nil || value.Job.TargetID != "789" {
			t.Fatalf("target lost: %v", err)
		}
	}
	if _, err := c.GetCron(t.Context(), "456", "123"); err == nil {
		t.Fatal("webhook resource accepted Function cron")
	}
	if _, err := c.GetFunctionCron(t.Context(), "456", "123"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteCron(t.Context(), "456", "123"); err != nil {
		t.Fatal(err)
	}
	if err := c.WaitFunctionCronDeleted(t.Context(), "456", "123"); err != nil {
		t.Fatal(err)
	}
	desired.Job.TargetID = "invalid"
	if _, err := c.CreateCron(t.Context(), "456", desired); err == nil || strings.Contains(err.Error(), "server-secret") {
		t.Fatal("invalid target accepted")
	}
}
