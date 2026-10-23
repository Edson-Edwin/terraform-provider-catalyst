// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const authUserFixture = `{"status":"success","data":{"user_id":9007199254740993123,"email_id":"private@example.test","first_name":"Private profile","user_type":"App User","status":"ACTIVE","role_details":{"role_id":700,"role_name":"Reader"}}}`

func TestAuthUserReadContract(t *testing.T) {
	for _, scenario := range []string{"numeric", "quoted", "denied", "malformed", "mismatch", "null", "failure", "missing_parent_readable", "missing_parent_denied"} {
		t.Run(scenario, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret", DataCenter: "in", Environment: "Development", OrganizationID: "123"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "Zoho-oauthtoken secret" || r.Header.Get("Environment") != "Development" || r.Header.Get("CATALYST-ORG") != "123" {
					t.Error("wrong request routing/authentication")
				}
				if r.URL.Path == "/baas/v1/project/123" {
					if scenario == "missing_parent_denied" {
						w.WriteHeader(403)
						writeResponse(t, w, "private@example.test")
						return
					}
					writeResponse(t, w, `{"status":"success","data":{"id":"123","project_name":"fixture"}}`)
					return
				}
				if r.URL.Path != "/baas/v1/project/123/project-user/"+projectID {
					t.Error("wrong user path")
				}
				body := authUserFixture
				switch scenario {
				case "quoted":
					body = strings.Replace(body, projectID, `"`+projectID+`"`, 1)
				case "denied":
					w.WriteHeader(403)
					body = "private@example.test"
				case "malformed":
					body = "private@example.test"
				case "mismatch":
					body = strings.Replace(body, projectID, "42", 1)
				case "null":
					body = `{"status":"success","data":null}`
				case "failure":
					body = `{"status":"failure","data":{"email_id":"private@example.test"}}`
				case "missing_parent_readable", "missing_parent_denied":
					w.WriteHeader(404)
					body = "private@example.test"
				}
				writeResponse(t, w, body)
			})
			u, err := c.GetAuthUser(t.Context(), "123", projectID)
			if scenario == "numeric" || scenario == "quoted" {
				if err != nil || u.ID != ID(projectID) || u.RoleID != "700" || u.RoleName != "Reader" {
					t.Fatal("incorrect access metadata", err)
				}
				raw, _ := json.Marshal(u)
				if strings.Contains(string(raw), "private@example.test") || strings.Contains(string(raw), "Private profile") {
					t.Fatal("profile escaped internal parsing")
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "private@example.test") {
					t.Fatal("failure accepted or profile leaked")
				}
				if errors.Is(err, ErrAuthUserNotFound) != (scenario == "missing_parent_readable") {
					t.Fatal("user absence not confirmed against parent")
				}
			}
		})
	}
}

func TestAuthUserRoleUpdateContract(t *testing.T) {
	for _, scenario := range []string{"update", "already_assigned", "administrator", "denied", "ambiguous", "wrong_readback", "false_success", "malformed_success"} {
		t.Run(scenario, func(t *testing.T) {
			writes := 0
			role := "700"
			if scenario == "already_assigned" {
				role = "701"
			}
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/baas/v1/project/123/project-user/"+projectID {
					t.Fatal("wrong path")
				}
				if r.Method == "GET" {
					body := strings.Replace(authUserFixture, `"role_id":700`, `"role_id":"`+role+`"`, 1)
					if scenario == "administrator" {
						body = strings.Replace(body, "App User", "App Administrator", 1)
					}
					writeResponse(t, w, body)
					return
				}
				if r.Method != "PUT" {
					t.Fatal("unexpected write method")
				}
				writes++
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 2 || body["role_id"] != "701" || body["email_id"] != "private@example.test" {
					t.Fatal("role update modified unrelated profile fields")
				}
				switch scenario {
				case "denied":
					w.WriteHeader(403)
					writeResponse(t, w, "private@example.test")
					return
				case "ambiguous":
					w.WriteHeader(503)
					writeResponse(t, w, "private@example.test")
					return
				case "false_success":
					writeResponse(t, w, `{"status":"failure","data":{"error":"private@example.test"}}`)
					return
				case "malformed_success":
					writeResponse(t, w, `{"status":"success","data":null}`)
					return
				}
				if scenario != "wrong_readback" {
					role = "701"
				}
				writeResponse(t, w, `{"status":"success","data":{"user_id":"`+projectID+`"}}`)
			})
			c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			u, err := c.SetAuthUserRole(t.Context(), "123", projectID, "701")
			success := scenario == "update" || scenario == "already_assigned"
			if success && (err != nil || u.RoleID != "701") {
				t.Fatal("role update failed", err)
			}
			if !success && (err == nil || strings.Contains(err.Error(), "private@example.test")) {
				t.Fatal("unsafe role failure handling")
			}
			expected := 1
			if scenario == "administrator" || scenario == "already_assigned" {
				expected = 0
			}
			if writes != expected {
				t.Fatalf("unexpected or replayed writes: %d", writes)
			}
		})
	}
}

func TestAuthUserInvalidIDsDoNotContactAPI(t *testing.T) {
	c := testClient(t, Config{AccessToken: "secret"}, func(_ http.ResponseWriter, _ *http.Request) { t.Error("invalid input contacted API") })
	for _, ids := range [][3]string{{"../1", "2", "3"}, {"1", "0", "3"}, {"1", "2", "bad"}} {
		if _, err := c.SetAuthUserRole(t.Context(), ids[0], ids[1], ids[2]); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
}
