// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestAccDataStoreContract(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_DATASTORE_ROW_CREATE") != "1" {
		t.Skip("requires isolated Data Store fixture opt-in")
	}
	p, table := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_DATASTORE_TABLE_ID")
	if !ValidID(p) || !ValidID(table) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated Development table")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := "/baas/v1/project/" + p + "/table/" + table
	call := func(ctx context.Context, method, suffix string, payload any) (json.RawMessage, error) {
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		r, err := c.request(ctx, method, base+suffix, body)
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return nil, apiResponseError("Data Store fixture request", r)
		}
		var e struct {
			Status string          `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if decode(r.Body, &e) != nil || e.Status != "success" {
			return nil, errors.New("invalid Data Store fixture response")
		}
		return e.Data, nil
	}
	raw, err := call(t.Context(), "GET", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		ID   ID     `json:"table_id"`
		Name string `json:"table_name"`
	}
	if json.Unmarshal(raw, &meta) != nil || string(meta.ID) != table || meta.Name != "TerraformProviderDataStoreTests" {
		t.Fatal("requires dedicated Data Store fixture table")
	}
	if _, err = c.GetDataStoreSchema(t.Context(), p, table); err != nil {
		t.Fatal(err)
	}
	raw, err = call(t.Context(), "POST", "/row", []any{map[string]any{"message": "terraform-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 1 {
		t.Fatal("creation response omitted row")
	}
	var id ID
	if json.Unmarshal(rows[0]["ROWID"], &id) != nil || !ValidID(string(id)) {
		t.Fatal("creation response omitted row identity")
	}
	t.Log("Generated Data Store fixture ROWID:", id)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := call(ctx, "DELETE", "/row/"+string(id), nil); err != nil {
			t.Error(err)
		}
		for attempt := 0; attempt < 3; attempt++ {
			data, err := call(ctx, "GET", "/row/"+string(id), nil)
			if err != nil || string(data) != "null" {
				t.Errorf("deleted row absence not verified: %v", err)
				return
			}
			if _, err := c.GetDataStoreSchema(ctx, p, table); err != nil {
				t.Error(err)
				return
			}
		}
	})
	_, err = call(t.Context(), "PATCH", "/row", []any{map[string]any{"ROWID": id, "message": nil}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = call(t.Context(), "GET", "/row/"+string(id), nil)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil || string(row["message"]) != "null" {
		t.Fatal("cleared column was not returned as null")
	}
	t.Log("Data Store PATCH null clearing and row identity verified")
}
