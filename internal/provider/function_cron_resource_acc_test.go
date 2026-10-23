// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
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

func TestAccFunctionCron(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FUNCTION_CRON_CREATE") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FUNCTION_CREATE") != "1" {
		t.Skip("requires explicit Function and disabled Function cron fixture opt-ins")
	}
	project, pool := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_FUNCTION_POOL_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(pool) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") != "in" {
		t.Fatal("requires isolated India Development project and Function Job Pool")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: "in", Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := client.ListFunctions(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	registerFunctionBaselineCheck(t, client, project, baseline)
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	source := writeFunctionArchiveVariant(t, t.TempDir(), name, "scheduled", "node24", "job")
	handler := strings.Replace(functionConfig(project, name, source, false), `resource "catalyst_function" "test" {`, `resource "catalyst_function" "test" {
 stack = "node24"
 type = "job"`, 1)
	config := func(update bool) string {
		cron := functionCronConfig(project, pool, "FUNCTION_REFERENCE", name, update)
		cron = strings.TrimPrefix(cron, "provider \"catalyst\" {}\n")
		cron = strings.Replace(cron, `function_id = "FUNCTION_REFERENCE"`, `function_id = catalyst_function.test.id`, 1)
		return handler + "\n" + cron
	}
	initial, updated := config(false), config(true)
	var latestCronID string
	rememberCron := func(state *terraform.State) error {
		r := state.RootModule().Resources["catalyst_function_cron.test"]
		if r == nil {
			return errors.New("cron missing from state")
		}
		latestCronID = r.Primary.ID
		return nil
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(state *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		for _, r := range state.RootModule().Resources {
			switch r.Type {
			case "catalyst_function_cron":
				_, err := client.GetFunctionCron(ctx, project, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrCronNotFound) {
					return errors.New("Function cron absence unverified")
				}
			case "catalyst_function":
				_, err := client.GetFunction(ctx, project, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrFunctionNotFound) {
					return errors.New("Job Function fixture absence unverified")
				}
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_function_cron.test", "enabled", "false"), rememberCron)},
		{ResourceName: "catalyst_function_cron.test", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(state *terraform.State) (string, error) {
			r := state.RootModule().Resources["catalyst_function_cron.test"]
			if r == nil {
				return "", errors.New("cron missing from state")
			}
			return project + "/" + r.Primary.ID, nil
		}},
		{Config: updated, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "params.test", "value")},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "params.%", "0")},
		{PreConfig: func() {
			current, err := client.GetFunctionCron(t.Context(), project, latestCronID)
			if err != nil {
				t.Fatal(err)
			}
			current.Schedule = current.Detail
			current.Job.Params = map[string]string{"external": "drift"}
			if _, err = client.UpdateCron(t.Context(), project, latestCronID, *current); err != nil {
				t.Fatal(err)
			}
		}, Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "params.%", "0")},
		{PreConfig: func() {
			if err := client.DeleteCron(t.Context(), project, latestCronID); err != nil {
				t.Fatal(err)
			}
			if err := client.WaitFunctionCronDeleted(t.Context(), project, latestCronID); err != nil {
				t.Fatal(err)
			}
		}, Config: initial, Check: rememberCron},
		{Config: initial, PlanOnly: true},
	}})
}
