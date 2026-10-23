// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccAppSail(t *testing.T)       { testAccAppSail(t, "node22") }
func TestAccAppSailNode24(t *testing.T) { testAccAppSail(t, "node24") }
func testAccAppSail(t *testing.T, stack string) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CREATE") != "1" {
		t.Skip("requires TF_ACC=1 and explicit CATALYST_TEST_ALLOW_APPSAIL_CREATE=1")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(project) {
		t.Fatal("set an isolated CATALYST_TEST_PROJECT_ID")
	}
	if os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("AppSail live tests require Development")
	}
	client, err := catalyst.New(catalyst.Config{DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := client.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	registerAppSailBaselineCheck(t, client, project, baseline)
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := t.TempDir()
	first := writeAppSailArchive(t, dir, name, "first")
	second := writeAppSailArchive(t, dir, name, "second")
	initial := strings.Replace(appsailConfig(project, name, first, false), `resource "catalyst_appsail" "test" {`, `resource "catalyst_appsail" "test" {`+"\n"+`stack = "`+stack+`"`, 1)
	updated := strings.Replace(appsailConfig(project, name, second, true), `resource "catalyst_appsail" "test" {`, `resource "catalyst_appsail" "test" {`+"\n"+`stack = "`+stack+`"`, 1)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(state *terraform.State) error {
		for _, r := range state.RootModule().Resources {
			if r.Type != "catalyst_appsail" {
				continue
			}
			_, err := client.GetAppSail(t.Context(), project, r.Primary.ID)
			if !errors.Is(err, catalyst.ErrAppSailNotFound) {
				return fmt.Errorf("AppSail %s absence unverified", r.Primary.ID)
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail.test", "memory", "128"), resource.TestCheckResourceAttr("catalyst_appsail.test", "stack", stack))},
		{ResourceName: "catalyst_appsail.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}, ImportStateIdFunc: func(state *terraform.State) (string, error) {
			r := state.RootModule().Resources["catalyst_appsail.test"]
			if r == nil {
				return "", errors.New("AppSail missing from state")
			}
			return project + "/" + r.Primary.ID, nil
		}},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail.test", "memory", "256"), resource.TestCheckResourceAttr("catalyst_appsail.test", "port", "9001"), resource.TestCheckResourceAttr("catalyst_appsail.test", "startup_command", "node --no-warnings index.js"), resource.TestCheckResourceAttr("catalyst_appsail.test", "environment_variables.TEST_VALUE", "fixture"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail.test", "environment_variables.%", "0")},
		{Config: initial, PlanOnly: true},
	}})
}
