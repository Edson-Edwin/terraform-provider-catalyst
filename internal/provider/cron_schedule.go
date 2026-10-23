// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type functionCalendarModel struct {
	Repetition types.String `tfsdk:"repetition"`
	Hour       types.Int64  `tfsdk:"hour"`
	Minute     types.Int64  `tfsdk:"minute"`
	Second     types.Int64  `tfsdk:"second"`
	Days       types.Set    `tfsdk:"days"`
	Months     types.Set    `tfsdk:"months"`
	Weeks      types.Set    `tfsdk:"weeks_of_month"`
	WeekDays   types.Set    `tfsdk:"week_days"`
}

func calendarScheduleTypes() map[string]attr.Type {
	return map[string]attr.Type{"repetition": types.StringType, "hour": types.Int64Type, "minute": types.Int64Type, "second": types.Int64Type, "days": types.SetType{ElemType: types.Int64Type}, "months": types.SetType{ElemType: types.Int64Type}, "weeks_of_month": types.SetType{ElemType: types.Int64Type}, "week_days": types.SetType{ElemType: types.Int64Type}}
}
func calendarScheduleSchema() schema.SingleNestedAttribute {
	attributes := map[string]schema.Attribute{"repetition": schema.StringAttribute{Required: true, MarkdownDescription: "daily, monthly or yearly."}, "hour": schema.Int64Attribute{Required: true, MarkdownDescription: "Hour of day, 0–23 in timezone."}, "minute": schema.Int64Attribute{Required: true, MarkdownDescription: "Minute, 0–59."}, "second": schema.Int64Attribute{Required: true, MarkdownDescription: "Second, 0–59."}}
	for key, description := range map[string]string{"days": "Dates in the month, 1–31. Use dates or the weeks/weekdays pair.", "months": "Months: 1 is January and 12 is December. Required for yearly schedules; omit for daily/monthly.", "weeks_of_month": "Weeks, 1–5. Supply with week_days, instead of days.", "week_days": "Exactly one weekday: 1 Sunday, 2 Monday, 3 Tuesday, 4 Wednesday, 5 Thursday, 6 Friday, 7 Saturday. Multiple weekdays are rejected by the service. Supply with weeks_of_month, instead of days."} {
		attributes[key] = schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type, Default: setdefault.StaticValue(types.SetValueMust(types.Int64Type, []attr.Value{})), MarkdownDescription: description + " Defaults to an empty set."}
	}
	return schema.SingleNestedAttribute{Optional: true, Attributes: attributes, MarkdownDescription: "Daily, monthly or yearly calendar schedule, using the resource timezone. Choose dates or weeks/weekdays for monthly/yearly schedules. Mutually exclusive with expression, one_time_at and interval_seconds."}
}
func calendarScheduleAPI(ctx context.Context, value types.Object) (catalyst.CronSchedule, diag.Diagnostics) {
	var model functionCalendarModel
	diags := value.As(ctx, &model, basetypes.ObjectAsOptions{})
	s := catalyst.CronSchedule{RepetitionType: model.Repetition.ValueString()}
	hour, minute, second := catalyst.Integer(model.Hour.ValueInt64()), catalyst.Integer(model.Minute.ValueInt64()), catalyst.Integer(model.Second.ValueInt64())
	s.Hour, s.Minute, s.Second = &hour, &minute, &second
	for _, part := range []struct {
		value  types.Set
		out    *[]catalyst.Integer
		offset int64
	}{{model.Days, &s.Days, 0}, {model.Months, &s.Months, -1}, {model.Weeks, &s.WeeksOfMonth, 0}, {model.WeekDays, &s.WeekDays, 0}} {
		var values []int64
		diags.Append(part.value.ElementsAs(ctx, &values, false)...)
		for _, v := range values {
			*part.out = append(*part.out, catalyst.Integer(v+part.offset))
		}
	}
	return s, diags
}
func validateCronSchedule(ctx context.Context, data cronScheduleModel, diags *diag.Diagnostics) {
	if data.Expression.IsUnknown() || data.Calendar.IsUnknown() || data.OneTimeAt.IsUnknown() || data.Interval.IsUnknown() {
		return
	}
	count := 0
	for _, set := range []bool{!data.Expression.IsNull(), !data.Calendar.IsNull(), !data.OneTimeAt.IsNull(), !data.Interval.IsNull()} {
		if set {
			count++
		}
	}
	if count != 1 {
		diags.AddAttributeError(path.Root("expression"), "Choose one schedule mode", "Set exactly one of expression, calendar_schedule, one_time_at or interval_seconds.")
		return
	}
	if !data.OneTimeAt.IsNull() {
		if data.OneTimeAt.ValueInt64() <= 0 {
			diags.AddAttributeError(path.Root("one_time_at"), "Invalid execution timestamp", "Use a positive Unix timestamp in seconds.")
		}
		if !data.EndTime.IsNull() && !data.EndTime.IsUnknown() {
			diags.AddAttributeError(path.Root("end_time"), "End time is not a one-time input", "Omit end_time when one_time_at is configured.")
		}
	} else if data.EndTime.IsNull() {
		diags.AddAttributeError(path.Root("end_time"), "Missing recurring end time", "Recurring schedules require a positive end_time in Unix seconds.")
	}
	if !data.Interval.IsNull() {
		if data.Interval.ValueInt64() <= 0 {
			diags.AddAttributeError(path.Root("interval_seconds"), "Invalid interval", "Use a positive interval in seconds.")
		}
		if !data.Timezone.IsNull() && !data.Timezone.IsUnknown() && data.Timezone.ValueString() != "UTC" {
			diags.AddAttributeError(path.Root("timezone"), "Periodic schedules use UTC", "Relative intervals have no wall-clock timezone; configure timezone = UTC.")
		}
	}
	if data.Calendar.IsNull() {
		return
	}
	for _, value := range data.Calendar.Attributes() {
		if value.IsUnknown() {
			return
		}
		if set, ok := value.(types.Set); ok {
			for _, element := range set.Elements() {
				if element.IsUnknown() {
					return
				}
			}
		}
	}
	s, more := calendarScheduleAPI(ctx, data.Calendar)
	diags.Append(more...)
	if more.HasError() {
		return
	}
	if err := catalyst.ValidCalendarSchedule(s); err != nil {
		diags.AddAttributeError(path.Root("calendar_schedule"), "Invalid calendar schedule", err.Error())
	}
}
func calendarScheduleState(ctx context.Context, s catalyst.CronSchedule) (types.Object, diag.Diagnostics) {
	model := functionCalendarModel{Repetition: types.StringValue(s.RepetitionType), Hour: types.Int64Value(int64(*s.Hour)), Minute: types.Int64Value(int64(*s.Minute)), Second: types.Int64Value(int64(*s.Second))}
	var diags diag.Diagnostics
	for _, part := range []struct {
		values []catalyst.Integer
		out    *types.Set
		offset int64
	}{{s.Days, &model.Days, 0}, {s.Months, &model.Months, 1}, {s.WeeksOfMonth, &model.Weeks, 0}, {s.WeekDays, &model.WeekDays, 0}} {
		values := make([]int64, len(part.values))
		for i, v := range part.values {
			values[i] = int64(v) + part.offset
		}
		set, more := types.SetValueFrom(ctx, types.Int64Type, values)
		diags.Append(more...)
		*part.out = set
	}
	value, more := types.ObjectValueFrom(ctx, calendarScheduleTypes(), model)
	diags.Append(more...)
	return value, diags
}

