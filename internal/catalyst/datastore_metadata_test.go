// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"net/http"
	"strings"
	"testing"
)

func TestDataStoreSchemaRejectsIncompleteAndForeignMetadata(t *testing.T) {
	for _, scenario := range []string{"denied", "null", "foreign_table", "duplicate_column", "foreign_column", "missing_flags"} {
		t.Run(scenario, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
				if scenario == "denied" {
					w.WriteHeader(403)
					writeResponse(t, w, "excluded-secret")
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/column") {
					body := `{"table_id":"456","table_name":"configuration"}`
					if scenario == "null" {
						body = "null"
					}
					if scenario == "foreign_table" {
						body = `{"table_id":"999","table_name":"foreign"}`
					}
					writeResponse(t, w, `{"status":"success","data":`+body+`}`)
					return
				}
				col := `{"column_id":"789","table_id":"456","column_name":"message","data_type":"text","is_mandatory":false,"is_unique":false,"search_index_enabled":false}`
				if scenario == "foreign_column" {
					col = strings.Replace(col, `"table_id":"456"`, `"table_id":"999"`, 1)
				}
				if scenario == "missing_flags" {
					col = strings.Replace(col, `,"is_mandatory":false`, "", 1)
				}
				if scenario == "duplicate_column" {
					col += "," + col
				}
				writeResponse(t, w, `{"status":"success","data":[`+col+`]}`)
			})
			if _, err := c.GetDataStoreSchema(t.Context(), "123", "456"); err == nil || strings.Contains(err.Error(), "excluded-secret") {
				t.Fatal("unsafe schema handling")
			}
		})
	}
}
