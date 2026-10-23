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

func TestAccAppSailCron(t *testing.T) { testAccAppSailCron(t, false) }

func TestAccAppSailCronSchedules(t *testing.T) { testAccAppSailCron(t, true) }

func TestAccAppSailCronDefaultTimezone(t *testing.T) { testAccAppSailCron(t, true, "utc") }

func TestAccAppSailCronPeriodic(t *testing.T) { testAccAppSailCron(t, true, "periodic") }

func testAccAppSailCron(t *testing.T, schedules bool, only ...string) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CRON_CREATE") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CREATE") != "1" {
		t.Skip("requires explicit AppSail and disabled AppSail cron fixture opt-ins")
	}
	project, pool := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_APPSAIL_POOL_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(pool) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") != "in" {
		t.Fatal("requires isolated India Development project and AppSail Job Pool")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: "in", Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := client.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	registerAppSailBaselineCheck(t, client, project, baseline)
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	source := writeAppSailArchive(t, t.TempDir(), name, "scheduled")
	handler := appsailConfig(project, name, source, false)
	config := func(update bool) string {
		cron := appSailCronConfig(project, pool, "APPSAIL_REFERENCE", name, update)
		cron = strings.TrimPrefix(cron, "provider \"catalyst\" {}\n")
		cron = strings.Replace(cron, `appsail_id = "APPSAIL_REFERENCE"`, `appsail_id = catalyst_appsail.test.id`, 1)
		return handler + "\n" + cron
	}
	initial, updated := config(false), config(true)
	if len(only) > 0 && only[0] == "utc" {
		initial = strings.Replace(initial, `timezone = "Asia/Kolkata"`, "", 1)
		updated = strings.Replace(updated, `timezone = "Asia/Kolkata"`, "", 1)
		only = nil
	}
	var latestCronID string
	rememberCron := func(state *terraform.State) error {
		r := state.RootModule().Resources["catalyst_appsail_cron.test"]
		if r == nil {
			return errors.New("cron missing from state")
		}
		latestCronID = r.Primary.ID
		return nil
	}
	testCase := resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(&CatalystProvider{version: "test", httpClient: liveCronDiagnosticClient(t)})}, CheckDestroy: func(state *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		for _, r := range state.RootModule().Resources {
			switch r.Type {
			case "catalyst_appsail_cron":
				_, err := client.GetAppSailCron(ctx, project, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrCronNotFound) {
					return errors.New("AppSail cron absence unverified")
				}
			case "catalyst_appsail":
				_, err := client.GetAppSail(ctx, project, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrAppSailNotFound) {
					return errors.New("AppSail fixture absence unverified")
				}
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "enabled", "false"), rememberCron)},
		{ResourceName: "catalyst_appsail_cron.test", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(state *terraform.State) (string, error) {
			r := state.RootModule().Resources["catalyst_appsail_cron.test"]
			if r == nil {
				return "", errors.New("cron missing from state")
			}
			return project + "/" + r.Primary.ID, nil
		}},
		{Config: updated, Check: resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "params.test", "value")},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "params.%", "0")},
		{PreConfig: func() {
			current, err := client.GetAppSailCron(t.Context(), project, latestCronID)
			if err != nil {
				t.Fatal(err)
			}
			current.Schedule = current.Detail
			current.Job.Params = map[string]string{"external": "drift"}
			if _, err = client.UpdateCron(t.Context(), project, latestCronID, *current); err != nil {
				t.Fatal(err)
			}
		}, Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail_cron.test", "params.%", "0")},
		{PreConfig: func() {
			if err := client.DeleteCron(t.Context(), project, latestCronID); err != nil {
				t.Fatal(err)
			}
			if err := client.WaitAppSailCronDeleted(t.Context(), project, latestCronID); err != nil {
				t.Fatal(err)
			}
		}, Config: initial, Check: rememberCron},
		{Config: initial, PlanOnly: true},
	}}
	if schedules {
		testCase.Steps = cronScheduleTestSteps(initial, rememberCron, project, "catalyst_appsail_cron", only...)
	}
	resource.Test(t, testCase)
}

func registerAppSailBaselineCheck(t *testing.T, client *catalyst.Client, project string, baseline []catalyst.AppSailMetadata) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		stable := 0
		for stable < 3 {
			current, err := client.ListAppSails(ctx, project)
			if err != nil {
				t.Error(err)
				return
			}
			same := len(current) == len(baseline)
			if same {
				for i := range current {
					if current[i].ID != baseline[i].ID {
						same = false
					}
				}
			}
			if same {
				stable++
			} else {
				stable = 0
			}
			if stable == 3 {
				return
			}
			select {
			case <-ctx.Done():
				t.Error("AppSail baseline not restored")
				return
			case <-time.After(2 * time.Second):
			}
		}
	})
}
