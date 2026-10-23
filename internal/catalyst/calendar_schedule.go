// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"errors"
	"math"
)

// ValidCalendarSchedule checks the supported daily/monthly/yearly schedule union.
// Unknown response-only scheduler fields are not inputs to this contract.
func ValidCalendarSchedule(s CronSchedule) error {
	if s.Hour == nil || s.Minute == nil || s.Second == nil || *s.Hour < 0 || *s.Hour > 23 || *s.Minute < 0 || *s.Minute > 59 || *s.Second < 0 || *s.Second > 59 || s.TimeOfExecution != nil {
		return errors.New("calendar schedule requires valid hour, minute and second values")
	}
	for _, values := range []struct {
		values []Integer
		min    Integer
		max    Integer
	}{{s.Days, 1, 31}, {s.Months, 0, 11}, {s.WeeksOfMonth, 1, 5}, {s.WeekDays, 1, 7}} {
		seen := map[Integer]bool{}
		for _, value := range values.values {
			if value < values.min || value > values.max || seen[value] {
				return errors.New("calendar date/week/month values are out of range or duplicated")
			}
			seen[value] = true
		}
	}
	switch s.RepetitionType {
	case "daily":
		if len(s.Days)+len(s.Months)+len(s.WeeksOfMonth)+len(s.WeekDays) != 0 {
			return errors.New("daily calendar schedules cannot specify dates, weeks or months")
		}
	case "monthly", "yearly":
		byDay := len(s.Days) > 0
		byWeek := len(s.WeeksOfMonth) > 0 && len(s.WeekDays) > 0
		if byWeek && len(s.WeekDays) != 1 {
			return errors.New("week-based calendar schedules require exactly one weekday")
		}
		if byDay == byWeek || byDay && (len(s.WeeksOfMonth)+len(s.WeekDays) != 0) || !byDay && !byWeek {
			return errors.New("calendar schedules require either dates or both weeks and weekdays")
		}
		if s.RepetitionType == "yearly" && len(s.Months) == 0 || s.RepetitionType == "monthly" && len(s.Months) != 0 {
			return errors.New("months are required only for yearly calendar schedules")
		}
	default:
		return errors.New("supported calendar repetitions are daily, monthly and yearly")
	}
	return nil
}

// PeriodicScheduleSeconds validates a relative interval without overflow.
func PeriodicScheduleSeconds(s CronSchedule) (int64, error) {
	if s.RepetitionType != "every" || s.Hour == nil || s.Minute == nil || s.Second == nil || *s.Hour < 0 || *s.Minute < 0 || *s.Minute > 59 || *s.Second < 0 || *s.Second > 59 || s.TimeOfExecution != nil || len(s.Days)+len(s.Months)+len(s.WeeksOfMonth)+len(s.WeekDays) != 0 {
		return 0, errors.New("periodic schedules require nonnegative hours, minutes/seconds from 0 to 59 and no calendar fields")
	}
	remainder := int64(*s.Minute)*60 + int64(*s.Second)
	if int64(*s.Hour) > (math.MaxInt64-remainder)/3600 {
		return 0, errors.New("periodic schedule interval exceeds integer range")
	}
	seconds := int64(*s.Hour)*3600 + remainder
	if seconds == 0 {
		return 0, errors.New("periodic schedule interval must be positive")
	}
	return seconds, nil
}
