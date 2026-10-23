// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestFunctionCronScheduleConfigValidation(t *testing.T) {
	testCronScheduleConfigValidation(t, "catalyst_function_cron")
}

func TestWebhookCronScheduleConfigValidation(t *testing.T) {
	testCronScheduleConfigValidation(t, "catalyst_webhook_cron")
}

func testCronScheduleConfigValidation(t *testing.T, resourceType string) {
	t.Helper()
	for _, mode := range []string{"expression", "daily", "one_time", "missing_mode", "mixed_modes", "one_time_end", "one_time_zero", "missing_recurring_end", "invalid_hour", "incomplete_month", "mixed_month_dates", "invalid_job_name", "periodic", "periodic_zero", "periodic_timezone", "periodic_missing_end"} {
		if mode == "invalid_job_name" && resourceType == "catalyst_webhook_cron" {
			continue
		}
		t.Run(mode, func(t *testing.T) {
			values := map[string]any{"project_id": "123", "job_pool_id": "456", "function_id": "789", "name": "test", "expression": "0 0 * * *", "timezone": "UTC", "end_time": 1893456000, "job_name": "job"}
			if resourceType == "catalyst_webhook_cron" {
				delete(values, "function_id")
				values["url"] = "https://example.com"
			}
			switch mode {
			case "daily", "invalid_hour", "incomplete_month", "mixed_month_dates":
				delete(values, "expression")
				calendar := map[string]any{"repetition": "daily", "hour": 0, "minute": 0, "second": 0}
				values["calendar_schedule"] = calendar
				if mode == "invalid_hour" {
					calendar["hour"] = 24
				}
				if mode == "incomplete_month" {
					calendar["repetition"] = "monthly"
				}
				if mode == "mixed_month_dates" {
					calendar["repetition"] = "monthly"
					calendar["days"] = []int{1}
					calendar["weeks_of_month"] = []int{1}
					calendar["week_days"] = []int{1}
				}
			case "one_time", "one_time_end", "one_time_zero":
				delete(values, "expression")
				if mode != "one_time_end" {
					delete(values, "end_time")
				}
				values["one_time_at"] = 1893456000
				if mode == "one_time_zero" {
					values["one_time_at"] = 0
				}
			case "missing_mode":
				delete(values, "expression")
			case "mixed_modes":
				values["one_time_at"] = 1893456000
			case "missing_recurring_end":
				delete(values, "end_time")
			case "periodic", "periodic_zero", "periodic_timezone", "periodic_missing_end":
				delete(values, "expression")
				values["interval_seconds"] = 7263
				values["timezone"] = "UTC"
				if mode == "periodic_zero" {
					values["interval_seconds"] = 0
				}
				if mode == "periodic_timezone" {
					values["timezone"] = "Asia/Kolkata"
				}
				if mode == "periodic_missing_end" {
					delete(values, "end_time")
				}
			case "invalid_job_name":
				values["job_name"] = "invalid-name"
			}
			server := providerserver.NewProtocol6(New("test")())()
			response, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: resourceType, Config: jsonConfig(t, values)})
			if err != nil {
				t.Fatal(err)
			}
			valid := mode == "expression" || mode == "daily" || mode == "one_time" || mode == "periodic"
			if hasError(response.Diagnostics) == valid {
				t.Fatalf("wrong schedule validation: %v", response.Diagnostics)
			}
		})
	}
}

func TestCronUTCStateAlias(t *testing.T) {
	for _, test := range []struct {
		name      string
		preferred types.String
		expected  string
	}{
		{"default", types.StringValue("UTC"), "UTC"},
		{"explicit_gmt", types.StringValue("GMT"), "GMT"},
		{"import", types.StringNull(), "UTC"},
		{"drift", types.StringValue("Asia/Kolkata"), "GMT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := &catalyst.Cron{Type: "CronExpression", Detail: catalyst.CronSchedule{Timezone: "GMT"}}
			got, diags := readCronSchedule(t.Context(), value, test.preferred)
			if diags.HasError() || got.Timezone.ValueString() != test.expected {
				t.Fatalf("unexpected timezone %s: %v", got.Timezone, diags)
			}
		})
	}
}
