// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFunctionConfigurationUpdateSafety(t *testing.T) {
	for _, kind := range []string{"basicio", "advancedio", "event", "cron", "job"} {
		for _, scenario := range []string{"success", "drift", "identity_changed", "runtime_changed", "denied", "ambiguous", "wrong_response_id", "missing_id", "bad_hash"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				artifact := []byte("PK\x03\x04fixture")
				digest := sha256.Sum256(artifact)
				expected := hex.EncodeToString(digest[:])
				desired := FunctionDeployment{FunctionMetadata: FunctionMetadata{ID: "123", Name: "example", Stack: "node24", Type: kind}}
				desired.Configuration.Memory = 256
				desired.Configuration.Environment.Variables = map[string]string{}
				current := desired
				current.Configuration.Memory = 128
				current.Configuration.Environment.Variables = map[string]string{"REMOVE": "old"}
				writes, requests := 0, 0
				c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
					requests++
					switch {
					case r.Method == "PUT":
						writes++
						endpoint := "function"
						if kind == "advancedio" {
							endpoint = "server"
						}
						if r.URL.Path != "/baas/v1/project/456/"+endpoint+"/upsert" {
							t.Error("incorrect configuration route")
						}
						if err := r.ParseMultipartForm(1 << 20); err != nil {
							t.Error(err)
							return
						}
						defer func() {
							if err := r.MultipartForm.RemoveAll(); err != nil {
								t.Errorf("remove multipart fixture: %v", err)
							}
						}()
						if len(r.MultipartForm.File) != 0 {
							t.Error("configuration update uploaded code")
						}
						var config FunctionConfiguration
						if err := json.Unmarshal([]byte(r.FormValue("configuration")), &config); err != nil {
							t.Error(err)
						}
						if config.Memory != 256 || config.Environment.Variables == nil || len(config.Environment.Variables) != 0 {
							t.Error("configuration must explicitly clear variables")
						}
						if scenario == "ambiguous" {
							w.WriteHeader(503)
							fmt.Fprint(w, "server-secret")
							return
						}
						result := desired
						if scenario == "wrong_response_id" {
							result.ID = "999"
						}
						if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": result}); err != nil {
							t.Error(err)
						}
					case strings.HasSuffix(r.URL.Path, "/download"):
						if scenario == "drift" {
							artifact = append(artifact, 'x')
						}
						if scenario == "denied" {
							w.WriteHeader(403)
							fmt.Fprint(w, "server-secret")
							return
						}
						if _, err := w.Write(artifact); err != nil {
							t.Errorf("write fixture response: %v", err)
						}
					case strings.HasSuffix(r.URL.Path, "/function"):
						if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []FunctionDeployment{current}}); err != nil {
							t.Errorf("encode fixture response: %v", err)
						}
					default:
						result := current
						if scenario == "identity_changed" {
							result.Name = "different"
						}
						if scenario == "runtime_changed" {
							result.Stack = "node22"
						}
						if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": result}); err != nil {
							t.Errorf("encode fixture response: %v", err)
						}
					}
				})
				id := "123"
				if scenario == "missing_id" {
					id = ""
				}
				if scenario == "bad_hash" {
					expected = "invalid"
				}
				result, err := c.UpdateFunctionConfiguration(t.Context(), "456", id, desired, expected)
				if scenario == "success" {
					if err != nil || result == nil || result.ID != "123" {
						t.Fatalf("configuration update failed: %v", err)
					}
				} else if err == nil || strings.Contains(err.Error(), "server-secret") {
					t.Fatalf("expected sanitized failure: %v", err)
				}
				expectedWrites := 0
				if scenario == "success" || scenario == "ambiguous" || scenario == "wrong_response_id" {
					expectedWrites = 1
				}
				if writes != expectedWrites {
					t.Fatalf("writes=%d, want %d (no blind retries)", writes, expectedWrites)
				}
				if (scenario == "missing_id" || scenario == "bad_hash") && requests != 0 {
					t.Fatal("invalid update made requests")
				}
			})
		}
	}
}

func TestFunctionOperationDeadline(t *testing.T) {
	for _, budget := range []time.Duration{20 * time.Millisecond, 10 * time.Minute} {
		parent, cancel := context.WithTimeout(t.Context(), budget)
		ctx, stop := functionOperationContext(parent, time.Minute)
		expected, _ := parent.Deadline()
		actual, _ := ctx.Deadline()
		if !actual.Equal(expected) {
			t.Fatal("nested waiter changed the operation deadline")
		}
		cancel()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("parent cancellation lost")
		}
		stop()
	}
	ctx, cancel := functionOperationContext(context.Background(), time.Minute)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Minute || time.Until(deadline) < 59*time.Second {
		t.Fatal("standalone client call has no bounded fallback")
	}
}

func TestFunctionRefreshReconcilesDrift(t *testing.T) {
	for _, scenario := range []string{"unchanged", "transient_metadata", "metadata_drift", "artifact_drift", "import", "unstable", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			artifact := []byte("PK\x03\x04fixture")
			digest := sha256.Sum256(artifact)
			savedHash := hex.EncodeToString(digest[:])
			saved := FunctionDeployment{FunctionMetadata: FunctionMetadata{ID: "123", Name: "example", Stack: "node24", Type: "job"}}
			saved.Configuration.Memory = 128
			saved.Configuration.Environment.Variables = map[string]string{}
			reads := 0
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/download") {
					if scenario == "artifact_drift" {
						if _, err := w.Write(append(artifact, 'x')); err != nil {
							t.Errorf("write fixture response: %v", err)
						}
					} else {
						if _, err := w.Write(artifact); err != nil {
							t.Errorf("write fixture response: %v", err)
						}
					}
					return
				}
				reads++
				if scenario == "denied" {
					w.WriteHeader(403)
					fmt.Fprint(w, "server-secret")
					return
				}
				current := saved
				if scenario == "metadata_drift" || (scenario == "transient_metadata" && reads == 1) {
					current.Configuration.Memory = 256
				}
				if scenario == "unstable" {
					current.Configuration.Memory = 256
					if reads%2 == 0 {
						current.Configuration.Memory = 512
					}
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": current}); err != nil {
					t.Errorf("encode fixture response: %v", err)
				}
			})
			c.wait = func(ctx context.Context, _ time.Duration) error {
				if scenario == "unstable" && reads == 4 {
					return context.DeadlineExceeded
				}
				return ctx.Err()
			}
			if scenario == "import" {
				savedHash = ""
			}
			current, hash, err := c.ReadStableFunctionDeployment(t.Context(), "456", "123", saved, savedHash)
			if scenario == "unstable" || scenario == "denied" {
				if err == nil || current != nil || strings.Contains(err.Error(), "server-secret") {
					t.Fatal("unstable/denied snapshot accepted")
				}
				return
			}
			if err != nil || current == nil {
				t.Fatalf("refresh failed: %v", err)
			}
			wantReads := 3
			if scenario == "unchanged" {
				wantReads = 1
			}
			if scenario == "transient_metadata" {
				wantReads = 2
			}
			if reads != wantReads {
				t.Fatalf("reads=%d, want=%d", reads, wantReads)
			}
			if scenario == "metadata_drift" && current.Configuration.Memory != 256 {
				t.Fatal("real configuration drift hidden")
			}
			if scenario == "artifact_drift" && hash == savedHash {
				t.Fatal("real code drift hidden")
			}
			if scenario == "transient_metadata" && current.Configuration.Memory != 128 {
				t.Fatal("stale configuration replaced state")
			}
		})
	}
}
