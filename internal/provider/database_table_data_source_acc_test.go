// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"fmt"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"os"
	"testing"
)

func TestAccDatabaseTableDataSources(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live metadata read opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(project) {
		t.Fatal("requires isolated project ID")
	}
	for kind, table := range map[string]string{"nosql_table": os.Getenv("CATALYST_TEST_NOSQL_TABLE_ID"), "datastore_table": os.Getenv("CATALYST_TEST_DATASTORE_TABLE_ID")} {
		t.Run(kind, func(t *testing.T) {
			if !catalyst.ValidID(table) {
				t.Skip("requires corresponding table ID")
			}
			expected := "TerraformProviderNoSQLTests"
			if kind == "datastore_table" {
				expected = "TerraformProviderDataStoreTests"
			}
			config := fmt.Sprintf("provider \"catalyst\" {}\ndata \"catalyst_%s\" \"test\" {\n project_id=%q\n id=%q\n}", kind, project, table)
			resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, Steps: []resource.TestStep{{Config: config, Check: resource.TestCheckResourceAttr("data.catalyst_"+kind+".test", "name", expected)}, {Config: config, PlanOnly: true}}})
		})
	}
}
