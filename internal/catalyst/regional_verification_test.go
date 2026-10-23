// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"
)

func regionalFixtureEnvironment() map[string]string {
	env := map[string]string{
		"CATALYST_TEST_REGIONS": "all", "CATALYST_TEST_ENVIRONMENTS": "all",
		"CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES": "1",
	}
	hash := sha256.Sum256([]byte("known fixture"))
	for _, dc := range []string{"us", "eu", "in", "au", "ca", "jp", "sa"} {
		for _, environment := range []string{"DEVELOPMENT", "PRODUCTION"} {
			prefix := "CATALYST_TEST_" + strings.ToUpper(dc) + "_" + environment + "_"
			for key, value := range map[string]string{
				"PROJECT_ID": "123", "ACCESS_TOKEN": "secret-access", "CLIENT_ID": "client",
				"CLIENT_SECRET": "secret-client", "REFRESH_TOKEN": "secret-refresh", "ORGANIZATION_ID": "456",
				"STRATUS_BUCKET": "test-bucket", "STRATUS_OBJECT_KEY": "known.bin",
				"STRATUS_SHA256": hex.EncodeToString(hash[:]), "ALLOW_STRATUS_WRITES": "1",
			} {
				env[prefix+key] = value
			}
		}
	}
	return env
}

func TestRegionalFixtureMatrixValidation(t *testing.T) {
	base := regionalFixtureEnvironment()
	fixtures, err := loadRegionalFixtures(func(k string) string { return base[k] }, regionalStratusWrite)
	if err != nil || len(fixtures) != 28 {
		t.Fatalf("expected seven regions, two environments and two auth modes: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range fixtures {
		if seen[f.name] || f.config.DataCenter == "" || f.config.Environment == "" {
			t.Fatal("matrix duplicated or defaulted a destination")
		}
		seen[f.name] = true
	}
	for _, tc := range []struct {
		name, key, value string
	}{
		{"missing-late-region", "CATALYST_TEST_US_PRODUCTION_PROJECT_ID", ""},
		{"missing-auth-mode", "CATALYST_TEST_JP_DEVELOPMENT_ACCESS_TOKEN", ""},
		{"partial-refresh", "CATALYST_TEST_CA_PRODUCTION_CLIENT_SECRET", ""},
		{"invalid-hash", "CATALYST_TEST_SA_PRODUCTION_STRATUS_SHA256", "secret-invalid"},
		{"wrong-bucket", "CATALYST_TEST_AU_PRODUCTION_STRATUS_BUCKET", "https://secret.invalid"},
		{"global-write-opt-in", "CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES", ""},
		{"fixture-write-opt-in", "CATALYST_TEST_EU_PRODUCTION_ALLOW_STRATUS_WRITES", ""},
		{"unknown-region", "CATALYST_TEST_REGIONS", "secret-invalid"},
		{"duplicate-region", "CATALYST_TEST_REGIONS", "in,in"},
		{"empty-region", "CATALYST_TEST_REGIONS", ""},
		{"unknown-environment", "CATALYST_TEST_ENVIRONMENTS", "secret-invalid"},
		{"duplicate-environment", "CATALYST_TEST_ENVIRONMENTS", "Production,Production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := maps.Clone(base)
			env[tc.key] = tc.value
			// Legacy/global credentials must never fill a missing matrix fixture.
			env["CATALYST_ACCESS_TOKEN"] = "secret-global"
			env["CATALYST_TEST_JP_ACCESS_TOKEN"] = "secret-legacy"
			got, err := loadRegionalFixtures(func(k string) string { return env[k] }, regionalStratusWrite)
			if err == nil || got != nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("incomplete matrix or sensitive diagnostic accepted: %v", err)
			}
		})
	}
	t.Run("legacy-subset", func(t *testing.T) {
		env := map[string]string{"CATALYST_TEST_REGIONS": "in", "CATALYST_TEST_IN_PROJECT_ID": "123", "CATALYST_TEST_IN_ACCESS_TOKEN": "secret"}
		got, err := loadRegionalFixtures(func(k string) string { return env[k] }, regionalProjectRead)
		if err != nil || len(got) != 1 || got[0].config.Environment != "Development" {
			t.Fatalf("legacy subset failed: %v", err)
		}
		env["CATALYST_TEST_REQUIRE_BOTH_AUTH_MODES"] = "1"
		if _, err := loadRegionalFixtures(func(k string) string { return env[k] }, regionalProjectRead); err == nil {
			t.Fatal("CI subset silently omitted refresh-mode coverage")
		}
	})
}

type regionalMockState struct {
	puts, deletes int
	exists        bool
	body          []byte
	cancel        context.CancelFunc
}

