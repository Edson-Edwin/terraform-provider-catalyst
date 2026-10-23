// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestInventoryLists(t *testing.T) {
	for _, tc := range []struct {
		endpoint, idKey, nameKey string
		list                     func(*Client, context.Context, string) ([]NamedResource, error)
	}{
		{"table", "table_id", "table_name", (*Client).ListTables},
		{"folder", "id", "folder_name", (*Client).ListFolders},
		{"segment", "id", "segment_name", (*Client).ListCacheSegments},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			good := `{"` + tc.idKey + `":9007199254740993123,"` + tc.nameKey + `":"Large"}`
			small := `{"` + tc.idKey + `":"123","` + tc.nameKey + `":"Small"}`
			for _, scenario := range []struct {
				name, body string
				status     int
				count      int
			}{
				{"sorted", `{"status":"success","data":[` + good + `,` + small + `]}`, 200, 2},
				{"empty", `{"status":"success","data":[]}`, 200, 0},
				{"missing", `{"status":"success"}`, 200, -1},
				{"null", `{"status":"success","data":null}`, 200, -1},
				{"incomplete", `{"status":"success","data":[{}]}`, 200, -1},
				{"duplicate", `{"status":"success","data":[` + good + `,` + good + `]}`, 200, -1},
				{"denied", `server-secret`, 403, -1},
				{"missing_project", `server-secret`, 404, -1},
				{"failure", `{"status":"failure","data":[]}`, 200, -1},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					c := testClient(t, Config{AccessToken: "secret", Environment: "Production", OrganizationID: "789", DataCenter: "us"}, func(w http.ResponseWriter, r *http.Request) {
						if r.Method != "GET" || r.URL.Path != "/baas/v1/project/456/"+tc.endpoint || r.URL.RawQuery != "" {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						}
						if r.Header.Get("Environment") != "Production" || r.Header.Get("CATALYST-ORG") != "789" {
							t.Error("routing lost")
						}
						w.WriteHeader(scenario.status)
						writeResponse(t, w, scenario.body)
					})
					items, err := tc.list(c, t.Context(), "456")
					if scenario.count < 0 {
						if err == nil || strings.Contains(err.Error(), "server-secret") {
							t.Fatalf("expected sanitized error, got %v", err)
						}
					} else {
						if err != nil || items == nil || len(items) != scenario.count {
							t.Fatalf("unexpected result: %+v %v", items, err)
						}
						if len(items) > 0 && (items[0].ID != "123" || items[1].ID != "9007199254740993123" || items[1].Name != "Large") {
							t.Fatal("unstable ordering or lost identity")
						}
					}
				})
			}
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ID reached network") })
			for _, id := range []string{"", "0", "../1", "1?x=y", "1.5"} {
				if _, err := tc.list(c, t.Context(), id); err == nil {
					t.Fatalf("accepted invalid ID %q", id)
				}
			}
		})
	}
}
func TestBucketLists(t *testing.T) {
	good := `{"bucket_name":"z-last","bucket_url":"https://z-last-development.zohostratus.in"}`
	first := `{"bucket_name":"a-first","bucket_url":"https://a-first-development.zohostratus.in"}`
	for _, tc := range []struct {
		name, body    string
		status, count int
	}{
		{"sorted", `{"status":"success","data":[` + good + `,` + first + `]}`, 200, 2},
		{"empty", `{"status":"success","data":[]}`, 200, 0},
		{"null", `{"status":"success","data":null}`, 200, -1},
		{"duplicate", `{"status":"success","data":[` + good + `,` + good + `]}`, 200, -1},
		{"incomplete", `{"status":"success","data":[{}]}`, 200, -1},
		{"bad_url", `{"status":"success","data":[{"bucket_name":"a","bucket_url":"http://example.com"}]}`, 200, -1},
		{"credential_url", `{"status":"success","data":[{"bucket_name":"a","bucket_url":"https://user:secret@example.com"}]}`, 200, -1},
		{"denied", `server-secret`, 403, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/baas/v1/project/456/bucket" {
					t.Error("wrong route")
				}
				w.WriteHeader(tc.status)
				writeResponse(t, w, tc.body)
			})
			items, err := c.ListBuckets(t.Context(), "456")
			if tc.count < 0 {
				if err == nil || strings.Contains(err.Error(), "server-secret") {
					t.Fatalf("expected sanitized error: %v", err)
				}
				return
			}
			if err != nil || items == nil || len(items) != tc.count {
				t.Fatalf("unexpected buckets: %+v %v", items, err)
			}
			if len(items) > 0 && items[0].Name != "a-first" {
				t.Fatal("unstable ordering")
			}
		})
	}
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ID reached network") })
	if _, err := c.ListBuckets(t.Context(), "../1"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}
