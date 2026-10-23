// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDataStoreRowValues(t *testing.T) {
	for _, v := range []string{`{}`, `null`, `{"x":1,"x":2}`, `{"ROWID":"1"}`, `{"creatorid":1}`, `{"x":[]}`, `{"x":{}}`} {
		if _, err := ParseDataStoreValues(v); err == nil {
			t.Fatal("unsafe values accepted")
		}
	}
	v, err := ParseDataStoreValues(`{"n":9007199254740993123,"text":"seed","enabled":true,"empty":null}`)
	if err != nil || string(v["n"]) != "9007199254740993123" {
		t.Fatal("precision lost")
	}
	if !dataStoreScalarEqual(json.RawMessage("1.00"), json.RawMessage("1e0")) {
		t.Fatal("numeric equivalence lost")
	}
	if !DataStoreValuesEquivalent(`{"n":1.00}`, `{"n":1}`) || DataStoreValuesEquivalent(`{"n":9007199254740993123}`, `{"n":9007199254740993124}`) {
		t.Fatal("row equivalence rounded values or rejected equivalent formatting")
	}
}
func TestDataStoreRowAmbiguousWriteNotRetried(t *testing.T) {
	writes := 0
	c := testClient(t, Config{AccessToken: "offline"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			w.WriteHeader(503)
			writeResponse(t, w, "private@example.test")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/column") {
			writeResponse(t, w, `{"status":"success","data":[]}`)
			return
		}
		writeResponse(t, w, `{"status":"success","data":{"table_id":"2","table_name":"Fixture"}}`)
	})
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	_, err := c.CreateDataStoreRow(t.Context(), "1", "2", `{"message":"seed"}`)
	if err == nil || strings.Contains(err.Error(), "private@example.test") || writes != 1 {
		t.Fatal("ambiguous creation mishandled")
	}
}
