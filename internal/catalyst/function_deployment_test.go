// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFunctionDeploymentSafety(t *testing.T) {
	desired := FunctionDeployment{FunctionMetadata: FunctionMetadata{Name: "example", Stack: "node22", Type: "basicio"}}
	desired.Configuration.Memory = 128
	desired.Configuration.Environment.Variables = map[string]string{}
	for _, scenario := range []string{"existing_name", "denied_list", "malformed_create", "ambiguous_write"} {
		t.Run(scenario, func(t *testing.T) {
			writes := 0
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					switch scenario {
					case "existing_name":
						writeResponse(t, w, `{"status":"success","data":[{"id":"123","name":"example","stack":"node22","type":"basicio"}]}`)
					case "denied_list":
						w.WriteHeader(403)
						writeResponse(t, w, "server-secret")
					default:
						writeResponse(t, w, `{"status":"success","data":[]}`)
					}
					return
				}
				writes++
				if scenario == "malformed_create" {
					writeResponse(t, w, `{"status":"success","data":{"id":"789","configuration":{"memory":{}}}}`)
				} else {
					w.WriteHeader(503)
					writeResponse(t, w, "server-secret")
				}
			})
			result, err := c.DeployFunction(t.Context(), "456", "", desired, []byte("fixture"))
			if err == nil || strings.Contains(err.Error(), "server-secret") {
				t.Fatalf("expected sanitized failure: %v", err)
			}
			if scenario == "existing_name" || scenario == "denied_list" {
				if writes != 0 {
					t.Fatal("unsafe upsert")
				}
			} else if writes != 1 {
				t.Fatal("write retried")
			}
			if scenario == "malformed_create" && (result == nil || result.ID != "789") {
				t.Fatal("created identity lost")
			}
		})
	}
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) { t.Fatal("cancelled waiter made request") })
	c.deploymentWrites <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.DeployFunction(ctx, "456", "", desired, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
func TestFunctionAbsenceRequiresVerifiedParentAndListing(t *testing.T) {
	for _, scenario := range []string{"absent", "parent_denied", "still_listed", "null_absent", "null_still_listed"} {
		t.Run(scenario, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/baas/v1/project/456":
					if scenario == "parent_denied" {
						w.WriteHeader(403)
						return
					}
					writeResponse(t, w, `{"status":"success","data":{"id":"456","project_name":"test"}}`)
				case "/baas/v1/project/456/function":
					if scenario == "still_listed" || scenario == "null_still_listed" {
						writeResponse(t, w, `{"status":"success","data":[{"id":"123","name":"example","stack":"node22","type":"basicio"}]}`)
					} else {
						writeResponse(t, w, `{"status":"success","data":[]}`)
					}
				default:
					if strings.HasPrefix(scenario, "null_") {
						writeResponse(t, w, `{"status":"success","data":null}`)
					} else {
						w.WriteHeader(404)
						writeResponse(t, w, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
					}
				}
			})
			c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			_, err := c.GetFunctionDeployment(ctx, "456", "123")
			if errors.Is(err, ErrFunctionNotFound) != (scenario == "absent" || scenario == "null_absent") {
				t.Fatalf("wrong absence classification: %v", err)
			}
		})
	}
}

func TestFunctionArtifactPropagationAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			downloads, waits := 0, 0
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/download") {
					downloads++
					if downloads == 1 {
						w.WriteHeader(404)
						return
					}
					writeResponse(t, w, "PK\x03\x04fixture")
					return
				}
				writeResponse(t, w, `{"status":"success","data":{"id":"123","name":"example","stack":"node22","type":"basicio"}}`)
			})
			c.wait = func(ctx context.Context, _ time.Duration) error {
				waits++
				if cancelled {
					return context.Canceled
				}
				return nil
			}
			hash, err := c.FunctionSHA256(t.Context(), "456", "123")
			if cancelled {
				if !errors.Is(err, context.Canceled) || downloads != 1 {
					t.Fatalf("cancellation ignored: %v", err)
				}
			} else if err != nil || len(hash) != 64 || downloads != 2 {
				t.Fatalf("propagation failed: %v", err)
			}
			if waits != 1 {
				t.Fatal("unexpected retry count")
			}
		})
	}
}

func TestFunctionReadSurvivesTransientNullAndMissingListing(t *testing.T) {
	reads, listings := 0, 0
	body := `{"id":"123","name":"example","stack":"node22","type":"basicio","configuration":{"memory":128}}`
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/baas/v1/project/456":
			writeResponse(t, w, `{"status":"success","data":{"id":"456","project_name":"test"}}`)
		case "/baas/v1/project/456/function":
			listings++
			if listings < 3 {
				writeResponse(t, w, `{"status":"success","data":[]}`)
			} else {
				writeResponse(t, w, `{"status":"success","data":[`+body+`]}`)
			}
		default:
			reads++
			if reads == 1 {
				writeResponse(t, w, `{"status":"success","data":null}`)
			} else {
				writeResponse(t, w, `{"status":"success","data":`+body+`}`)
			}
		}
	})
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	result, err := c.GetFunctionDeployment(t.Context(), "456", "123")
	if err != nil || result == nil || result.ID != "123" || reads != 2 {
		t.Fatalf("temporarily hidden Function was lost: %+v %v", result, err)
	}
}

