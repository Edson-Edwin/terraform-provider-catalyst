// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProjectManagementRecoveryAndDeletion(t *testing.T) {
	for _, scenario := range []string{"partial", "ambiguous", "existing", "denied", "missing", "wrong_name", "delete"} {
		t.Run(scenario, func(t *testing.T) {
			writes := 0
			deleted := false
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					writes++
					if scenario == "ambiguous" {
						w.WriteHeader(503)
						writeResponse(t, w, "server-secret")
						return
					}
					writeResponse(t, w, `{"status":"success","data":{"id":"123","project_name":{}}}`)
					return
				}
				if r.Method == "DELETE" {
					writes++
					deleted = true
					writeResponse(t, w, `{"status":"success","data":{"id":"123"}}`)
					return
				}
				if scenario == "denied" {
					w.WriteHeader(403)
					writeResponse(t, w, "server-secret")
					return
				}
				if r.URL.Path == "/baas/v1/project" {
					if scenario == "existing" {
						writeResponse(t, w, `{"status":"success","data":[{"id":"123","project_name":"fixture"}]}`)
					} else {
						writeResponse(t, w, `{"status":"success","data":[]}`)
					}
					return
				}
				if scenario == "missing" || deleted {
					w.WriteHeader(404)
					writeResponse(t, w, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
					return
				}
				writeResponse(t, w, `{"status":"success","data":{"id":"123","project_name":"fixture"}}`)
			})
			c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			if scenario == "partial" || scenario == "ambiguous" || scenario == "existing" {
				p, err := c.CreateProject(t.Context(), "fixture")
				if err == nil || strings.Contains(err.Error(), "server-secret") {
					t.Fatal("creation failure mishandled")
				}
				if scenario == "partial" && (p == nil || p.ID != "123") {
					t.Fatal("created ID lost")
				}
				if scenario == "existing" && writes != 0 {
					t.Fatal("existing project overwritten")
				}
				if scenario == "ambiguous" && writes != 1 {
					t.Fatal("ambiguous creation replayed")
				}
				return
			}
			if scenario == "missing" {
				_, err := c.GetManagedProject(t.Context(), "123")
				if !errors.Is(err, ErrProjectNotFound) {
					t.Fatal(err)
				}
				return
			}
			name := "fixture"
			if scenario == "wrong_name" {
				name = "other"
			}
			err := c.DeleteProject(t.Context(), "123", name)
			if scenario == "delete" {
				if err != nil || writes != 1 {
					t.Fatal("deletion failed", err)
				}
				if err = c.WaitProjectDeleted(t.Context(), "123"); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || writes != 0 || strings.Contains(err.Error(), "server-secret") {
				t.Fatal("unsafe project deletion")
			}
		})
	}
}
