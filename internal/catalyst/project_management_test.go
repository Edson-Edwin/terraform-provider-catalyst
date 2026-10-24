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

func TestProjectLifecycleWaiters(t *testing.T) {
	for _, scenario := range []string{"ready", "delayed_ready", "delayed_delete", "wrong_name", "wrong_id", "permission", "malformed", "listing_denied", "listing_null", "unknown_404", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			reads, lists, writes, waits := 0, 0, 0, 0
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				if r.URL.Path == "/baas/v1/project" {
					lists++
					switch {
					case scenario == "listing_denied":
						w.WriteHeader(403)
						writeResponse(t, w, "server-secret")
					case scenario == "listing_null":
						writeResponse(t, w, `{"status":"success","data":null}`)
					case scenario == "delayed_delete" && lists == 1:
						writeResponse(t, w, `{"status":"success","data":[{"id":"123","project_name":"fixture"}]}`)
					default:
						writeResponse(t, w, `{"status":"success","data":[]}`)
					}
					return
				}
				reads++
				switch {
				case scenario == "permission":
					w.WriteHeader(403)
					writeResponse(t, w, "server-secret")
				case scenario == "malformed":
					writeResponse(t, w, `{"status":"success","data":null}`)
				case scenario == "wrong_name":
					writeResponse(t, w, `{"status":"success","data":{"id":"123","project_name":"foreign"}}`)
				case scenario == "wrong_id":
					writeResponse(t, w, `{"status":"success","data":{"id":"456","project_name":"fixture"}}`)
				case scenario == "unknown_404":
					w.WriteHeader(404)
					writeResponse(t, w, `{"status":"failure","data":{"error_code":"NO_ACCESS"}}`)
				case scenario == "delayed_delete", scenario == "listing_denied", scenario == "listing_null", scenario == "delayed_ready" && reads == 1:
					w.WriteHeader(404)
					writeResponse(t, w, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
				default:
					writeResponse(t, w, `{"status":"success","data":{"id":"123","project_name":"fixture"}}`)
				}
			})
			c.wait = func(ctx context.Context, _ time.Duration) error {
				waits++
				if scenario == "cancel" {
					return context.Canceled
				}
				return ctx.Err()
			}
			var err error
			if scenario == "delayed_delete" || scenario == "cancel" {
				err = c.WaitProjectDeleted(t.Context(), "123")
			} else {
				_, err = c.WaitProjectReady(t.Context(), "123", "fixture")
			}
			success := scenario == "ready" || scenario == "delayed_ready" || scenario == "delayed_delete"
			if (err == nil) != success {
				t.Fatalf("unexpected waiter result: %v", err)
			}
			if writes != 0 || (err != nil && strings.Contains(err.Error(), "server-secret")) {
				t.Fatal("waiter mutated a project or exposed the response")
			}
			if !success && scenario != "cancel" && waits != 0 {
				t.Fatal("waiter retried an unsafe observation")
			}
			if scenario == "delayed_delete" && (reads != 2 || lists != 4) {
				t.Fatalf("absence not confirmed after inconsistent listing: reads=%d lists=%d", reads, lists)
			}
		})
	}
}

func TestProjectWaitRespectsOperationDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	waitCtx, stop := projectWaitContext(ctx)
	defer stop()
	want, _ := ctx.Deadline()
	got, ok := waitCtx.Deadline()
	if !ok || !want.Equal(got) {
		t.Fatal("configured deadline was shortened")
	}
	bounded, done := projectWaitContext(context.WithoutCancel(t.Context()))
	defer done()
	if deadline, ok := bounded.Deadline(); !ok || time.Until(deadline) > 2*time.Minute {
		t.Fatal("standalone client wait is unbounded")
	}
}

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