func regionalStratusMock(t *testing.T, f regionalFixture, key, scenario string) (*Client, *regionalMockState) {
	t.Helper()
	state := &regionalMockState{exists: scenario == "collision"}
	c, err := New(f.config, &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, ""
		respond := func(value any) {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			body = string(raw)
		}
		if r.URL.Host == strings.TrimPrefix(regions[f.config.DataCenter].accounts, "https://") {
			body = `{"access_token":"secret-access","expires_in":3600}`
		} else if strings.Contains(r.URL.Host, ".zohostratus.") {
			if r.Header.Get("Authorization") != "" || r.Header.Get("CATALYST-ORG") != "" || r.Header.Get("CATALYST-ENVIRONMENT") != "" {
				t.Fatal("OAuth or routing headers leaked to signed transfer host")
			}
			if r.Method == http.MethodPut {
				if r.Header.Get("overwrite") != "false" || r.URL.Path != "/_signed/"+key {
					t.Fatal("unsafe fixture upload")
				}
				state.puts++
				state.exists = true
				state.body = regionalStratusPayload(key)
				if scenario == "late-collision" {
					state.body = []byte("someone else's object")
					return &http.Response{StatusCode: http.StatusConflict, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				if state.cancel != nil {
					state.cancel()
				}
				if scenario == "ambiguous-upload" {
					return nil, errors.New("secret transport detail")
				}
			} else if r.URL.Path == "/_signed/"+f.key {
				body = "known fixture"
			} else {
				body = string(state.body)
			}
		} else {
			switch {
			case strings.HasSuffix(r.URL.Path, "/signature"):
				body = stratusSignatureJSON()
			case strings.HasSuffix(r.URL.Path, "/objects"):
				body = `{"status":"success","data":{"contents":[],"truncated":false}}`
			case strings.HasSuffix(r.URL.Path, "/object"):
				if r.Method == http.MethodPut {
					var deletion struct {
						Objects []struct {
							Key string `json:"key"`
						} `json:"objects"`
					}
					if json.NewDecoder(r.Body).Decode(&deletion) != nil || len(deletion.Objects) != 1 || deletion.Objects[0].Key != key || key == f.key {
						t.Fatal("cleanup targeted an unowned object")
					}
					state.deletes++
					if scenario == "cleanup-denied" {
						status = http.StatusForbidden
					} else {
						state.exists = false
					}
					body = `{"status":"success","data":{}}`
				} else if r.URL.Query().Get("object_key") == f.key || state.exists {
					objectKey, size := r.URL.Query().Get("object_key"), len(state.body)
					if objectKey == f.key {
						size = len("known fixture")
					}
					respond(map[string]any{"status": "success", "data": map[string]any{"key": objectKey, "size": size, "content_type": "application/octet-stream", "etag": "opaque"}})
				} else {
					status = http.StatusNotFound
				}
			default:
				if scenario == "permission-denied" {
					status = http.StatusForbidden
				}
				suffix := ""
				if f.config.Environment == "Development" {
					suffix = "-development"
				}
				origin := "https://" + f.bucket + suffix + ".zohostratus." + regions[f.config.DataCenter].stratusSuffix
				respond(map[string]any{"status": "success", "data": []any{map[string]any{"bucket_name": f.bucket, "bucket_url": origin, "bucket_meta": map[string]bool{"versioning": false}}}})
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return c, state
}

func TestRegionalStratusVerificationMatrix(t *testing.T) {
	env := regionalFixtureEnvironment()
	fixtures, err := loadRegionalFixtures(func(k string) string { return env[k] }, regionalStratusWrite)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			c, state := regionalStratusMock(t, f, "tfacc-regional/disposable.bin", "success")
			if err := verifyRegionalStratusRead(t.Context(), c, f); err != nil {
				t.Fatal(err)
			}
			if state.puts != 0 || state.deletes != 0 {
				t.Fatal("read check mutated objects")
			}
			if err := verifyRegionalStratusLifecycle(t.Context(), c, f, "tfacc-regional/disposable.bin"); err != nil {
				t.Fatal(err)
			}
			if state.puts != 1 || state.deletes != 1 || state.exists {
				t.Fatal("lifecycle failed to clean its exact object")
			}
		})
	}
}

func TestRegionalStratusFailureAndCleanup(t *testing.T) {
	env := regionalFixtureEnvironment()
	fixtures, err := loadRegionalFixtures(func(k string) string { return env[k] }, regionalStratusWrite)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"hash-mismatch", "permission-denied", "collision", "late-collision", "ambiguous-upload", "cancel-after-upload", "cleanup-denied"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixtures[0]
			if scenario == "hash-mismatch" {
				f.sha256 = strings.Repeat("0", 64)
			}
			c, state := regionalStratusMock(t, f, "tfacc-regional/disposable.bin", scenario)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancel-after-upload" {
				state.cancel = cancel
			}
			err := verifyRegionalStratusLifecycle(ctx, c, f, "tfacc-regional/disposable.bin")
			if err == nil || strings.Contains(fmt.Sprint(err), "secret") {
				t.Fatalf("unsafe failure result: %v", err)
			}
			if scenario == "hash-mismatch" || scenario == "permission-denied" || scenario == "collision" {
				if state.puts != 0 || state.deletes != 0 {
					t.Fatal("failed preflight mutated or deleted an object")
				}
			} else if scenario == "late-collision" {
				if state.puts != 1 || state.deletes != 0 || !state.exists {
					t.Fatal("cleanup deleted an object that won a create race")
				}
			} else if state.puts != 1 || state.deletes != 1 || (state.exists && scenario != "cleanup-denied") {
				t.Fatal("failed upload/verification did not attempt bounded independent cleanup")
			}
		})
	}
}
