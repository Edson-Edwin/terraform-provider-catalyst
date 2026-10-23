// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"net/http"
	"strings"
	"testing"
)

func TestFunctionMetadataContracts(t *testing.T) {
	good := `{"id":9007199254740993123,"name":"large","stack":"node22","type":"basicio","configuration":{"environment":{"variables":{"SECRET":"never retain"}}}}`
	small := `{"id":"123","name":"small","stack":"node20","type":"job"}`
	for _, tc := range []struct {
		name, body string
		status     int
		count      int
	}{
		{"ordered", `{"status":"success","data":[` + good + `,` + small + `]}`, 200, 2},
		{"empty", `{"status":"success","data":[]}`, 200, 0},
		{"null", `{"status":"success","data":null}`, 200, -1},
		{"missing", `{"status":"success","data":[{"id":"123","name":"x"}]}`, 200, -1},
		{"duplicate", `{"status":"success","data":[` + good + `,` + good + `]}`, 200, -1},
		{"denied", "server-secret", 403, -1},
		{"failure", `{"status":"failure","data":[]}`, 200, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "test", Environment: "Production", OrganizationID: "789"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/baas/v1/project/456/function" || r.URL.RawQuery != "" {
					t.Error("wrong request")
				}
				if r.Header.Get("Environment") != "Production" || r.Header.Get("CATALYST-ORG") != "789" {
					t.Error("lost routing")
				}
				w.WriteHeader(tc.status)
				writeResponse(t, w, tc.body)
			})
			result, err := c.ListFunctions(t.Context(), "456")
			if tc.count < 0 {
				if err == nil || strings.Contains(err.Error(), "server-secret") {
					t.Fatalf("unsafe result: %v", err)
				}
				return
			}
			if err != nil || result == nil || len(result) != tc.count {
				t.Fatalf("wrong result: %+v %v", result, err)
			}
			if len(result) > 0 && (result[0].ID != "123" || result[1].ID != "9007199254740993123") {
				t.Fatal("ordering or precision lost")
			}
		})
	}
	for _, body := range []string{good, small, `{"id":"123","name":"x"}`} {
		c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, `{"status":"success","data":`+body+`}`)
		})
		result, err := c.GetFunction(t.Context(), "456", "123")
		if (err == nil) != (body == small) {
			t.Fatalf("identity/metadata validation failed: %+v %v", result, err)
		}
	}
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ID reached network") })
	if _, err := c.ListFunctions(t.Context(), "../123"); err == nil {
		t.Fatal("invalid project accepted")
	}
	if _, err := c.GetFunction(t.Context(), "123", ""); err == nil {
		t.Fatal("missing ID accepted")
	}
}
