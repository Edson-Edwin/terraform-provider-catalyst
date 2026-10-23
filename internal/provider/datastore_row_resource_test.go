// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
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

func dataStoreRowConfig(project, table, values string) string {
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_datastore_row" "test" {
 project_id = %q
 table_id = %q
 values_json = %q
}`, project, table, values)
}

func TestDataStoreRowTerraformLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline")
	var mu sync.Mutex
	var row map[string]json.RawMessage
	creates, deletes := 0, 0
	cleared := false
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		var data any
		switch {
		case strings.HasSuffix(req.URL.Path, "/column"):
			data = []map[string]any{{"column_id": "3", "table_id": "2", "column_name": "message", "data_type": "text", "is_mandatory": false, "is_unique": false, "search_index_enabled": false}}
		case req.URL.Path == "/baas/v1/project/1/table/2":
			data = map[string]string{"table_id": "2", "table_name": "Fixture"}
		case req.Method == "POST" || req.Method == "PATCH":
			var rows []map[string]json.RawMessage
			if json.NewDecoder(req.Body).Decode(&rows) != nil || len(rows) != 1 {
				return nil, errors.New("invalid row payload")
			}
			if req.Method == "POST" {
				creates++
				row = map[string]json.RawMessage{"ROWID": json.RawMessage(`"9007199254740993123"`), "CREATORID": json.RawMessage(`{"email_id":"private@example.test"}`)}
			}
			if row == nil {
				return nil, errors.New("updated missing row")
			}
			for k, v := range rows[0] {
				if k == "number" && string(v) == "1.00" {
					v = json.RawMessage("1")
				}
				row[k] = v
				if k == "label" && string(v) == "null" {
					cleared = true
				}
			}
			data = []any{row}
		case req.Method == "DELETE":
			deletes++
			row = nil
			data = map[string]string{"message": "success"}
		case req.Method == "GET":
			data = row
		default:
			return nil, errors.New("unexpected row request")
		}
		raw, _ := json.Marshal(map[string]any{"status": "success", "data": data})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}}
	address := "catalyst_datastore_row.test"
	initial := dataStoreRowConfig("1", "2", `{"message":"hello","label":"seed","number":1.00}`)
	updated := dataStoreRowConfig("1", "2", `{"message":"updated"}`)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		if row != nil {
			return errors.New("row survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr(address, "row_id", "9007199254740993123")},
		{ResourceName: address, ImportState: true, ImportStateId: "1/2/9007199254740993123", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"values_json"}},
		{Config: updated, Check: func(s *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if !cleared {
				return errors.New("removed field was not cleared")
			}
			for _, v := range s.RootModule().Resources[address].Primary.Attributes {
				if strings.Contains(v, "private@example.test") {
					return errors.New("creator profile leaked")
				}
			}
			return nil
		}},
		{PreConfig: func() { mu.Lock(); row["message"] = json.RawMessage(`"drift"`); mu.Unlock() }, Config: updated},
		{PreConfig: func() { mu.Lock(); row = nil; mu.Unlock() }, Config: updated},
		{Config: updated, PlanOnly: true},
	}})
	if creates != 2 || deletes != 1 {
		t.Fatalf("unexpected lifecycle mutations: %d/%d", creates, deletes)
	}
}

func TestDataStoreRowPermissionFailureRetainsState(t *testing.T) {
	clearEnvironment(t)
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private@example.test"))}, nil
	})}}
	server := providerserver.NewProtocol6(p)()
	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "offline"})})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, configured.Diagnostics)
	state := jsonConfig(t, map[string]any{"id": "1/2/3", "project_id": "1", "table_id": "2", "row_id": "3", "values_json": `{"message":"seed"}`})
	read, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_datastore_row", CurrentState: state})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(read.Diagnostics) {
		t.Fatal("permission denial erased ownership")
	}
	for _, d := range read.Diagnostics {
		if strings.Contains(d.Detail, "private@example.test") {
			t.Fatal("response leaked")
		}
	}
}