type cronScheduleModel struct {
	Expression types.String
	Calendar   types.Object
	OneTimeAt  types.Int64
	Interval   types.Int64
	EndTime    types.Int64
	Timezone   types.String
}

func applyCronSchedule(ctx context.Context, cron catalyst.Cron, d cronScheduleModel) (catalyst.Cron, diag.Diagnostics) {
	var diags diag.Diagnostics
	count := 0
	for _, present := range []bool{!d.Expression.IsNull(), !d.Calendar.IsNull(), !d.OneTimeAt.IsNull(), !d.Interval.IsNull()} {
		if present {
			count++
		}
	}
	if count != 1 || d.Expression.IsUnknown() || d.Calendar.IsUnknown() || d.OneTimeAt.IsUnknown() || d.Interval.IsUnknown() {
		diags.AddError("Invalid cron schedule", "Exactly one schedule mode must be fully known before applying.")
		return cron, diags
	}
	if !d.Calendar.IsNull() {
		calendar, more := calendarScheduleAPI(ctx, d.Calendar)
		diags.Append(more...)
		if !more.HasError() {
			if err := catalyst.ValidCalendarSchedule(calendar); err != nil {
				diags.AddError("Invalid calendar schedule", err.Error())
			}
		}
		calendar.Timezone = d.Timezone.ValueString()
		cron.Type, cron.Expression, cron.Schedule = "Calendar", "", calendar
	}
	if !d.OneTimeAt.IsNull() {
		if d.OneTimeAt.ValueInt64() <= 0 {
			diags.AddError("Invalid one-time schedule", "Use a positive Unix timestamp in seconds.")
		}
		at := catalyst.Integer(d.OneTimeAt.ValueInt64())
		cron.Type, cron.Expression, cron.EndTime = "OneTime", "", 0
		cron.Schedule = catalyst.CronSchedule{Timezone: d.Timezone.ValueString(), TimeOfExecution: &at}
	}
	if !d.Interval.IsNull() {
		seconds := d.Interval.ValueInt64()
		if seconds <= 0 || d.Timezone.ValueString() != "UTC" {
			diags.AddError("Invalid periodic schedule", "Use a positive interval_seconds and timezone = UTC.")
		}
		hour, minute, second := catalyst.Integer(seconds/3600), catalyst.Integer((seconds%3600)/60), catalyst.Integer(seconds%60)
		cron.Type, cron.Expression = "Periodic", ""
		cron.Schedule = catalyst.CronSchedule{RepetitionType: "every", Hour: &hour, Minute: &minute, Second: &second}
	}
	if d.OneTimeAt.IsNull() && d.EndTime.ValueInt64() <= 0 {
		diags.AddError("Missing recurring end time", "Use a positive end_time in Unix seconds.")
	}
	return cron, diags
}
func readCronSchedule(ctx context.Context, value *catalyst.Cron) (cronScheduleModel, diag.Diagnostics) {
	d := cronScheduleModel{Expression: types.StringValue(value.Expression), EndTime: types.Int64Value(int64(value.EndTime)), Timezone: types.StringValue(value.Detail.Timezone)}
	var diags diag.Diagnostics
	d.Calendar = types.ObjectNull(calendarScheduleTypes())
	d.OneTimeAt = types.Int64Null()
	d.Interval = types.Int64Null()
	if value.Type == "Calendar" {
		d.Expression = types.StringNull()
		calendar, more := calendarScheduleState(ctx, value.Detail)
		diags.Append(more...)
		d.Calendar = calendar
	}
	if value.Type == "OneTime" {
		d.Expression = types.StringNull()
		d.OneTimeAt = types.Int64Value(int64(*value.Detail.TimeOfExecution))
	}
	if value.Type == "Periodic" {
		d.Expression = types.StringNull()
		seconds, err := catalyst.PeriodicScheduleSeconds(value.Detail)
		if err != nil {
			diags.AddError("Invalid periodic schedule metadata", err.Error())
		} else {
			d.Interval = types.Int64Value(seconds)
		}
		d.Timezone = types.StringValue("UTC")
	}
	return d, diags
}

func cronScheduleAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"expression":        schema.StringAttribute{Optional: true, MarkdownDescription: "Five-field cron expression. Set exactly one of expression, calendar_schedule, one_time_at or interval_seconds."},
		"calendar_schedule": calendarScheduleSchema(),
		"one_time_at":       schema.Int64Attribute{Optional: true, MarkdownDescription: "One-time execution Unix timestamp in seconds. Omit end_time for this mode; use a future timestamp when enabling. Select exactly one schedule mode."},
		"end_time":          schema.Int64Attribute{Optional: true, Computed: true, MarkdownDescription: "Required positive end timestamp in Unix seconds for recurring schedules. Omit for one-time schedules; the API end sentinel is computed."},
		"interval_seconds":  schema.Int64Attribute{Optional: true, MarkdownDescription: "Positive relative repetition interval in seconds. Recurring end_time is required. Relative intervals use the default UTC and omit the wall-clock timezone from API requests. Select exactly one schedule mode."},
		"timezone":          schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("UTC"), MarkdownDescription: "IANA wall-clock timezone for expression/calendar/one-time schedules; defaults to UTC. Periodic intervals use UTC because they measure elapsed time."},
	}
}
