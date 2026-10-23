// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
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

func TestAccFunctionCron(t *testing.T) { testAccFunctionCron(t, false) }

func TestAccFunctionCronSchedules(t *testing.T) { testAccFunctionCron(t, true) }

func TestAccFunctionCronPeriodic(t *testing.T) { testAccFunctionCron(t, true, "periodic") }

func testAccFunctionCron(t *testing.T, schedules bool, only ...string) {
	t.Helper()
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
	testCase := resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(&CatalystProvider{version: "test", httpClient: liveCronDiagnosticClient(t)})}, CheckDestroy: func(state *terraform.State) error {
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
	}}
	if schedules {
		testCase.Steps = functionCronScheduleSteps(initial, rememberCron, project, only...)
	}
	resource.Test(t, testCase)
}

func functionCronScheduleSteps(initial string, remember resource.TestCheckFunc, project string, only ...string) []resource.TestStep {
	variants := []struct{ name, calendar string }{
		{"daily", `{ repetition = "daily", hour = 1, minute = 5, second = 0 }`},
		{"monthly_dates", `{ repetition = "monthly", hour = 2, minute = 10, second = 5, days = [1, 15] }`},
		{"monthly_weeks", `{ repetition = "monthly", hour = 3, minute = 15, second = 10, weeks_of_month = [1, 3], week_days = [2] }`},
		{"yearly_dates", `{ repetition = "yearly", hour = 4, minute = 20, second = 15, months = [1, 12], days = [10, 20] }`},
		{"yearly_weeks", `{ repetition = "yearly", hour = 5, minute = 25, second = 20, months = [3, 9], weeks_of_month = [2, 4], week_days = [1] }`},
		{"one_time", ""},
		{"periodic", ""},
	}
	steps := []resource.TestStep{}
	for _, variant := range variants {
		if len(only) > 0 && variant.name != only[0] {
			continue
		}
		config := strings.Replace(initial, `expression = "0 0 * * *"`, "calendar_schedule = "+variant.calendar, 1)
		var check resource.TestCheckFunc
		switch variant.name {
		case "one_time":
			config = strings.Replace(initial, `expression = "0 0 * * *"`, "one_time_at = 1893456000", 1)
			config = strings.Replace(config, "end_time = 1893456000", "", 1)
			check = resource.TestCheckResourceAttr("catalyst_function_cron.test", "one_time_at", "1893456000")
		case "periodic":
			config = strings.Replace(initial, `expression = "0 0 * * *"`, "interval_seconds = 21600", 1)
			config = strings.Replace(config, `timezone = "Asia/Kolkata"`, "", 1)
			check = resource.TestCheckResourceAttr("catalyst_function_cron.test", "interval_seconds", "21600")
		default:
			repetition := strings.Split(variant.name, "_")[0]
			check = resource.TestCheckResourceAttr("catalyst_function_cron.test", "calendar_schedule.repetition", repetition)
		}
		updated := regexp.MustCompile(`second = [0-9]+`).ReplaceAllString(config, "second = 30")
		if variant.name == "periodic" {
			updated = strings.Replace(config, "interval_seconds = 21600", "interval_seconds = 7263", 1)
		}
		if variant.name == "one_time" {
			updated = strings.Replace(config, "one_time_at = 1893456000", "one_time_at = 1893542400", 1)
		}
		steps = append(steps, resource.TestStep{Config: config, Check: resource.ComposeTestCheckFunc(check, remember)}, resource.TestStep{Config: updated, Check: remember}, resource.TestStep{ResourceName: "catalyst_function_cron.test", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(state *terraform.State) (string, error) {
			r := state.RootModule().Resources["catalyst_function_cron.test"]
			if r == nil {
				return "", errors.New("cron missing from state")
			}
			return project + "/" + r.Primary.ID, nil
		}}, resource.TestStep{Config: updated, PlanOnly: true})
	}
	return append(steps, resource.TestStep{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function_cron.test", "expression", "0 0 * * *")}, resource.TestStep{Config: initial, PlanOnly: true})
}

// Test diagnostics expose only fixed-format error codes, never response bodies,
// URLs, credentials, parameter values or environment variables.
func liveCronDiagnosticClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || resp.StatusCode != 400 || !strings.Contains(req.URL.Path, "/job_scheduling/cron") {
			return resp, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if readErr == nil {
			var result struct {
				Data struct {
					Code string `json:"error_code"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &result) == nil && regexp.MustCompile(`^[A-Z_]{1,64}$`).MatchString(result.Data.Code) {
				t.Logf("Cron API rejected test fixture with code %s", result.Data.Code)
			}
			for _, field := range []string{"days", "weeks_of_month", "week_day", "months", "repetition_type", "cron_type", "time_of_execution", "timezone", "hour", "minute", "second", "end_time", "job_detail", "cron_execution_type"} {
				if bytes.Contains(body, []byte(field)) {
					t.Logf("Cron error response references schedule field %s", field)
				}
			}
		}
		return resp, nil
	})}
}
