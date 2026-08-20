// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestStratusVersionPagination(t *testing.T) {
	const key = "nested/a ?#&.bin"
	token := ""
	requests := 0
	client := stratusTestClient(t, func(r *http.Request) (int, string, error) {
		requests++
		query := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Host != "api.catalyst.zoho.in" || r.URL.Path != "/baas/v1/project/123/bucket/objects/versions" ||
			query.Get("object_key") != key || query.Get("bucket_name") != "test-bucket" || query.Get("max_versions") != "100" ||
			query.Get("continuation_token") != token || r.Header.Get("CATALYST-ORG") != "123" ||
			r.Header.Get("Authorization") != "Zoho-oauthtoken oauth-secret" || r.Header.Get("Environment") != "Development" {
			t.Fatal("unexpected versions request")
		}
		if requests == 1 {
			token = "page+/=&"
			return 200, `{"status":"success","data":{"key":"nested/a ?#&.bin","truncated":true,"is_truncated":"True","next_continuation_token":"page+/=&","version":[{"version_id":"b","latest":true}]}}`, nil
		}
		return 200, `{"status":"success","data":{"key":"nested/a ?#&.bin","is_truncated":"False","next_continuation_token":"ignored","version":[{"version_id":"a","is_latest":"False"}]}}`, nil
	})
	actual, err := client.ListStratusObjectVersions(t.Context(), "123", "test-bucket", key)
	expected := []StratusVersion{{ID: "a"}, {ID: "b", Latest: true}}
	if err != nil || requests != 2 || !slices.Equal(actual, expected) {
		t.Fatalf("unexpected version listing: %v, %v, requests=%d", actual, err, requests)
	}
}

func TestStratusVersionInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name, page, want string
		status, calls    int
	}{
		{"denied", "sensitive response body", "403", 403, 1},
		{"missing", "sensitive response body", "404", 404, 1},
		{"wrong key", `{"key":"different","truncated":false,"version":[]}`, "unexpected", 200, 1},
		{"no version list", `{"key":"key","truncated":false}`, "unexpected", 200, 1},
		{"no continuation token", `{"key":"key","truncated":true,"version":[]}`, "continuation token", 200, 1},
		{"repeated token", `{"key":"key","truncated":true,"next_continuation_token":"same","version":[]}`, "continuation token", 200, 2},
		{"conflicting flags", `{"key":"key","is_truncated":true,"truncated":false,"version":[]}`, "conflicting", 200, 1},
		{"duplicate version", `{"key":"key","truncated":false,"version":[{"version_id":"one","is_latest":false},{"version_id":"one","is_latest":true}]}`, "duplicate", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := stratusTestClient(t, func(_ *http.Request) (int, string, error) {
				calls++
				if tc.status != 200 {
					return tc.status, tc.page, nil
				}
				return 200, `{"status":"success","data":` + tc.page + `}`, nil
			})
			actual, err := client.ListStratusObjectVersions(t.Context(), "123", "test-bucket", "key")
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "sensitive response body") || actual != nil || calls != tc.calls {
				t.Fatalf("unexpected error, result or request count: %v, %v, %d", err, actual, calls)
			}
		})
	}
}

func TestStratusVersionPageLimit(t *testing.T) {
	calls := 0
	client := stratusTestClient(t, func(r *http.Request) (int, string, error) {
		if calls > 0 && r.URL.Query().Get("continuation_token") != strconv.Itoa(calls) {
			t.Fatal("pagination did not advance")
		}
		calls++
		return 200, fmt.Sprintf(`{"status":"success","data":{"key":"key","version":[],"truncated":true,"next_continuation_token":"%d"}}`, calls), nil
	})
	versions, err := client.ListStratusObjectVersions(t.Context(), "123", "test-bucket", "key")
	if versions != nil || err == nil || !strings.Contains(err.Error(), "exceeded 100 pages") || calls != stratusVersionsMaxPages {
		t.Fatalf("page limit returned %v, %v, %d", versions, err, calls)
	}
}
