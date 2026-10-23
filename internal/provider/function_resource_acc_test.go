// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"errors"
	"fmt"
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

func TestAccFunction(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FUNCTION_CREATE") != "1" {
		t.Skip("requires TF_ACC=1 and explicit CATALYST_TEST_ALLOW_FUNCTION_CREATE=1")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(project) {
		t.Fatal("set an isolated CATALYST_TEST_PROJECT_ID")
	}
	if os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("Function live tests require Development")
	}
	client, err := catalyst.New(catalyst.Config{DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := t.TempDir()
	first := writeFunctionArchive(t, dir, name, "first")
	second := writeFunctionArchive(t, dir, name, "second")
	initial := functionConfig(project, name, first, false)
	updated := functionConfig(project, name, second, true)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(state *terraform.State) error {
		for _, r := range state.RootModule().Resources {
			if r.Type != "catalyst_function" {
				continue
			}
			_, err := client.GetFunction(t.Context(), project, r.Primary.ID)
			if !errors.Is(err, catalyst.ErrFunctionNotFound) {
				return fmt.Errorf("Function %s absence unverified", r.Primary.ID)
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function.test", "memory", "128")},
		{ResourceName: "catalyst_function.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}, ImportStateIdFunc: func(state *terraform.State) (string, error) {
			r := state.RootModule().Resources["catalyst_function.test"]
			if r == nil {
				return "", errors.New("Function missing from state")
			}
			return project + "/" + r.Primary.ID, nil
		}},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_function.test", "memory", "256"), resource.TestCheckResourceAttr("catalyst_function.test", "environment_variables.TEST_VALUE", "fixture"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function.test", "environment_variables.%", "0")},
		{Config: initial, PlanOnly: true},
	}})
}
