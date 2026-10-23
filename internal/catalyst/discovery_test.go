// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"net/http"
	"strings"
	"testing"
)

func TestDiscoveryLists(t *testing.T) {
	for _, kind := range []string{"projects", "pools"} {
		t.Run(kind, func(t *testing.T) {
			route := "/baas/v1/project"
			first := `{"id":9007199254740993123,"project_name":"Large"}`
			second := `{"id":"123","project_name":"Small"}`
			if kind == "pools" {
				route += "/456/job_scheduling/jobpool"
				first = `{"id":9007199254740993123,"name":"Large","type":"Webhook"}`
				second = `{"id":"123","name":"Small","type":"Function"}`
			}
			c := testClient(t, Config{AccessToken: "secret", OrganizationID: "789", Environment: "Production", DataCenter: "us"}, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != route || r.URL.RawQuery != "" {
					t.Errorf("unexpected route: %s", r.URL)
				}
				if r.Header.Get("Authorization") != "Zoho-oauthtoken secret" || r.Header.Get("CATALYST-ORG") != "789" || r.Header.Get("Environment") != "Production" {
					t.Error("missing routing/auth headers")
				}
				writeResponse(t, w, `{"status":"success","data":[`+first+`,`+second+`]}`)
			})
			if kind == "projects" {
				items, err := c.ListProjects(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 2 || items[0].ID != "123" || items[1].ID != ID(projectID) {
					t.Fatalf("wrong project list: %+v", items)
				}
			} else {
				items, err := c.ListJobPools(t.Context(), "456")
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 2 || items[0].ID != "123" || items[1].ID != ID(projectID) || items[1].Type != "Webhook" {
					t.Fatalf("wrong pool list: %+v", items)
				}
			}
		})
	}
}

func TestDiscoveryResponses(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		success    bool
	}{
		{"empty", `{"status":"success","data":[]}`, 200, true},
		{"null", `{"status":"success","data":null}`, 200, false},
		{"missing", `{"status":"success"}`, 200, false},
		{"object", `{"status":"success","data":{}}`, 200, false},
		{"failure", `{"status":"failure","data":[]}`, 200, false},
		{"missing_fields", `{"status":"success","data":[{}]}`, 200, false},
		{"bad_id", `{"status":"success","data":[{"id":1.5}]}`, 200, false},
		{"duplicates", `{"status":"success","data":[{"id":"123","project_name":"A","name":"A","type":"Function"},{"id":"123","project_name":"A","name":"A","type":"Function"}]}`, 200, false},
		{"denied", "secret-from-server", 403, false},
		{"missing_project", "secret-from-server", 404, false},
		{"rate_limit", "secret-from-server", 429, false},
		{"oversize", strings.Repeat("x", 1024*1024+1), 200, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); writeResponse(t, w, tc.body) })
			projects, err := c.ListProjects(t.Context())
			if (err == nil) != tc.success {
				t.Fatalf("projects: unexpected error %v", err)
			}
			if tc.success && projects == nil {
				t.Fatal("empty list must not be nil")
			}
			if err != nil && strings.Contains(err.Error(), "secret-from-server") {
				t.Fatal("raw response leaked")
			}
			pools, err := c.ListJobPools(t.Context(), "456")
			if (err == nil) != tc.success {
				t.Fatalf("pools: unexpected error %v", err)
			}
			if tc.success && pools == nil {
				t.Fatal("empty list must not be nil")
			}
			if err != nil && strings.Contains(err.Error(), "secret-from-server") {
				t.Fatal("raw response leaked")
			}
		})
	}
}

func TestJobPoolsRejectInvalidProjectID(t *testing.T) {
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ID caused network request") })
	for _, id := range []string{"", "0", "-1", "1/../../project", "1?x=y", "1.5"} {
		if _, err := c.ListJobPools(t.Context(), id); err == nil {
			t.Errorf("accepted invalid ID %q", id)
		}
	}
}
