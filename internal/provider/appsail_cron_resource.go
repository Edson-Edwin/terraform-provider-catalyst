// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Shared configuration, import and schedule validation stay aligned with webhook crons.
// Target-specific API reads protect against importing or modifying the wrong kind.
type AppSailCronResource struct{ WebhookCronResource }

var appSailCronJobName = regexp.MustCompile(`^[A-Za-z0-9_]{1,20}$`)

var _ resource.ResourceWithConfigure = &AppSailCronResource{}
var _ resource.ResourceWithImportState = &AppSailCronResource{}
var _ resource.ResourceWithValidateConfig = &AppSailCronResource{}

type appSailCronModel struct {
	RoutingScope  types.Object `tfsdk:"routing_scope"`
	Path          types.String `tfsdk:"path"`
	Method        types.String `tfsdk:"method"`
	Headers       types.Map    `tfsdk:"headers"`
	Body          types.String `tfsdk:"body"`
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
	AppSailID     types.String `tfsdk:"appsail_id"`
	Params        types.Map    `tfsdk:"params"`
	RetryCount    types.Int64  `tfsdk:"retry_count"`
	RetryInterval types.Int64  `tfsdk:"retry_interval"`
}

func NewAppSailCronResource() resource.Resource { return withEnvironmentScope(&AppSailCronResource{}) }
func (r *AppSailCronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_appsail_cron"
}
func (r *AppSailCronResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.WebhookCronResource.Schema(ctx, req, resp)
	for _, field := range []string{"url"} {
		delete(resp.Schema.Attributes, field)
	}
	resp.Schema.Attributes["path"] = schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("/"), MarkdownDescription: "Absolute path on the selected AppSail origin, starting with /. Use params for query parameters. Defaults to /."}
	resp.Schema.Attributes["appsail_id"] = schema.StringAttribute{Required: true, MarkdownDescription: "Existing deployed AppSail ID in the same project."}
	resp.Schema.Attributes["job_pool_id"] = schema.StringAttribute{Required: true, MarkdownDescription: "Existing AppSail Job Pool ID in the same project."}
	resp.Schema.Attributes["job_name"] = schema.StringAttribute{Required: true, MarkdownDescription: "Job name: 1–20 ASCII letters, digits or underscores. Use letters, digits or underscores."}
	resp.Schema.Attributes["params"] = schema.MapAttribute{Optional: true, Computed: true, Sensitive: true, ElementType: types.StringType, MarkdownDescription: "Complete string parameter map passed to the AppSail request. Defaults to an empty map.", Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{}))}
	resp.Schema.MarkdownDescription = "Manage a pre-defined AppSail Job Scheduling cron using an expression, daily/monthly/yearly calendar, one-time timestamp or relative interval. Requires an existing AppSail Job Pool and deployed AppSail in the same project. The provider resolves path against the authenticated AppSail origin and rejects another origin on refresh/import. Defaults to disabled; enabling schedules real requests and may incur usage. Supports in-place updates, import as project_id/cron_id, drift repair and confirmed deletion. Exactly one schedule mode is required. Recurring schedules require end_time; one-time schedules omit it. Headers, parameters and body are sensitive but remain in Terraform state. Requires cron create/read/update/delete, jobpool read, AppSail read and project read access. AppSail code and pool are managed separately. Live validation is limited to India Development."

}
func (d appSailCronModel) shared() webhookCronModel {
	return webhookCronModel{ID: d.ID, ProjectID: d.ProjectID, Name: d.Name, Description: d.Description, Expression: d.Expression, Calendar: d.Calendar, OneTimeAt: d.OneTimeAt, Interval: d.Interval, Timezone: d.Timezone, Enabled: d.Enabled, EndTime: d.EndTime, JobPoolID: d.JobPoolID, JobName: d.JobName, Params: d.Params, RetryCount: d.RetryCount, RetryInterval: d.RetryInterval, URL: d.Path, Method: d.Method, Headers: d.Headers, Body: d.Body}
}
func (r *AppSailCronResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data appSailCronModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	shared := data.shared()
	shared.URL = types.StringNull()
	validateCronFields(shared, resp)
	validateCronSchedule(ctx, data.shared().schedule(), &resp.Diagnostics)
	if !data.Path.IsNull() && !data.Path.IsUnknown() {
		if err := validAppSailCronPath(data.Path.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("path"), "Invalid AppSail path", err.Error())
		}
	}
	if !data.Method.IsNull() && !data.Method.IsUnknown() {
		switch data.Method.ValueString() {
		case "get", "post", "put", "delete", "head", "patch":
		default:
			resp.Diagnostics.AddAttributeError(path.Root("method"), "Invalid HTTP method", "Use get, post, put, delete, head or patch.")
		}
	}
	if !data.JobName.IsNull() && !data.JobName.IsUnknown() && !appSailCronJobName.MatchString(data.JobName.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("job_name"), "Invalid AppSail job name", "Use 1–20 ASCII letters, digits or underscores.")
	}
	if !data.AppSailID.IsNull() && !data.AppSailID.IsUnknown() && !catalyst.ValidID(data.AppSailID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("appsail_id"), "Invalid AppSail ID", "Specify a positive integer written as a string.")
	}
}
func (d appSailCronModel) api(ctx context.Context) (catalyst.Cron, diag.Diagnostics) {
	cron, diags := d.shared().api(ctx)
	cron.Job.TargetType = "AppSail"
	cron.Job.TargetID = catalyst.ID(d.AppSailID.ValueString())
	return cron, diags
}
func (d *appSailCronModel) populate(ctx context.Context, value *catalyst.Cron, origin string) diag.Diagnostics {
	route, err := appSailCronRoute(origin, value.Job.URL)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Invalid AppSail scheduling URL", err.Error())
		return diags
	}
	shared := d.shared()
	diags := shared.populate(ctx, value)
	d.ID, d.Name, d.Description, d.Expression, d.Timezone = shared.ID, shared.Name, shared.Description, shared.Expression, shared.Timezone
	d.Enabled, d.EndTime, d.JobPoolID, d.JobName = shared.Enabled, shared.EndTime, shared.JobPoolID, shared.JobName
	d.Params, d.RetryCount, d.RetryInterval = shared.Params, shared.RetryCount, shared.RetryInterval
	d.Path, d.Method, d.Headers, d.Body = types.StringValue(route), shared.Method, shared.Headers, shared.Body
	d.AppSailID = types.StringValue(string(value.Job.TargetID))
	d.Calendar, d.OneTimeAt, d.Interval = shared.Calendar, shared.OneTimeAt, shared.Interval
	return diags
}
func (r *AppSailCronResource) verifyTarget(ctx context.Context, data appSailCronModel) (*catalyst.AppSailMetadata, error) {
	pools, err := r.client.ListJobPools(ctx, data.ProjectID.ValueString())
	if err != nil {
		return nil, err
	}
	found := false
	for _, pool := range pools {
		if string(pool.ID) == data.JobPoolID.ValueString() && pool.Type == "AppSail" {
			found = true
		}
	}
	if !found {
		return nil, errors.New("the selected pool is not an accessible AppSail Job Pool in this project")
	}
	app, err := r.client.GetAppSail(ctx, data.ProjectID.ValueString(), data.AppSailID.ValueString())
	if err != nil {
		return nil, err
	}
	if _, err = appSailCronOrigin(app.URL); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *AppSailCronResource) persist(ctx context.Context, data *appSailCronModel, creating bool, diags *diag.Diagnostics, setState func()) {
	if !creating {
		if _, err := r.client.GetAppSailCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString()); err != nil {
			diags.AddError("Unable to verify current AppSail cron", err.Error())
			return
		}
	}
	app, err := r.verifyTarget(ctx, *data)
	if err != nil {
		diags.AddError("Invalid AppSail scheduling target", err.Error())
		return
	}
	cron, more := data.api(ctx)
	diags.Append(more...)
	if diags.HasError() {
		return
	}
	var result *catalyst.Cron
	cron.Job.URL = strings.TrimRight(app.URL, "/") + data.Path.ValueString()
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
		diags.AddError("Unable to persist AppSail cron", err.Error())
		return
	}
	diags.Append(data.populate(ctx, result, app.URL)...)
	setState()
}
func (r *AppSailCronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data appSailCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *AppSailCronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data appSailCronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *AppSailCronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data appSailCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	cron, err := r.client.GetAppSailCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrCronNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read AppSail cron", err.Error())
		return
	}
	app, err := r.client.GetAppSail(ctx, data.ProjectID.ValueString(), string(cron.Job.TargetID))
	if err != nil {
		resp.Diagnostics.AddError("Unable to verify AppSail scheduling origin", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, cron, app.URL)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *AppSailCronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data appSailCronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	_, err := r.client.GetAppSailCron(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
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
		err = r.client.WaitAppSailCronDeleted(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm AppSail cron deletion", err.Error())
	}
}

func validAppSailCronPath(value string) error {
	u, err := url.ParseRequestURI(value)
	if err != nil || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(value, "?#\\\r\n") {
		return errors.New("use an absolute path beginning with /, without an origin, query, fragment or backslash")
	}
	return nil
}
func appSailCronOrigin(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("the AppSail metadata must contain a canonical HTTPS origin")
	}
	return u, nil
}
func appSailCronRoute(origin, target string) (string, error) {
	base, err := appSailCronOrigin(origin)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("the scheduled URL does not match the selected AppSail origin")
	}
	route := u.EscapedPath()
	if route == "" {
		route = "/"
	}
	if err = validAppSailCronPath(route); err != nil {
		return "", err
	}
	return route, nil
}
