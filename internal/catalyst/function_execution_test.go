// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFunctionExecutionFixtureSafety(t *testing.T) {
	for _, scenario := range []string{"success", "invoke_failure", "wrong_output", "configuration_failure", "foreign_id", "stale_runtime"} {
		t.Run(scenario, func(t *testing.T) {
			baseline := FunctionMetadata{ID: "111", Name: "existing", Stack: "node22", Type: "basicio"}
			var current *FunctionDeployment
			var artifact []byte
			uploads, configurations, invocations, deletes := 0, 0, 0, 0
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				respond := func(value any) {
					t.Helper()
					if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": value}); err != nil {
						t.Error(err)
					}
				}
				switch {
				case r.Method == "PUT":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					defer func() {
						if err := r.MultipartForm.RemoveAll(); err != nil {
							t.Errorf("remove multipart fixture: %v", err)
						}
					}()
					file, _, err := r.FormFile("code")
					if err == nil {
						artifact, err = io.ReadAll(file)
						file.Close()
						if err != nil {
							t.Error(err)
							return
						}
						uploads++
					} else if errors.Is(err, http.ErrMissingFile) {
						configurations++
					} else {
						t.Error(err)
						return
					}
					current = &FunctionDeployment{FunctionMetadata: FunctionMetadata{ID: "789", Name: "tfexecfixture", Stack: "node22", Type: "basicio"}}
					if err := json.Unmarshal([]byte(r.FormValue("configuration")), &current.Configuration); err != nil {
						t.Error(err)
					}
					if scenario == "foreign_id" {
						result := *current
						result.ID = baseline.ID
						current = nil
						respond(result)
						return
					}
					if scenario == "configuration_failure" && configurations == 1 {
						w.WriteHeader(503)
						if _, err := io.WriteString(w, "server-secret"); err != nil {
							t.Errorf("write fixture response: %v", err)
						}
						return
					}
					respond(current)
				case strings.HasSuffix(r.URL.Path, "/execute"):
					invocations++
					if r.Method != "POST" || r.URL.Path != "/baas/v1/project/456/function/789/execute" {
						t.Error("invocation escaped owned fixture")
					}
					if scenario == "invoke_failure" {
						w.WriteHeader(503)
						if _, err := io.WriteString(w, "server-secret"); err != nil {
							t.Errorf("write fixture response: %v", err)
						}
						return
					}
					output := "tfexecfixture-first"
					if uploads == 2 {
						output = "tfexecfixture-updated"
					}
					if scenario == "stale_runtime" && invocations == 8 {
						output = "tfexecfixture-first"
					}
					if scenario == "wrong_output" {
						output = "server-secret"
					}
					if err := json.NewEncoder(w).Encode(map[string]string{"output": output}); err != nil {
						t.Errorf("encode fixture response: %v", err)
					}
				case r.Method == "DELETE":
					if r.URL.Path != "/baas/v1/project/456/function/789" {
						t.Error("cleanup targeted foreign ID")
					}
					deletes++
					current = nil
					respond(map[string]string{"id": "789"})
				case strings.HasSuffix(r.URL.Path, "/download"):
					if _, err := w.Write(artifact); err != nil {
						t.Errorf("write fixture response: %v", err)
					}
				case r.URL.Path == "/baas/v1/project/456/function":
					list := []FunctionMetadata{baseline}
					if current != nil {
						list = append(list, current.FunctionMetadata)
					}
					respond(list)
				case r.URL.Path == "/baas/v1/project/456":
					respond(map[string]string{"id": "456", "project_name": "Test"})
				default:
					respond(current)
				}
			})
			c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			err := runFunctionExecutionFixture(t.Context(), c, "456", "tfexecfixture", "node22")
			if scenario == "success" || scenario == "stale_runtime" {
				want := 12
				if scenario == "stale_runtime" {
					want = 14
				}
				if err != nil || uploads != 3 || configurations != 1 || invocations != want {
					t.Fatalf("lifecycle failed: uploads=%d configs=%d invokes=%d err=%v", uploads, configurations, invocations, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "server-secret") {
				t.Fatalf("expected sanitized failure: %v", err)
			}
			expectedDeletes := 1
			if scenario == "foreign_id" {
				expectedDeletes = 0
				if invocations != 0 {
					t.Fatal("foreign Function invoked")
				}
			}
			if deletes != expectedDeletes || current != nil {
				t.Fatalf("cleanup failed: deletes=%d", deletes)
			}
			if (scenario == "invoke_failure" || scenario == "wrong_output") && invocations != 1 {
				t.Fatal("invocation retried")
			}
			if scenario == "configuration_failure" && configurations != 1 {
				t.Fatal("ambiguous configuration write retried")
			}
		})
	}
}

func TestFunctionExecutionRefusesArtifactDrift(t *testing.T) {
	calls := 0
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("drifted artifact invoked")
		}
		if _, err := w.Write([]byte("PK\x03\x04changed")); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	})
	digest := sha256.Sum256([]byte("original"))
	if err := executeFunctionFixture(t.Context(), c, "456", "789", hex.EncodeToString(digest[:]), "fixture"); err == nil || calls != 1 {
		t.Fatal("artifact drift did not block invocation")
	}
}

func TestFunctionExecutionFixturePrerequisites(t *testing.T) {
	valid := map[string]string{"CATALYST_TEST_ALLOW_FUNCTION_CREATE": "1", "CATALYST_TEST_ALLOW_FUNCTION_INVOKE": "1", "CATALYST_DATA_CENTER": "in", "CATALYST_ENVIRONMENT": "Development", "CATALYST_ORGANIZATION_ID": "123", "CATALYST_TEST_PROJECT_ID": "456"}
	for key := range valid {
		t.Run(key, func(t *testing.T) {
			_, _, err := functionExecutionFixtureConfig(func(k string) string {
				if k == key {
					return ""
				}
				return valid[k]
			})
			if err == nil {
				t.Fatal("missing prerequisite accepted")
			}
		})
	}
	_, _, err := functionExecutionFixtureConfig(func(k string) string {
		if k == "CATALYST_ENVIRONMENT" {
			return "Production"
		}
		return valid[k]
	})
	if err == nil {
		t.Fatal("Production execution accepted")
	}
	if _, _, err := functionExecutionFixtureConfig(func(k string) string { return valid[k] }); err != nil {
		t.Fatal(err)
	}
}
