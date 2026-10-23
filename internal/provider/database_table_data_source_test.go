// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"io"
	"net/http"
	"strings"
	"testing"
)

const dataStoreColumnsFixture = `{"status":"success","data":[{"column_id":"9007199254740993123","table_id":"789","column_name":"message","data_type":"text","is_mandatory":false,"is_unique":false,"search_index_enabled":true,"max_length":255,"default_value":"excluded-secret"}]}`

func TestDatabaseTableTerraform(t *testing.T) {
	for _, kind := range []string{"nosql_table", "datastore_table"} {
		t.Run(kind, func(t *testing.T) {
			clearEnvironment(t)
			t.Setenv("CATALYST_ACCESS_TOKEN", "test")
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					return nil, fmt.Errorf("unexpected schema mutation")
				}
				body := noSQLMetadataFixture
				if kind == "datastore_table" {
					body = `{"status":"success","data":{"table_id":"789","table_name":"configuration","created_by":{"email":"excluded-secret"}}}`
					if strings.HasSuffix(r.URL.Path, "/column") {
						body = dataStoreColumnsFixture
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			address := "data.catalyst_" + kind + ".test"
			config := fmt.Sprintf("provider \"catalyst\" {}\ndata \"catalyst_%s\" \"test\" {\n project_id=\"123\"\n id=\"789\"\n}", kind)
			resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, Steps: []resource.TestStep{{Config: config, Check: func(s *terraform.State) error {
				r := s.RootModule().Resources[address]
				if r == nil || r.Primary.Attributes["name"] != "configuration" {
					return fmt.Errorf("table name missing")
				}
				raw := r.Primary.Attributes["schema_json"]
				if !json.Valid([]byte(raw)) || strings.Contains(raw, "excluded-secret") {
					return fmt.Errorf("schema was malformed or leaked excluded metadata")
				}
				if kind == "datastore_table" && !strings.Contains(raw, "9007199254740993123") {
					return fmt.Errorf("large column ID lost precision")
				}
				return nil
			}}, {Config: config, PlanOnly: true}}})
		})
	}
}
