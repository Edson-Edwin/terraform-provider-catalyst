// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testGatewayRule() GatewayRule {
	return GatewayRule{ID: "123", Name: "fixture", Source: "/fixture", Endpoint: "/server/function/execute", Target: "basicio", TargetID: "456", Method: "GET", Authentication: []string{"OAuth"}}
}
func TestGatewayReadContracts(t *testing.T) {
	for _, body := range []string{`{"status":"success","data":{"status":false,"scheduled":false}}`, `{"status":"success","data":{"status":false}}`, `{"status":"success","data":null}`, `{"status":"failure","data":{"status":false,"scheduled":false}}`} {
		c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) { writeResponse(t, w, body) })
		result, err := c.GetGateway(t.Context(), "123")
		if (err == nil) != (strings.Contains(body, `"scheduled":false`) && !strings.Contains(body, `"failure"`)) {
			t.Fatalf("invalid status classification: %v", err)
		}
		if err == nil && *result.Enabled {
			t.Fatal("false status was not preserved")
		}
	}
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); writeResponse(t, w, "server-secret") })
	if result, err := c.ListGatewayRules(t.Context(), "123"); result != nil || err == nil || strings.Contains(err.Error(), "server-secret") {
		t.Fatal("disabled/denied rules classified as absence")
	}
}
func TestGatewayPagination(t *testing.T) {
	calls := 0
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		start := r.URL.Query().Get("start")
		if r.URL.Query().Get("num_of_apis") != "200" || r.URL.Query().Get("enhance") != "true" {
			t.Error("wrong pagination query")
		}
		items := []GatewayRule{}
		switch start {
		case "1":
			for i := 1; i <= 200; i++ {
				rule := testGatewayRule()
				rule.ID = ID(fmt.Sprint(9000 + i))
				rule.Name = fmt.Sprintf("r%03d", i)
				items = append(items, rule)
			}
		case "201":
			rule := testGatewayRule()
			rule.ID = "9007199254740993123"
			rule.Name = "r000"
			items = append(items, rule)
		default:
			t.Error("unexpected page")
		}
		raw, err := json.Marshal(map[string]any{"status": "success", "data": items})
		if err != nil {
			t.Fatal(err)
		}
		writeResponse(t, w, string(raw))
	})
	rules, err := c.ListGatewayRules(t.Context(), "456")
	if err != nil || calls != 2 || len(rules) != 201 || rules[0].ID != "9007199254740993123" {
		t.Fatalf("pagination/precision failed: calls=%d count=%d err=%v", calls, len(rules), err)
	}
}
func TestGatewayRulesetSafety(t *testing.T) {
	for _, scenario := range []string{"success", "failed_job", "existing", "unsupported_existing", "denied_list", "ambiguous_write"} {
		t.Run(scenario, func(t *testing.T) {
			writes, reads := 0, 0
			remembered := false
			desired := []GatewayRule{testGatewayRule()}
			desired[0].ID = ""
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "PUT":
					writes++
					if scenario == "ambiguous_write" {
						w.WriteHeader(503)
						writeResponse(t, w, "server-secret")
					} else {
						writeResponse(t, w, `{"status":"success","data":"scheduled"}`)
					}
				case strings.HasSuffix(r.URL.Path, "/schedule"):
					fail := 0
					if scenario == "failed_job" {
						fail = 1
					}
					writeResponse(t, w, fmt.Sprintf(`{"status":"success","data":{"state":4,"create":{"success_count":0,"fail_count":%d}}}`, fail))
				case strings.HasSuffix(r.URL.Path, "/api"):
					if scenario == "denied_list" {
						w.WriteHeader(403)
						writeResponse(t, w, "server-secret")
						return
					}
					items := []GatewayRule{}
					if scenario == "existing" || scenario == "unsupported_existing" || writes > 0 {
						rule := testGatewayRule()
						if scenario == "unsupported_existing" {
							rule.Throttling.Overall = map[string]json.RawMessage{"unknown": json.RawMessage("true")}
						}
						items = append(items, rule)
					}
					raw, err := json.Marshal(map[string]any{"status": "success", "data": items})
					if err != nil {
						t.Fatal(err)
					}
					writeResponse(t, w, string(raw))
				default:
					busy := false
					if writes > 0 {
						reads++
						busy = reads == 1
					}
					writeResponse(t, w, fmt.Sprintf(`{"status":"success","data":{"status":true,"scheduled":%v}}`, busy))
				}
			})
			c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			err := c.SubmitGatewayRules(t.Context(), "456", desired, true, func() error { remembered = true; return nil })
			if (err == nil) != (scenario == "success") {
				t.Fatalf("wrong job result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "server-secret") {
				t.Fatal("secret leaked")
			}
			blocked := scenario == "existing" || scenario == "unsupported_existing" || scenario == "denied_list"
			if blocked && (writes != 0 || remembered) {
				t.Fatal("precondition failed after ownership/write")
			}
			if !blocked && (writes != 1 || !remembered) {
				t.Fatal("mutation replayed or recovery identity missing")
			}
		})
	}
}
func TestGatewayCancellationAndPolicyValidation(t *testing.T) {
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled/invalid operation reached API") })
	c.gatewayWrites <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.SetGatewayEnabled(ctx, "123", true); !errors.Is(err, context.Canceled) {
		t.Fatal("Gateway gate ignored cancellation")
	}
	a, b := testGatewayRule(), testGatewayRule()
	b.Name = "second"
	b.Method = "ANY"
	if err := c.SubmitGatewayRules(t.Context(), "123", []GatewayRule{a, b}, true, nil); err == nil {
		t.Fatal("overlapping ANY route accepted")
	}
	for _, raw := range []string{`{"name":"x","source_endpoint":"/x","target_endpoint":"/server/a/execute","target_id":"123","target":"basicio","method":"GET","new_security_policy":true}`, `{"name":"x","source_endpoint":"/x","target_endpoint":"/server/a/execute","target_id":"123","target":"basicio","method":"GET","throttling":{"new_policy":true}}`} {
		var rule GatewayRule
		if json.Unmarshal([]byte(raw), &rule) != nil || SupportedGatewayRule(rule) {
			t.Fatal("unrepresented policy could be overwritten")
		}
	}
}

func TestGatewayRejectsIncompleteMigrationReports(t *testing.T) {
	for _, data := range []string{`{"state":4}`, `{"state":4,"create":{}}`, `{"state":4,"create":{"success_count":1}}`, `{"state":4,"create":{"success_count":0,"fail_count":-1}}`, `{"state":4,"create":{"success_count":1,"fail_count":0}}`} {
		c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
			writeResponse(t, w, `{"status":"success","data":`+data+`}`)
		})
		err := c.gatewayJobSucceeded(t.Context(), "/baas/v1/project/123/api-gateway")
		if (err == nil) != (data == `{"state":4,"create":{"success_count":1,"fail_count":0}}`) {
			t.Fatal("incomplete job metadata accepted")
		}
	}
}
