// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const noSQLMetadataFixture = `{"status":"success","data":{"id":"789","name":"configuration","status":"ONLINE","partition_key":{"column_name":"pk","data_type":"S"},"sort_key":{"column_name":"sk","data_type":"S"},"ttl_enabled":false,"additional_sort_keys":[],"local_index":[],"global_index":[]}}`

func noSQLItemConfig(project, table, partition string, update bool) string {
	fields := `value = { S = "first" }`
	if update {
		fields = `value = { S = "second" }, feature = { BOOL = true }, number = { N = "900719925474099312345" }, empty = { NULL = true }, binary = { B = "AP8=" }, nested = { M = { list = { L = [{ BOOL = false }, { S = "nested" }] } } }, strings = { SS = ["b", "a"] }, numbers = { SN = ["2", "1"] }, binaries = { SB = ["AP8=", "AQ=="] }`
	}
	return `provider "catalyst" {}
resource "catalyst_nosql_item" "test" {
 project_id = "` + project + `"
 table_id = "` + table + `"
 keys_json = jsonencode({ pk = { S = "` + partition + `" }, sk = { S = "site" } })
 item_json = jsonencode({ pk = { S = "` + partition + `" }, sk = { S = "site" }, ` + fields + ` })
}
`
}
func TestNoSQLItemTerraformLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "test")
	var mu sync.Mutex
	var stored map[string]json.RawMessage
	creates, updates, deletes := 0, 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		body := ""
		if strings.HasSuffix(req.URL.Path, "/nosqltable/789") {
			body = noSQLMetadataFixture
		} else {
			data := map[string]any{"size": 0}
			switch {
			case strings.HasSuffix(req.URL.Path, "/fetch"):
				if stored != nil {
					data["size"] = 85
					data["get"] = []any{map[string]any{"item": stored}}
				}
			case req.Method == "POST":
				var payload []struct {
					Item      map[string]json.RawMessage `json:"item"`
					Condition struct {
						Operator string `json:"operator"`
					} `json:"condition"`
				}
				if json.NewDecoder(req.Body).Decode(&payload) != nil || len(payload) != 1 || payload[0].Condition.Operator != "not_equals" {
					return nil, errors.New("missing atomic duplicate condition")
				}
				status := "CriteriaMismatch"
				if stored == nil {
					stored = payload[0].Item
					creates++
					status = "Success"
				}
				data["size"] = 85
				data["create"] = []any{map[string]any{"status": status}}
			case req.Method == "PUT":
				var payload []struct {
					Operations []struct {
						Type  string          `json:"operation_type"`
						Path  []string        `json:"attribute_path"`
						Value json.RawMessage `json:"update_value"`
					} `json:"update_attributes"`
				}
				if json.NewDecoder(req.Body).Decode(&payload) != nil || len(payload) != 1 {
					return nil, errors.New("invalid update")
				}
				for _, op := range payload[0].Operations {
					if len(op.Path) != 1 {
						return nil, errors.New("invalid attribute path")
					}
					if op.Type == "DELETE" {
						delete(stored, op.Path[0])
					} else {
						stored[op.Path[0]] = op.Value
					}
				}
				updates++
				data["size"] = 85
				data["update"] = []any{map[string]any{"status": "Success", "item": stored}}
			case req.Method == "DELETE":
				stored = nil
				deletes++
				data["size"] = 85
				data["delete"] = []any{map[string]any{"status": "Success"}}
			default:
				return nil, errors.New("unexpected NoSQL request")
			}
			raw, _ := json.Marshal(map[string]any{"status": "success", "data": data})
			body = string(raw)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	initial, updated := noSQLItemConfig("123", "789", "configuration", false), noSQLItemConfig("123", "789", "configuration", true)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if stored != nil {
			return errors.New("NoSQL item survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttrSet("catalyst_nosql_item.test", "id")},
		{ResourceName: "catalyst_nosql_item.test", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(s *terraform.State) (string, error) {
			return s.RootModule().Resources["catalyst_nosql_item.test"].Primary.ID, nil
		}},
		{Config: updated},
		{Config: initial, Check: func(_ *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if _, ok := stored["feature"]; ok {
				return errors.New("removed attribute remained")
			}
			return nil
		}},
		{PreConfig: func() { mu.Lock(); stored["value"] = json.RawMessage(`{"S":"external"}`); mu.Unlock() }, Config: initial},
		{PreConfig: func() { mu.Lock(); stored = nil; mu.Unlock() }, Config: initial},
		{Config: initial, PlanOnly: true},
	}})
	if creates != 2 || updates != 3 || deletes != 1 {
		t.Fatalf("unexpected mutation counts %d/%d/%d", creates, updates, deletes)
	}
}