func TestFunctionDeploymentRequiresConsecutiveMatches(t *testing.T) {
	reads, waits := 0, 0
	artifact := "PK\x03\x04fixture"
	digest := sha256.Sum256([]byte(artifact))
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") {
			writeResponse(t, w, artifact)
			return
		}
		reads++
		memory := 256
		if reads == 1 || reads == 3 {
			memory = 128
		}
		writeResponse(t, w, fmt.Sprintf(`{"status":"success","data":{"id":"123","name":"example","stack":"node24","type":"advancedio","configuration":{"memory":%d,"environment":{"variables":{}}}}}`, memory))
	})
	c.wait = func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() }
	desired := FunctionDeployment{FunctionMetadata: FunctionMetadata{Name: "example", Stack: "node24", Type: "advancedio"}}
	desired.Configuration.Memory = 256
	current, err := c.WaitFunctionDeployment(t.Context(), "456", "123", desired, hex.EncodeToString(digest[:]))
	if err != nil || current == nil || reads != 6 || waits != 5 {
		t.Fatalf("stale Function deployment accepted: reads=%d waits=%d err=%v", reads, waits, err)
	}
}
func TestFunctionDeleteReconcilesDefinitive404Only(t *testing.T) {
	for _, status := range []int{404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			writes := 0
			deleted := false
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "DELETE":
					writes++
					if writes == 1 {
						w.WriteHeader(status)
						writeResponse(t, w, "server-secret")
						return
					}
					deleted = true
					writeResponse(t, w, `{"status":"success","data":{"id":"123"}}`)
				case r.URL.Path == "/baas/v1/project/456":
					writeResponse(t, w, `{"status":"success","data":{"id":"456","project_name":"test"}}`)
				case strings.HasSuffix(r.URL.Path, "/function"):
					if deleted {
						writeResponse(t, w, `{"status":"success","data":[]}`)
					} else {
						writeResponse(t, w, `{"status":"success","data":[{"id":"123","name":"fixture","stack":"node22","type":"basicio"}]}`)
					}
				default:
					if deleted {
						writeResponse(t, w, `{"status":"success","data":null}`)
					} else {
						writeResponse(t, w, `{"status":"success","data":{"id":"123","name":"fixture","stack":"node22","type":"basicio"}}`)
					}
				}
			})
			c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			err := c.DeleteFunction(t.Context(), "456", "123")
			if status == 404 {
				if err != nil || writes != 2 {
					t.Fatalf("temporary deletion 404 not reconciled: %v", err)
				}
				if err = c.WaitFunctionDeleted(t.Context(), "456", "123"); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || writes != 1 || strings.Contains(err.Error(), "server-secret") {
				t.Fatal("ambiguous deletion replayed or response body leaked")
			}
		})
	}
}

func TestFunctionManagementRoutesAndLegacyType(t *testing.T) {
	for _, kind := range []string{"basicio", "advancedio", "event", "cron", "job"} {
		t.Run(kind, func(t *testing.T) { testFunctionManagementRoute(t, kind) })
	}
}

func testFunctionManagementRoute(t *testing.T, kind string) {
	t.Helper()
	remoteType, endpoint := kind, "function"
	if kind == "advancedio" {
		remoteType, endpoint = "applogic", "server"
	}
	stored := false
	writes, deletes := 0, 0
	metadata := fmt.Sprintf(`{"id":"123","name":"example","stack":"node24","type":%q,"configuration":{"memory":128,"environment":{"variables":{}}}}`, remoteType)
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT":
			if r.URL.Path != "/baas/v1/project/456/"+endpoint+"/upsert" {
				t.Error("Advanced I/O used generic Function upsert")
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.MultipartForm.RemoveAll() }()
			expectedType := kind
			if kind == "advancedio" {
				expectedType = ""
			}
			if r.FormValue("type") != expectedType {
				t.Error("server upsert must not send generic type")
			}
			stored = true
			writes++
			writeResponse(t, w, `{"status":"success","data":`+metadata+`}`)
		case r.Method == "DELETE":
			if r.URL.Path != "/baas/v1/project/456/"+endpoint+"/123" {
				t.Error("Advanced I/O used generic Function deletion")
			}
			stored = false
			deletes++
			writeResponse(t, w, `{"status":"success","data":{"id":"123"}}`)
		case r.URL.Path == "/baas/v1/project/456/function":
			if stored {
				writeResponse(t, w, `{"status":"success","data":[`+metadata+`]}`)
			} else {
				writeResponse(t, w, `{"status":"success","data":[]}`)
			}
		case r.URL.Path == "/baas/v1/project/456":
			writeResponse(t, w, `{"status":"success","data":{"id":"456","project_name":"Test"}}`)
		default:
			if stored {
				writeResponse(t, w, `{"status":"success","data":`+metadata+`}`)
			} else {
				writeResponse(t, w, `{"status":"success","data":null}`)
			}
		}
	})
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	desired := FunctionDeployment{FunctionMetadata: FunctionMetadata{Name: "example", Stack: "node24", Type: kind}}
	desired.Configuration.Memory = 128
	desired.Configuration.Environment.Variables = map[string]string{}
	for _, id := range []string{"", "123"} {
		value, err := c.DeployFunction(t.Context(), "456", id, desired, []byte("fixture"))
		if err != nil || value == nil || value.Type != kind {
			t.Fatalf("legacy type/routing failure: %v", err)
		}
	}
	if err := c.DeleteFunction(t.Context(), "456", "123"); err != nil {
		t.Fatal(err)
	}
	if err := c.WaitFunctionDeleted(t.Context(), "456", "123"); err != nil {
		t.Fatal(err)
	}
	if writes != 2 || deletes != 1 {
		t.Fatal("unexpected mutation count")
	}
}
