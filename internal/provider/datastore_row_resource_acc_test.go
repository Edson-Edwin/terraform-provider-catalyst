// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccDataStoreRow(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_DATASTORE_ROW_CREATE") != "1" {
		t.Skip("requires isolated Data Store row creation opt-in")
	}
	if os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires Development")
	}
	project, table := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_DATASTORE_TABLE_ID")
	c, err := catalyst.New(catalyst.Config{DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := c.GetDataStoreSchema(t.Context(), project, table)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "TerraformProviderDataStoreTests" {
		t.Fatal("requires dedicated fixture table")
	}
	marker := "tfaccrow" + strconv.FormatInt(time.Now().UnixNano(), 36)
	initial := dataStoreRowConfig(project, table, `{"message":"`+marker+`"}`)
	updated := dataStoreRowConfig(project, table, `{"message":"`+marker+`updated"}`)
	address := "catalyst_datastore_row.test"
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(s *terraform.State) error {
		for _, r := range s.RootModule().Resources {
			if r.Type != "catalyst_datastore_row" {
				continue
			}
			_, err := c.GetDataStoreRow(t.Context(), project, table, r.Primary.Attributes["row_id"])
			if !errors.Is(err, catalyst.ErrDataStoreRowNotFound) {
				return errors.New("row cleanup not confirmed")
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial},
		{ResourceName: address, ImportState: true, ImportStateIdFunc: func(s *terraform.State) (string, error) { return s.RootModule().Resources[address].Primary.ID, nil }, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"values_json"}},
		{Config: updated},
		{Config: dataStoreRowConfig(project, table, `{"message":null}`)},
		{Config: updated},
		{Config: updated, PlanOnly: true},
	}})
}
