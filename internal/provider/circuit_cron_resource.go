// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type CircuitCronResource struct{ WebhookCronResource }

type circuitCronModel struct {
	RoutingScope  types.Object `tfsdk:"routing_scope"`
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	CircuitID     types.String `tfsdk:"circuit_id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Expression    types.String `tfsdk:"expression"`
	Timezone      types.String `tfsdk:"timezone"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	EndTime       types.Int64  `tfsdk:"end_time"`
	JobPoolID     types.String `tfsdk:"job_pool_id"`
	JobName       types.String `tfsdk:"job_name"`
	RetryCount    types.Int64  `tfsdk:"retry_count"`
	RetryInterval types.Int64  `tfsdk:"retry_interval"`
}

func NewCircuitCronResource() resource.Resource {
	return withEnvironmentScope(&CircuitCronResource{})
}

func (r *CircuitCronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_circuit_cron"
}

func (r *CircuitCronResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.WebhookCronResource.Schema(ctx, req, resp)
	attributes := resp.Schema.Attributes
	for _, field := range []string{"url", "method", "headers", "body", "params", "calendar_schedule", "one_time_at", "interval_seconds"} {
		delete(attributes, field)
	}
	attributes["circuit_id"] = schema.StringAttribute{Required: true, MarkdownDescription: "ID of an existing Circuit in this project. Catalyst validates the target when saving the schedule."}
	attributes["job_pool_id"] = schema.StringAttribute{Required: true, MarkdownDescription: "ID of an existing Circuit Job Pool in this project."}
	attributes["expression"] = schema.StringAttribute{Required: true, MarkdownDescription: "Five-field cron expression."}
	attributes["end_time"] = schema.Int64Attribute{Required: true, MarkdownDescription: "Positive Unix timestamp in seconds at which scheduling ends."}
	attributes["timezone"] = schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("UTC"), MarkdownDescription: "IANA time zone for the expression. Defaults to UTC, encoded as GMT on requests."}
	resp.Schema.MarkdownDescription = "Experimental. Manage a pre-defined CronExpression schedule for an existing Circuit and Circuit Job Pool in Development. Supports schedules with empty job inputs. Requires an explicit organization and an eligible data center. Zoho lists EU, AU, IN, and CA as unavailable for Circuit scheduling. Defaults to disabled; enabling may execute the Circuit and incur usage. Requires `ZohoCatalyst.cron.CREATE`, `.READ`, `.UPDATE`, and `.DELETE`, `ZohoCatalyst.jobpool.READ`, and `ZOHOCATALYST.projects.Read`. Import as `project_id/cron_id`. Lifecycle tested offline."
}

func (r *CircuitCronResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data circuitCronModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateCronFields(webhookCronModel{
		ProjectID: data.ProjectID, JobPoolID: data.JobPoolID, Name: data.Name, JobName: data.JobName,
		Expression: data.Expression, Timezone: data.Timezone, EndTime: data.EndTime,
		RetryCount: data.RetryCount, RetryInterval: data.RetryInterval,
	}, resp)
	if !data.CircuitID.IsNull() && !data.CircuitID.IsUnknown() && !catalyst.ValidID(data.CircuitID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("circuit_id"), "Invalid Circuit ID", "Specify a positive integer written as a string.")
	}
}

func (r *CircuitCronResource) ready(diags *diag.Diagnostics) bool {
	if !r.WebhookCronResource.ready(diags) {
		return false
	}
	dc, env, _, err := r.client.ResourceRoutingScope()
	if err != nil {
		diags.AddError("Missing Circuit cron routing scope", err.Error())
		return false
	}
	if env != "Development" || dc == "eu" || dc == "au" || dc == "in" || dc == "ca" {
		diags.AddError("Circuit cron unavailable in this scope", "Use a Development project in a data center that supports Catalyst Circuit scheduling.")
		return false
	}
	return true
}

func (d circuitCronModel) api() catalyst.Cron {
	enabled := d.Enabled.ValueBool()
	return catalyst.Cron{
		Name: d.Name.ValueString(), Description: d.Description.ValueString(), Type: "CronExpression",
		ExecutionType: "pre-defined", Expression: d.Expression.ValueString(), Enabled: &enabled,
		EndTime: catalyst.Integer(d.EndTime.ValueInt64()), Schedule: catalyst.CronSchedule{Timezone: d.Timezone.ValueString()},
		Job: catalyst.CronJob{
			Name: d.JobName.ValueString(), PoolID: catalyst.ID(d.JobPoolID.ValueString()), TargetType: "Circuit",
			SourceType: "Cron", TargetID: catalyst.ID(d.CircuitID.ValueString()), Params: map[string]string{},
			Retry: catalyst.CronRetry{Count: catalyst.Integer(d.RetryCount.ValueInt64()), Interval: catalyst.Integer(d.RetryInterval.ValueInt64())},
		},
	}
}

