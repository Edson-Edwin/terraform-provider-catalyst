// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
)

func TestCalendarScheduleValidation(t *testing.T) {
	for _, mode := range []string{"daily", "monthly_dates", "monthly_weeks", "yearly_dates", "yearly_weeks", "missing_clock", "invalid_clock", "mixed_dates", "incomplete_week", "missing_months", "unexpected_months", "duplicate_dates", "invalid_week", "multiple_weekdays", "invalid_api_month", "unknown_repetition"} {
		t.Run(mode, func(t *testing.T) {
			hour, minute, second := Integer(0), Integer(5), Integer(0)
			s := CronSchedule{RepetitionType: "daily", Hour: &hour, Minute: &minute, Second: &second}
			switch mode {
			case "monthly_dates":
				s.RepetitionType = "monthly"
				s.Days = []Integer{1, 15}
			case "monthly_weeks":
				s.RepetitionType = "monthly"
				s.WeeksOfMonth = []Integer{1, 3}
				s.WeekDays = []Integer{2}
			case "yearly_dates":
				s.RepetitionType = "yearly"
				s.Months = []Integer{0, 11}
				s.Days = []Integer{10, 20}
			case "yearly_weeks":
				s.RepetitionType = "yearly"
				s.Months = []Integer{3, 9}
				s.WeeksOfMonth = []Integer{2, 4}
				s.WeekDays = []Integer{1}
			case "missing_clock":
				s.Hour = nil
			case "invalid_clock":
				hour = 24
			case "mixed_dates":
				s.RepetitionType = "monthly"
				s.Days = []Integer{1}
				s.WeeksOfMonth = []Integer{1}
				s.WeekDays = []Integer{1}
			case "incomplete_week":
				s.RepetitionType = "monthly"
				s.WeeksOfMonth = []Integer{1}
			case "missing_months":
				s.RepetitionType = "yearly"
				s.Days = []Integer{1}
			case "unexpected_months":
				s.Months = []Integer{1}
			case "duplicate_dates":
				s.RepetitionType = "monthly"
				s.Days = []Integer{1, 1}
			case "invalid_week":
				s.RepetitionType = "monthly"
				s.WeeksOfMonth = []Integer{6}
				s.WeekDays = []Integer{1}
			case "multiple_weekdays":
				s.RepetitionType = "monthly"
				s.WeeksOfMonth = []Integer{1}
				s.WeekDays = []Integer{1, 2}
			case "invalid_api_month":
				s.RepetitionType = "yearly"
				s.Months = []Integer{12}
				s.Days = []Integer{1}
			case "unknown_repetition":
				s.RepetitionType = "weekly"
			}
			valid := mode == "daily" || mode == "monthly_dates" || mode == "monthly_weeks" || mode == "yearly_dates" || mode == "yearly_weeks"
			if err := ValidCalendarSchedule(s); (err == nil) != valid {
				t.Fatalf("wrong schedule acceptance: %v", err)
			}
		})
	}
}
func TestOneTimeCronSecondsAndResponseNormalization(t *testing.T) {
	enabled := false
	at := Integer(1893456000)
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if _, ok := request["end_time"]; ok {
			t.Error("recurring end time sent to one-time API")
		}
		if _, ok := request["cron_expression"]; ok {
			t.Error("expression sent to one-time API")
		}
		var detail CronSchedule
		if err := json.Unmarshal(request["job_detail"], &detail); err != nil {
			t.Error(err)
		}
		if detail.TimeOfExecution == nil || *detail.TimeOfExecution != at {
			t.Error("one-time timestamp was rescaled")
		}
		writeResponse(t, w, `{"status":"success","data":{"id":"123","cron_name":"test","cron_type":"OneTime","cron_execution_type":"pre-defined","cron_status":false,"job_detail":{"time_of_execution":"1893456000","timezone":"UTC"},"job_meta":{"job_name":"job","jobpool_id":"456","target_type":"Function","target_details":{"id":"789"}}}}`)
	})
	value, err := c.CreateCron(t.Context(), "999", Cron{Name: "test", Type: "OneTime", ExecutionType: "pre-defined", Enabled: &enabled, Schedule: CronSchedule{Timezone: "UTC", TimeOfExecution: &at}, Job: CronJob{Name: "job", PoolID: "456", TargetType: "Function", TargetID: "789"}})
	if err != nil || value.Detail.TimeOfExecution == nil || *value.Detail.TimeOfExecution != at {
		t.Fatalf("one-time normalization failed: %v", err)
	}
}

func TestPeriodicScheduleValidationAndOverflow(t *testing.T) {
	for _, mode := range []string{"valid", "zero", "negative", "invalid_minute", "missing_clock", "wrong_repetition", "calendar_fields", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			hour, minute, second := Integer(2), Integer(1), Integer(3)
			s := CronSchedule{RepetitionType: "every", Hour: &hour, Minute: &minute, Second: &second}
			switch mode {
			case "zero":
				hour, minute, second = 0, 0, 0
			case "negative":
				hour = -1
			case "invalid_minute":
				minute = 60
			case "missing_clock":
				s.Second = nil
			case "wrong_repetition":
				s.RepetitionType = "daily"
			case "calendar_fields":
				s.Days = []Integer{1}
			case "overflow":
				hour = Integer(math.MaxInt64)
			}
			got, err := PeriodicScheduleSeconds(s)
			if (err == nil) != (mode == "valid") || mode == "valid" && got != 7263 {
				t.Fatalf("wrong periodic interval: %d %v", got, err)
			}
		})
	}
}
func TestPeriodicCronRelativeClockAndResponseNormalization(t *testing.T) {
	enabled := false
	hour, minute, second := Integer(2), Integer(1), Integer(3)
	c := testClient(t, Config{AccessToken: "test"}, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Detail map[string]json.RawMessage `json:"job_detail"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if _, ok := request.Detail["timezone"]; ok {
			t.Error("wall-clock timezone sent for a relative interval")
		}
		for key, want := range map[string]string{"hour": "2", "minute": "1", "second": "3"} {
			if string(request.Detail[key]) != want {
				t.Error("periodic interval rescaled")
			}
		}
		writeResponse(t, w, `{"status":"success","data":{"id":"123","cron_name":"test","cron_type":"Periodic","cron_execution_type":"pre-defined","cron_status":false,"end_time":"1893456000","cron_detail":{"repetition_type":"every","hour":2,"minute":1,"second":3},"job_meta":{"job_name":"job","jobpool_id":"456","target_type":"Function","target_details":{"id":"789"}}}}`)
	})
	got, err := c.CreateCron(t.Context(), "999", Cron{Name: "test", Type: "Periodic", ExecutionType: "pre-defined", Enabled: &enabled, EndTime: 1893456000, Schedule: CronSchedule{RepetitionType: "every", Hour: &hour, Minute: &minute, Second: &second}, Job: CronJob{Name: "job", PoolID: "456", TargetType: "Function", TargetID: "789"}})
	if err != nil || got.Detail.Timezone != "UTC" {
		t.Fatalf("periodic normalization failed: %v", err)
	}
}
