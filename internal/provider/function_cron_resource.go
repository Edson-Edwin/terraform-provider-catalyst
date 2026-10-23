// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"regexp"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Shared configuration, import and schedule validation stay aligned with webhook crons.
// Target-specific API reads protect against importing or modifying the wrong kind.
type FunctionCronResource struct{ WebhookCronResource }

var functionCronJobName = regexp.MustCompile(`^[A-Za-z0-9_]{1,20}$`)

var _ resource.ResourceWithConfigure = &FunctionCronResource{}
var _ resource.ResourceWithImportState = &FunctionCronResource{}
var _ resource.ResourceWithValidateConfig = &FunctionCronResource{}

type functionCronModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Expression    types.String `tfsdk:"expression"`
	Timezone      types.String `tfsdk:"timezone"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	EndTime       types.Int64  `tfsdk:"end_time"`
	JobPoolID     types.String `tfsdk:"job_pool_id"`
	JobName       types.String `tfsdk:"job_name"`
	FunctionID    types.String `tfsdk:"function_id"`
	Params        types.Map    `tfsdk:"params"`
	RetryCount    types.Int64  `tfsdk:"retry_count"`
	RetryInterval types.Int64  `tfsdk:"retry_interval"`
}

func NewFunctionCronResource() resource.Resource { return &FunctionCronResource{} }
func (r *FunctionCronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_function_cron"
}
func (r *FunctionCronResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.WebhookCronResource.Schema(ctx, req, resp)
	for _, field := range []string{"url", "method", "headers", "body"} {
		delete(resp.Schema.Attributes, field)
	}
	resp.Schema.Attributes["function_id"] = schema.StringAttribute{Required: true, MarkdownDescription: "Existing Job Function ID in the same project. This is a Job Function (type job), not a legacy Cron Function."}
	resp.Schema.Attributes["job_pool_id"] = schema.StringAttribute{Required: true, MarkdownDescription: "Existing Function Job Pool ID in the same project."}
	resp.Schema.Attributes["job_name"] = schema.StringAttribute{Required: true, MarkdownDescription: "Job name: 1–20 ASCII letters, digits or underscores. Hyphens were rejected by the live Function scheduling API."}
	resp.Schema.Attributes["params"] = schema.MapAttribute{Optional: true, Computed: true, Sensitive: true, ElementType: types.StringType, MarkdownDescription: "Complete string parameter map passed to the Job Function. Defaults to an empty map.", Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{}))}
	resp.Schema.MarkdownDescription = "Manage a pre-defined CronExpression schedule targeting an existing Job Function and Function Job Pool. Validates target identities and project membership before writes. Defaults to disabled. Enabling submits real jobs and may incur Catalyst usage. The Function code and pool are separate resources/prerequisites. Supports updates, import as project_id/cron_id, drift and confirmed deletion. Requires cron create/read/update/delete, jobpool read, Function read and project read scopes. Parameter values are sensitive but remain in Terraform state; secure your state storage. Calendar/one-time schedules and legacy Cron Function triggers are not managed here. Live validation is limited to India Development."
}
func (d functionCronModel) shared() webhookCronModel {
	return webhookCronModel{ID: d.ID, ProjectID: d.ProjectID, Name: d.Name, Description: d.Description, Expression: d.Expression, Timezone: d.Timezone, Enabled: d.Enabled, EndTime: d.EndTime, JobPoolID: d.JobPoolID, JobName: d.JobName, Params: d.Params, RetryCount: d.RetryCount, RetryInterval: d.RetryInterval, Headers: types.MapValueMust(types.StringType, map[string]attr.Value{})}
}
func (r *FunctionCronResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data functionCronModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateCronFields(data.shared(), resp)
	if !data.JobName.IsNull() && !data.JobName.IsUnknown() && !functionCronJobName.MatchString(data.JobName.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("job_name"), "Invalid Function job name", "Use 1–20 ASCII letters, digits or underscores.")
	}
	if !data.FunctionID.IsNull() && !data.FunctionID.IsUnknown() && !catalyst.ValidID(data.FunctionID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("function_id"), "Invalid Job Function ID", "Specify a positive integer written as a string.")
	}
}
func (d functionCronModel) api(ctx context.Context) (catalyst.Cron, diag.Diagnostics) {
	cron, diags := d.shared().api(ctx)
	cron.Job.TargetType = "Function"
	cron.Job.TargetID = catalyst.ID(d.FunctionID.ValueString())
	return cron, diags
}
func (d *functionCronModel) populate(ctx context.Context, value *catalyst.Cron) diag.Diagnostics {
	shared := d.shared()
	diags := shared.populate(ctx, value)
	d.ID, d.Name, d.Description, d.Expression, d.Timezone = shared.ID, shared.Name, shared.Description, shared.Expression, shared.Timezone
	d.Enabled, d.EndTime, d.JobPoolID, d.JobName = shared.Enabled, shared.EndTime, shared.JobPoolID, shared.JobName
	d.Params, d.RetryCount, d.RetryInterval = shared.Params, shared.RetryCount, shared.RetryInterval
	d.FunctionID = types.StringValue(string(value.Job.TargetID))
	return diags
}
func (r *FunctionCronResource) verifyTarget(ctx context.Context, data functionCronModel) error {
	pools, err := r.client.ListJobPools(ctx, data.ProjectID.ValueString())
	if err != nil {
		return err
	}
	found := false
	for _, pool := range pools {
		if string(pool.ID) == data.JobPoolID.ValueString() && pool.Type == "Function" {
			found = true
		}
	}
	if !found {
		return errors.New("the selected pool is not an accessible Function Job Pool in this project")
	}
	function, err := r.client.GetFunction(ctx, data.ProjectID.ValueString(), data.FunctionID.ValueString())
	if err != nil {
		return err
	}
	if function.Type != "job" {
		return errors.New("the selected target must be a Job Function (type job)")
	}
	return nil
}
func (r *FunctionCronResource) persist(ctx context.Context, data *functionCronModel, creating bool, diags *diag.Diagnostics, setState func()) {
	if !creating {
		if _, err := r.client.GetFunctionCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString()); err != nil {
			diags.AddError("Unable to verify current Function cron", err.Error())
			return
		}
	}
	if err := r.verifyTarget(ctx, *data); err != nil {
		diags.AddError("Invalid Function scheduling target", err.Error())
		return
	}
	cron, more := data.api(ctx)
	diags.Append(more...)
	if diags.HasError() {
		return
	}
	var result *catalyst.Cron
	var err error
	if creating {
		result, err = r.client.CreateCron(ctx, data.ProjectID.ValueString(), cron)
	} else {
		result, err = r.client.UpdateCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString(), cron)
	}
	if creating && result != nil && catalyst.ValidID(string(result.ID)) {
		data.ID = types.StringValue(string(result.ID))
		setState()
	}
	if err != nil {
		diags.AddError("Unable to persist Function cron", err.Error())
		return
	}
	diags.Append(data.populate(ctx, result)...)
	setState()
}
func (r *FunctionCronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data functionCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *FunctionCronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data functionCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *FunctionCronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data functionCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	cron, err := r.client.GetFunctionCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Function cron", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, cron)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *FunctionCronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data functionCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	_, err := r.client.GetFunctionCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		return
	}
	if err == nil {
		err = r.client.DeleteCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	}
	if errors.Is(err, catalyst.ErrCronNotFound) {
		return
	}
	if err == nil {
		err = r.client.WaitFunctionCronDeleted(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm Function cron deletion", err.Error())
	}
}
