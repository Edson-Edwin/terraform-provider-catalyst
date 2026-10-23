// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata" // Make IANA timezone validation independent of the host OS.
	"unicode/utf8"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &WebhookCronResource{}
var _ resource.ResourceWithImportState = &WebhookCronResource{}
var _ resource.ResourceWithValidateConfig = &WebhookCronResource{}

type WebhookCronResource struct{ client *catalyst.Client }
type webhookCronModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Expression    types.String `tfsdk:"expression"`
	Calendar      types.Object `tfsdk:"calendar_schedule"`
	OneTimeAt     types.Int64  `tfsdk:"one_time_at"`
	Interval      types.Int64  `tfsdk:"interval_seconds"`
	Timezone      types.String `tfsdk:"timezone"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	EndTime       types.Int64  `tfsdk:"end_time"`
	JobPoolID     types.String `tfsdk:"job_pool_id"`
	JobName       types.String `tfsdk:"job_name"`
	URL           types.String `tfsdk:"url"`
	Method        types.String `tfsdk:"method"`
	Headers       types.Map    `tfsdk:"headers"`
	Params        types.Map    `tfsdk:"params"`
	Body          types.String `tfsdk:"body"`
	RetryCount    types.Int64  `tfsdk:"retry_count"`
	RetryInterval types.Int64  `tfsdk:"retry_interval"`
}

func NewWebhookCronResource() resource.Resource { return &WebhookCronResource{} }
func (r *WebhookCronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_cron"
}
func (r *WebhookCronResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	emptyMap := types.MapValueMust(types.StringType, map[string]attr.Value{})
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a pre-defined Webhook Job Scheduling cron using a five-field expression, daily/monthly/yearly calendar, one-time timestamp or relative periodic interval. Exactly one schedule mode is required; timezone defaults to UTC. Recurring schedules require end_time; one-time schedules omit it. Requires an existing Webhook job pool and the `ZohoCatalyst.cron.CREATE`, `.READ`, `.UPDATE`, and `.DELETE` scopes, plus `ZOHOCATALYST.projects.Read` to distinguish an absent cron from lost project access. Crons default to disabled. Enabling a cron schedules real HTTP requests and may incur Catalyst usage charges. Updates and deletion verify target ownership. Deletion waits up to two minutes for confirmed absence and respects cancellation; failures preserve state. Import using `project_id/cron_id`. Secret values are marked sensitive but remain in Terraform state; secure your state storage.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Cron ID as an exact numeric string.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id":     schema.StringAttribute{Required: true, MarkdownDescription: "Existing project ID. Changing it replaces the cron.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Unique cron name in the project."},
			"description":    schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Cron description. Defaults to an empty string."},
			"expression":     schema.StringAttribute{Required: true, MarkdownDescription: "Five-field Catalyst cron expression: minute, hour, day of month, month, day of week. Catalyst validates schedule syntax."},
			"timezone":       schema.StringAttribute{Required: true, MarkdownDescription: "IANA timezone such as Asia/Kolkata or UTC."},
			"enabled":        schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether the cron submits jobs. Defaults to false; set true deliberately to activate the schedule."},
			"end_time":       schema.Int64Attribute{Required: true, MarkdownDescription: "Positive Unix timestamp in seconds at which scheduling ends. Choose a future time when enabling the cron."},
			"job_pool_id":    schema.StringAttribute{Required: true, MarkdownDescription: "ID of an existing Webhook job pool in the project."},
			"job_name":       schema.StringAttribute{Required: true, MarkdownDescription: "Name used for jobs submitted by this cron (1–20 characters)."},
			"url":            schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Absolute HTTP or HTTPS webhook URL, without embedded user credentials or a fragment. Marked sensitive because query parameters may contain secrets."},
			"method":         schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("get"), MarkdownDescription: "Lowercase HTTP method: get, post, put, delete, head, or patch. Defaults to get."},
			"headers":        schema.MapAttribute{Optional: true, Computed: true, Sensitive: true, ElementType: types.StringType, Default: mapdefault.StaticValue(emptyMap), MarkdownDescription: "Webhook headers as string values. Defaults to an empty map."},
			"params":         schema.MapAttribute{Optional: true, Computed: true, Sensitive: true, ElementType: types.StringType, Default: mapdefault.StaticValue(emptyMap), MarkdownDescription: "Webhook parameters as string values. Defaults to an empty map."},
			"body":           schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Webhook request body. Defaults to an empty string."},
			"retry_count":    schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), MarkdownDescription: "Number of retries for failed jobs. Defaults to zero; Catalyst validates its service limits."},
			"retry_interval": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), MarkdownDescription: "Retry interval in seconds. Use 60 to 86400 seconds with retry_count greater than zero; otherwise leave zero."},
		},
	}
	for name, attribute := range cronScheduleAttributes() {
		resp.Schema.Attributes[name] = attribute
	}
}
func (r *WebhookCronResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client. Please report this provider bug.")
		return
	}
	r.client = client
}
func (r *WebhookCronResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data webhookCronModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateCronFields(data, resp)
	validateCronSchedule(ctx, data.schedule(), &resp.Diagnostics)
}

func validateCronFields(data webhookCronModel, resp *resource.ValidateConfigResponse) {
	for key, value := range map[string]types.String{"project_id": data.ProjectID, "job_pool_id": data.JobPoolID} {
		if !value.IsNull() && !value.IsUnknown() && !catalyst.ValidID(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(key), "Invalid ID", "Specify a positive integer written as a string.")
		}
	}
	for key, value := range map[string]types.String{"name": data.Name, "job_name": data.JobName} {
		if !value.IsNull() && !value.IsUnknown() && strings.TrimSpace(value.ValueString()) == "" {
			resp.Diagnostics.AddAttributeError(path.Root(key), "Empty name", "Specify a non-empty name.")
		}
	}
	if !data.JobName.IsNull() && !data.JobName.IsUnknown() && utf8.RuneCountInString(data.JobName.ValueString()) > 20 {
		resp.Diagnostics.AddAttributeError(path.Root("job_name"), "Job name too long", "Use at most 20 characters.")
	}
	if !data.Expression.IsNull() && !data.Expression.IsUnknown() && len(strings.Fields(data.Expression.ValueString())) != 5 {
		resp.Diagnostics.AddAttributeError(path.Root("expression"), "Invalid cron expression", "Specify five whitespace-separated fields: minute, hour, day of month, month, day of week.")
	}
	if !data.Timezone.IsNull() && !data.Timezone.IsUnknown() {
		zone := data.Timezone.ValueString()
		_, err := time.LoadLocation(zone)
		if err != nil || zone == "" || zone == "Local" {
			resp.Diagnostics.AddAttributeError(path.Root("timezone"), "Invalid timezone", "Specify an IANA timezone such as Asia/Kolkata or UTC.")
		}
	}
	if !data.URL.IsNull() && !data.URL.IsUnknown() {
		u, err := url.Parse(data.URL.ValueString())
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
			resp.Diagnostics.AddAttributeError(path.Root("url"), "Invalid webhook URL", "Specify an absolute HTTP or HTTPS URL without user credentials or a fragment.")
		}
	}
	if !data.Method.IsNull() && !data.Method.IsUnknown() {
		switch data.Method.ValueString() {
		case "get", "post", "put", "delete", "head", "patch":
		default:
			resp.Diagnostics.AddAttributeError(path.Root("method"), "Invalid HTTP method", "Use get, post, put, delete, head, or patch.")
		}
	}
	if !data.EndTime.IsNull() && !data.EndTime.IsUnknown() && data.EndTime.ValueInt64() <= 0 {
		resp.Diagnostics.AddAttributeError(path.Root("end_time"), "Invalid end time", "Specify a positive Unix timestamp in seconds.")
	}
	for key, value := range map[string]types.Int64{"retry_count": data.RetryCount, "retry_interval": data.RetryInterval} {
		if !value.IsNull() && !value.IsUnknown() && value.ValueInt64() < 0 {
			resp.Diagnostics.AddAttributeError(path.Root(key), "Invalid retry value", "Retry values cannot be negative.")
		}
	}
	if !data.RetryCount.IsUnknown() && !data.RetryInterval.IsUnknown() {
		count, interval := data.RetryCount.ValueInt64(), data.RetryInterval.ValueInt64()
		if (count > 0 && (interval < 60 || interval > 86400)) || (count == 0 && interval > 0) {
			resp.Diagnostics.AddAttributeError(path.Root("retry_interval"), "Inconsistent retry settings", "Use retry_interval from 60 to 86400 seconds when retry_count is positive; otherwise use zero for both.")
		}
	}
}
func (d webhookCronModel) api(ctx context.Context) (catalyst.Cron, diag.Diagnostics) {
	var diags diag.Diagnostics
	headers, params := map[string]string{}, map[string]string{}
	diags.Append(d.Headers.ElementsAs(ctx, &headers, false)...)
	diags.Append(d.Params.ElementsAs(ctx, &params, false)...)
	enabled := d.Enabled.ValueBool()
	cron := catalyst.Cron{Name: d.Name.ValueString(), Description: d.Description.ValueString(), Type: "CronExpression", ExecutionType: "pre-defined", Expression: d.Expression.ValueString(), Enabled: &enabled, EndTime: catalyst.Integer(d.EndTime.ValueInt64()), Schedule: catalyst.CronSchedule{Timezone: d.Timezone.ValueString()}, Job: catalyst.CronJob{Name: d.JobName.ValueString(), PoolID: catalyst.ID(d.JobPoolID.ValueString()), TargetType: "Webhook", SourceType: "Cron", URL: d.URL.ValueString(), Method: d.Method.ValueString(), Headers: headers, Params: params, Body: d.Body.ValueString(), Retry: catalyst.CronRetry{Count: catalyst.Integer(d.RetryCount.ValueInt64()), Interval: catalyst.Integer(d.RetryInterval.ValueInt64())}}}
	scheduled, more := applyCronSchedule(ctx, cron, d.schedule())
	diags.Append(more...)
	return scheduled, diags
}
func (d *webhookCronModel) populate(ctx context.Context, c *catalyst.Cron) diag.Diagnostics {
	var diags diag.Diagnostics
	d.ID = types.StringValue(string(c.ID))
	d.Name = types.StringValue(c.Name)
	d.Description = types.StringValue(c.Description)
	schedule, more := readCronSchedule(ctx, c, d.Timezone)
	diags.Append(more...)
	d.Expression, d.Calendar, d.OneTimeAt, d.Interval, d.Timezone = schedule.Expression, schedule.Calendar, schedule.OneTimeAt, schedule.Interval, schedule.Timezone
	d.Enabled = types.BoolValue(*c.Enabled)
	d.EndTime = types.Int64Value(int64(c.EndTime))
	d.JobPoolID = types.StringValue(string(c.Job.PoolID))
	d.JobName = types.StringValue(c.Job.Name)
	d.URL = types.StringValue(c.Job.URL)
	d.Method = types.StringValue(c.Job.Method)
	d.Body = types.StringValue(c.Job.Body)
	d.RetryCount = types.Int64Value(int64(c.Job.Retry.Count))
	d.RetryInterval = types.Int64Value(int64(c.Job.Retry.Interval))
	if c.Job.Retry.Count == 0 {
		d.RetryInterval = types.Int64Value(0)
	}
	if c.Job.Headers == nil {
		c.Job.Headers = map[string]string{}
	}
	if c.Job.Params == nil {
		c.Job.Params = map[string]string{}
	}
	headers, hd := types.MapValueFrom(ctx, types.StringType, c.Job.Headers)
	params, pd := types.MapValueFrom(ctx, types.StringType, c.Job.Params)
	diags.Append(hd...)
	diags.Append(pd...)
	d.Headers = headers
	d.Params = params
	return diags
}
func (r *WebhookCronResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing a cron.")
		return false
	}
	return true
}
func (r *WebhookCronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data webhookCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	cron, diags := data.api(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	result, err := r.client.CreateCron(ctx, data.ProjectID.ValueString(), cron)
	if result != nil && catalyst.ValidID(string(result.ID)) {
		data.ID = types.StringValue(string(result.ID))
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Catalyst webhook cron", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, result)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *WebhookCronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data webhookCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	cron, err := r.client.GetCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst webhook cron", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, cron)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *WebhookCronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data webhookCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if _, err := r.client.GetCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to verify current Webhook cron", err.Error())
		return
	}
	cron, diags := data.api(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	result, err := r.client.UpdateCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString(), cron)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Catalyst webhook cron", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, result)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *WebhookCronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data webhookCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err := r.client.GetCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to verify current Webhook cron", err.Error())
		return
	}
	err = r.client.DeleteCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete Catalyst webhook cron", err.Error())
		return
	}
	if err := r.client.WaitCronDeleted(ctx, data.ProjectID.ValueString(), data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to verify cron deletion", "Terraform retained the cron in state because deletion could not be confirmed: "+err.Error())
	}
}
func (r *WebhookCronResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids := strings.Split(req.ID, "/")
	if len(ids) != 2 || !catalyst.ValidID(ids[0]) || !catalyst.ValidID(ids[1]) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use project_id/cron_id with two positive numeric IDs.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1])...)
}

func (d webhookCronModel) schedule() cronScheduleModel {
	return cronScheduleModel{Expression: d.Expression, Calendar: d.Calendar, OneTimeAt: d.OneTimeAt, Interval: d.Interval, EndTime: d.EndTime, Timezone: d.Timezone}
}
