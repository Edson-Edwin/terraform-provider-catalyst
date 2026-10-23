// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
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

func TestAccFunction(t *testing.T) {
	testAccFunctionVariant(t, "node22", "basicio")
}

func TestAccFunctionVariants(t *testing.T) {
	for _, stack := range []string{"node22", "node24"} {
		for _, kind := range []string{"basicio", "advancedio"} {
			if !t.Run(stack+"/"+kind, func(t *testing.T) { testAccFunctionVariant(t, stack, kind) }) {
				return
			}
		}
	}
}

func TestAccFunctionBackgroundVariants(t *testing.T) {
	for _, stack := range []string{"node22", "node24"} {
		for _, kind := range []string{"event", "cron", "job"} {
			if !t.Run(stack+"/"+kind, func(t *testing.T) { testAccFunctionVariant(t, stack, kind) }) {
				return
			}
		}
	}
}

func testAccFunctionVariant(t *testing.T, stack, kind string) {
	t.Helper()
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
	baseline, err := client.ListFunctions(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// testing cancels t.Context before running cleanup callbacks.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		defer cancel()
		stable := 0
		for {
			current, err := client.ListFunctions(ctx, project)
			matches := err == nil && len(current) == len(baseline)
			if matches {
				for i := range current {
					if current[i].ID != baseline[i].ID {
						matches = false
						break
					}
				}
			}
			if matches {
				stable++
				if stable == 3 {
					return
				}
			} else {
				stable = 0
			}
			select {
			case <-ctx.Done():
				t.Error("Function baseline not restored after bounded reconciliation")
				return
			case <-time.After(2 * time.Second):
			}
		}
	})
	first := writeFunctionArchiveVariant(t, dir, name, "first", stack, kind)
	second := writeFunctionArchiveVariant(t, dir, name, "second", stack, kind)
	config := func(source string, update bool) string {
		return strings.Replace(functionConfig(project, name, source, update), `resource "catalyst_function" "test" {`, fmt.Sprintf("resource \"catalyst_function\" \"test\" {\n stack = %q\n type = %q", stack, kind), 1)
	}
	initial := config(first, false)
	updated := config(second, true)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(state *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Minute)
		defer cancel()
		for _, r := range state.RootModule().Resources {
			if r.Type != "catalyst_function" {
				continue
			}
			_, err := client.GetFunction(ctx, project, r.Primary.ID)
			if !errors.Is(err, catalyst.ErrFunctionNotFound) {
				return fmt.Errorf("Function %s absence unverified", r.Primary.ID)
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_function.test", "memory", "128"), resource.TestCheckResourceAttr("catalyst_function.test", "stack", stack), resource.TestCheckResourceAttr("catalyst_function.test", "type", kind))},
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