func (d *circuitCronModel) populate(ctx context.Context, value *catalyst.Cron) diag.Diagnostics {
	schedule, diags := readCronSchedule(ctx, value, d.Timezone)
	d.ID, d.CircuitID = types.StringValue(string(value.ID)), types.StringValue(string(value.Job.TargetID))
	d.Name, d.Description = types.StringValue(value.Name), types.StringValue(value.Description)
	d.Expression, d.Timezone, d.EndTime = schedule.Expression, schedule.Timezone, schedule.EndTime
	d.Enabled = types.BoolValue(*value.Enabled)
	d.JobPoolID, d.JobName = types.StringValue(string(value.Job.PoolID)), types.StringValue(value.Job.Name)
	d.RetryCount, d.RetryInterval = types.Int64Value(int64(value.Job.Retry.Count)), types.Int64Value(0)
	if value.Job.Retry.Count != 0 {
		d.RetryInterval = types.Int64Value(int64(value.Job.Retry.Interval))
	}
	return diags
}

func (r *CircuitCronResource) persist(ctx context.Context, data *circuitCronModel, creating bool, diags *diag.Diagnostics, state *tfsdk.State) {
	project, id := data.ProjectID.ValueString(), data.ID.ValueString()
	if !creating {
		if _, err := r.client.GetCircuitCron(ctx, project, id); err != nil {
			diags.AddError("Unable to verify current Circuit cron", err.Error())
			return
		}
	}
	desired := data.api()
	pool, err := r.client.GetJobPool(ctx, project, string(desired.Job.PoolID))
	if err != nil {
		diags.AddError("Unable to verify Circuit Job Pool", err.Error())
		return
	}
	if pool.Type != "Circuit" {
		diags.AddError("Invalid Circuit Job Pool", "Select an existing Circuit Job Pool in the same project.")
		return
	}
	var result *catalyst.Cron
	if creating {
		result, err = r.client.CreateCron(ctx, project, desired)
	} else {
		result, err = r.client.UpdateCron(ctx, project, id, desired)
	}
	if result != nil && ((result.Job.TargetType != "" && result.Job.TargetType != "Circuit") ||
		(result.Job.TargetID != "" && result.Job.TargetID != desired.Job.TargetID) ||
		(result.Job.PoolID != "" && result.Job.PoolID != desired.Job.PoolID)) {
		diags.AddError("Unexpected Circuit scheduling target", "Catalyst returned a different target or pool for the write. Review the schedule in Catalyst before retrying.")
		return
	}
	if err != nil {
		if creating && result != nil {
			data.ID = types.StringValue(string(result.ID))
			diags.Append(state.Set(ctx, data)...)
		}
		diags.AddError("Unable to persist Circuit cron", err.Error())
		return
	}
	diags.Append(data.populate(ctx, result)...)
	diags.Append(state.Set(ctx, data)...)
}

func (r *CircuitCronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data circuitCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if !resp.Diagnostics.HasError() && r.ready(&resp.Diagnostics) {
		r.persist(ctx, &data, true, &resp.Diagnostics, &resp.State)
	}
}

func (r *CircuitCronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data circuitCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if !resp.Diagnostics.HasError() && r.ready(&resp.Diagnostics) {
		r.persist(ctx, &data, false, &resp.Diagnostics, &resp.State)
	}
}

func (r *CircuitCronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data circuitCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	cron, err := r.client.GetCircuitCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Circuit cron", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, cron)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CircuitCronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data circuitCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	project, id := data.ProjectID.ValueString(), data.ID.ValueString()
	_, err := r.client.GetCircuitCron(ctx, project, id)
	if err == nil {
		err = r.client.DeleteCron(ctx, project, id)
	}
	if errors.Is(err, catalyst.ErrCronNotFound) {
		return
	}
	if err == nil {
		err = r.client.WaitCircuitCronDeleted(ctx, project, id)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm Circuit cron deletion", err.Error())
	}
}
