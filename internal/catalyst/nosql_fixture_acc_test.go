// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Recovery is restricted to recent generated keys in a named disposable table.
// Its separately prepared value index avoids an unsupported whole-table scan.
func TestAccNoSQLFixtureRecovery(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FIXTURE_RECOVERY") != "1" {
		t.Skip("requires explicit fixture recovery opt-in")
	}
	project, table, index := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_NOSQL_TABLE_ID"), os.Getenv("CATALYST_TEST_NOSQL_FIXTURE_INDEX_ID")
	if !ValidID(project) || !ValidID(table) || !ValidID(index) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated Development table/index")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := c.GetNoSQLTable(t.Context(), project, table)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "TerraformProviderNoSQLTests" {
		t.Fatal("requires dedicated test table")
	}
	found := false
	for _, i := range meta.Global {
		if string(i.ID) == index && i.Name == "TerraformProviderFixtureValue" && i.Status == "ONLINE" {
			found = true
		}
	}
	if !found {
		t.Fatal("requires verified ONLINE fixture lookup index")
	}
	removed := 0
	for _, value := range []string{"first", "second", "external"} {
		body, _ := json.Marshal(map[string]any{"key_condition": map[string]any{"attribute": []string{"value"}, "operator": "equals", "value": map[string]string{"S": value}}, "consistent_read": true, "limit": 100})
		res, err := c.request(t.Context(), "POST", "/baas/v1/project/"+project+"/nosqltable/"+table+"/index/"+index+"/item/query", body)
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			Status string `json:"status"`
			Data   struct {
				Get      []noSQLEntry               `json:"get"`
				StartKey map[string]json.RawMessage `json:"start_key"`
			} `json:"data"`
		}
		if res.StatusCode != 200 {
			err = apiResponseError("fixture index query", res)
			_ = res.Body.Close()
			t.Fatal(err)
		}
		err = decode(res.Body, &e)
		_ = res.Body.Close()
		if err != nil || e.Status != "success" || len(e.Data.Get) > 10 || len(e.Data.StartKey) > 0 {
			t.Fatal("fixture recovery query was incomplete or unexpectedly large")
		}
		for _, entry := range e.Data.Get {
			var pk, sk struct {
				S string `json:"S"`
			}
			if json.Unmarshal(entry.Item["pk"], &pk) != nil || json.Unmarshal(entry.Item["sk"], &sk) != nil || sk.S != "site" || !strings.HasPrefix(pk.S, "tfacc") {
				t.Fatal("index returned a foreign item; no further deletions attempted")
			}
			timestamp, err := strconv.ParseInt(strings.TrimPrefix(pk.S, "tfacc"), 36, 64)
			age := time.Since(time.Unix(0, timestamp))
			if err != nil || age < 0 || age > 6*time.Hour {
				t.Fatal("item is not a recent generated fixture")
			}
			keys, _ := json.Marshal(map[string]json.RawMessage{"pk": entry.Item["pk"], "sk": entry.Item["sk"]})
			if err = c.DeleteNoSQLItem(t.Context(), project, table, string(keys)); err != nil {
				t.Fatal(err)
			}
			removed++
		}
	}
	t.Log("Generated fixture item deletions confirmed:", removed)
}
