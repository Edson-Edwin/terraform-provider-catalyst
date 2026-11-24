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

func TestAccCircuitCron(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_CIRCUIT_CRON_CREATE") != "1" {
		t.Skip("requires explicit disabled Circuit cron creation opt-in")
	}
	project, pool, circuit := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_CIRCUIT_POOL_ID"), os.Getenv("CATALYST_TEST_CIRCUIT_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(pool) || !catalyst.ValidID(circuit) ||
		os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") == "" || !catalyst.ValidID(os.Getenv("CATALYST_ORGANIZATION_ID")) {
		t.Fatal("requires explicit eligible data center, Development environment, organization, disposable project, Circuit Pool and Circuit IDs")
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	endTime := time.Now().Add(48 * time.Hour).Unix()
	initial := circuitCronConfig(project, pool, circuit, name, "0 9 * * *", endTime)
	updated := circuitCronConfig(project, pool, circuit, name, "30 9 * * *", endTime)
	const address = "catalyst_circuit_cron.test"
	var createdID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "enabled", "false"), func(state *terraform.State) error {
				cron := state.RootModule().Resources[address]
				if cron == nil || cron.Primary == nil || !catalyst.ValidID(cron.Primary.ID) {
					return errors.New("created Circuit cron ID missing from state")
				}
				createdID = cron.Primary.ID
				return nil
			})},
			{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) {
				return project + "/" + createdID, nil
			}},
			{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "expression", "30 9 * * *"), func(state *terraform.State) error {
				cron := state.RootModule().Resources[address]
				if cron == nil || cron.Primary == nil || cron.Primary.ID != createdID {
					return errors.New("updating the Circuit cron replaced its ID")
				}
				return nil
			})},
			{Config: updated, PlanOnly: true},
		},
	})
}
