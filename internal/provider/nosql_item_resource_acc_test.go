// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAccNoSQLItem(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_NOSQL_ITEM_CREATE") != "1" {
		t.Skip("requires explicit isolated NoSQL fixture opt-in")
	}
	project, table := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_NOSQL_TABLE_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(table) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") != "in" {
		t.Fatal("requires isolated India Development table")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := client.GetNoSQLTable(t.Context(), project, table)
	if err != nil || metadata.Name != "TerraformProviderNoSQLTests" {
		t.Fatal("requires dedicated TerraformProviderNoSQLTests table")
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Log("Generated disposable fixture partition key:", name)
	raw, _ := json.Marshal(map[string]any{"pk": map[string]string{"S": name}, "sk": map[string]string{"S": "site"}})
	keys := string(raw)
	if _, err = client.ReadNoSQLItem(t.Context(), project, table, keys); !errors.Is(err, catalyst.ErrNoSQLItemNotFound) {
		t.Fatalf("new fixture key absence unverified: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := client.DeleteNoSQLItem(ctx, project, table, keys); err != nil {
			t.Error(err)
		}
	})
	initial, updated := noSQLItemConfig(project, table, name, false), noSQLItemConfig(project, table, name, true)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, err := client.ReadNoSQLItem(ctx, project, table, keys)
		if !errors.Is(err, catalyst.ErrNoSQLItemNotFound) {
			return errors.New("NoSQL fixture item absence unverified")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial},
		{ResourceName: "catalyst_nosql_item.test", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(s *terraform.State) (string, error) {
			return s.RootModule().Resources["catalyst_nosql_item.test"].Primary.ID, nil
		}},
		{Config: updated},
		{Config: initial, Check: func(_ *terraform.State) error {
			value, err := client.ReadNoSQLItem(t.Context(), project, table, keys)
			if err != nil {
				return err
			}
			item, err := catalyst.ParseNoSQLItem(value)
			if err != nil {
				return err
			}
			if _, exists := item["feature"]; exists {
				return errors.New("removed configuration attribute remains")
			}
			return nil
		}},
		{PreConfig: func() {
			value, err := client.ReadNoSQLItem(t.Context(), project, table, keys)
			if err != nil {
				t.Fatal(err)
			}
			item, _ := catalyst.ParseNoSQLItem(value)
			item["value"] = json.RawMessage(`{"S":"external"}`)
			raw, _ := json.Marshal(item)
			if _, err = client.WriteNoSQLItem(t.Context(), project, table, keys, string(raw), false); err != nil {
				t.Fatal(err)
			}
		}, Config: initial},
		{PreConfig: func() {
			if err := client.DeleteNoSQLItem(t.Context(), project, table, keys); err != nil {
				t.Fatal(err)
			}
		}, Config: initial},
		{Config: initial, PlanOnly: true},
	}})
}
