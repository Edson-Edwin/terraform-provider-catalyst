// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNoSQLTypedJSONValidation(t *testing.T) {
	for _, raw := range []string{`{"pk":{"S":"config"}}`, `{"number":{"N":"9007199254740993123456789"},"flag":{"BOOL":true},"empty":{"NULL":true},"nested":{"M":{"list":{"L":[{"S":"x"}]}}},"set":{"SS":["b","a"]},"blob":{"B":"AP8="}}`} {
		if _, err := ParseNoSQLItem(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"v":{"N":123}}`, `{"v":{"N":"NaN"}}`, `{"v":{"N":"01"}}`, `{"v":{"BOOL":"true"}}`, `{"v":{"NULL":false}}`, `{"v":{"B":"bad!"}}`, `{"v":{"SS":["x","x"]}}`, `{"v":{"S":"x","N":"1"}}`, `{"v":{"unknown":"x"}}`} {
		if _, err := ParseNoSQLItem(raw); err == nil {
			t.Fatalf("invalid typed JSON accepted: %s", raw)
		}
	}
	a, _ := CanonicalNoSQLItem(`{"v":{"SS":["b","a"]}}`)
	b, _ := CanonicalNoSQLItem(`{"v":{"SS":["a","b"]}}`)
	if a != b {
		t.Fatal("set ordering not normalized")
	}
	id, err := NoSQLItemIdentity("123", "456", `{"pk":{"S":"a/b?secret"}}`)
	if err != nil {
		t.Fatal(err)
	}
	project, table, keys, err := ParseNoSQLItemIdentity(id)
	if err != nil || project != "123" || table != "456" || !strings.Contains(keys, "a/b?secret") {
		t.Fatal("import identity failed")
	}
}
func TestNoSQLWriteStatusAndPermission(t *testing.T) {
	for _, scenario := range []string{"condition_mismatch", "partial_success", "denied_read", "ambiguous_write"} {
		t.Run(scenario, func(t *testing.T) {
			writes := 0
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/nosqltable/456") {
					writeResponse(t, w, `{"status":"success","data":{"id":"456","name":"config","status":"ONLINE","partition_key":{"column_name":"pk","data_type":"S"},"ttl_enabled":false}}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/fetch") {
					if scenario == "denied_read" {
						w.WriteHeader(403)
						writeResponse(t, w, "server-secret")
					} else {
						writeResponse(t, w, `{"status":"success","data":{"size":0}}`)
					}
					return
				}
				writes++
				if scenario == "ambiguous_write" {
					w.WriteHeader(503)
					writeResponse(t, w, "server-secret")
					return
				}
				status := "CriteriaMismatch"
				if scenario == "partial_success" {
					status = "Success"
				}
				raw, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"size": 1, "create": []any{map[string]any{"status": status}}}})
				writeResponse(t, w, string(raw))
			})
			accepted, err := c.WriteNoSQLItem(t.Context(), "123", "456", `{"pk":{"S":"config"}}`, `{"pk":{"S":"config"},"value":{"S":"first"}}`, true)
			if scenario == "partial_success" {
				if err != nil || !accepted {
					t.Fatal("confirmed identity not retained")
				}
				return
			}
			if err == nil || accepted || strings.Contains(err.Error(), "server-secret") {
				t.Fatal("unsafe NoSQL status handling")
			}
			if scenario == "denied_read" && writes != 0 {
				t.Fatal("permission failure led to mutation")
			}
			if scenario == "ambiguous_write" && writes != 1 {
				t.Fatal("ambiguous mutation replayed")
			}
			if scenario == "denied_read" {
				_, err = c.ReadNoSQLItem(t.Context(), "123", "456", `{"pk":{"S":"config"}}`)
				if errors.Is(err, ErrNoSQLItemNotFound) {
					t.Fatal("permission failure mistaken for absence")
				}
			}
		})
	}
}

func TestNoSQLReadNormalizesBooleanWireValues(t *testing.T) {
	for _, wire := range []string{`{"BOOL":"true"}`, `{"M":{"nested":{"L":[{"BOOL":"false"}]}}}`} {
		value, err := normalizeNoSQLReadAttribute(json.RawMessage(wire))
		if err != nil || !validNoSQLAttribute(value, 0) {
			t.Fatalf("valid API boolean rejected: %v", err)
		}
	}
	if _, err := normalizeNoSQLReadAttribute(json.RawMessage(`{"BOOL":"yes"}`)); err == nil {
		t.Fatal("invalid API boolean accepted")
	}
}

func TestNoSQLExactNumericAndBinaryEquivalence(t *testing.T) {
	for _, pair := range [][2]string{{`{"N":"1.00"}`, `{"N":"1e0"}`}, {`{"N":"900719925474099312345"}`, `{"N":"9007199254740993123450e-1"}`}, {`{"N":"-0.000e-999"}`, `{"N":"0"}`}, {`{"B":"AB=="}`, `{"B":"AA=="}`}} {
		if !noSQLValuesEqual(json.RawMessage(pair[0]), json.RawMessage(pair[1])) {
			t.Fatal("equivalent exact values differ")
		}
	}
	for _, raw := range []string{`{"pk":{"S":"first"},"pk":{"S":"second"}}`, `{"nested":{"M":{"v":{"S":"a"},"v":{"S":"b"}}}}`, `{"n":{"SN":["1","1.0"]}}`, `{"b":{"SB":["AA==","AB=="]}}`} {
		if _, err := ParseNoSQLItem(raw); err == nil {
			t.Fatal("duplicate typed JSON/set member accepted")
		}
	}
}
