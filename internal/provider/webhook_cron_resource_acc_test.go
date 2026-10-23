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

func runLiveWebhookCron(t *testing.T) {
	t.Helper()
	projectID, poolID := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_JOB_POOL_ID")
	if !catalyst.ValidID(projectID) || !catalyst.ValidID(poolID) {
		t.Fatal("set CATALYST_TEST_PROJECT_ID and CATALYST_TEST_JOB_POOL_ID to an isolated project's Webhook pool")
	}
	if env := os.Getenv("CATALYST_ENVIRONMENT"); env != "" && env != "Development" {
		t.Fatal("live cron tests only run in Development")
	}
	client, err := catalyst.New(catalyst.Config{DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), ClientID: os.Getenv("CATALYST_CLIENT_ID"), ClientSecret: os.Getenv("CATALYST_CLIENT_SECRET"), RefreshToken: os.Getenv("CATALYST_REFRESH_TOKEN"), RedirectURI: os.Getenv("CATALYST_REDIRECT_URI")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	endTime := fmt.Sprint(time.Now().AddDate(1, 0, 0).Unix())
	config := strings.ReplaceAll(webhookCronConfig(projectID, poolID, name, false), "1893456000", endTime)
	updated := strings.ReplaceAll(webhookCronConfig(projectID, poolID, name+"u", true), "1893456000", endTime)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(state *terraform.State) error {
			for _, r := range state.RootModule().Resources {
				if r.Type != "catalyst_webhook_cron" {
					continue
				}
				_, err := client.GetCron(t.Context(), projectID, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrCronNotFound) {
					return fmt.Errorf("test cron %s deletion not verified", r.Primary.ID)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttrSet("catalyst_webhook_cron.test", "id"), resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "enabled", "false"))},
			{ResourceName: "catalyst_webhook_cron.test", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(state *terraform.State) (string, error) {
				r, ok := state.RootModule().Resources["catalyst_webhook_cron.test"]
				if !ok {
					return "", fmt.Errorf("test resource missing")
				}
				return projectID + "/" + r.Primary.ID, nil
			}},
			{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "enabled", "false"), resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "method", "post"))},
			{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "headers.%", "0"), resource.TestCheckResourceAttr("catalyst_webhook_cron.test", "body", ""))},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestAccWebhookCronSchedules(t *testing.T)       { testAccWebhookCronSchedules(t, false) }
func TestAccWebhookCronDefaultTimezone(t *testing.T) { testAccWebhookCronSchedules(t, true) }
func testAccWebhookCronSchedules(t *testing.T, defaultTimezone bool) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_CRON_CREATE") != "1" {
		t.Skip("requires explicit isolated disabled Webhook cron write opt-in")
	}
	project, pool := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_JOB_POOL_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(pool) || os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development project and Webhook pool")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: "in", Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pools, err := client.ListJobPools(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pools {
		if string(p.ID) == pool && p.Type == "Webhook" {
			found = true
		}
	}
	if !found {
		t.Fatal("test pool is not an existing Webhook pool in this project")
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	initial := strings.Replace(webhookCronConfig(project, pool, name, false), `expression = "0 0 1 1 *"`, `expression = "0 0 * * *"`, 1)
	if defaultTimezone {
		initial = strings.Replace(initial, `timezone = "Asia/Kolkata"`, "", 1)
	}
	remember := func(state *terraform.State) error {
		r := state.RootModule().Resources["catalyst_webhook_cron.test"]
		if r == nil {
			return errors.New("Webhook cron missing from state")
		}
		return nil
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(&CatalystProvider{version: "test", httpClient: liveCronDiagnosticClient(t)})}, CheckDestroy: func(state *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		for _, r := range state.RootModule().Resources {
			if r.Type == "catalyst_webhook_cron" {
				_, err := client.GetCron(ctx, project, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrCronNotFound) {
					return errors.New("Webhook cron absence unverified")
				}
			}
		}
		return nil
	}, Steps: cronScheduleTestSteps(initial, remember, project, "catalyst_webhook_cron")})
}
