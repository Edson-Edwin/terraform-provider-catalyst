// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	tfpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &FunctionResource{}
var _ resource.ResourceWithImportState = &FunctionResource{}
var _ resource.ResourceWithValidateConfig = &FunctionResource{}

type FunctionResource struct{ client *catalyst.Client }
type functionModel struct {
	RoutingScope types.Object `tfsdk:"routing_scope"`
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	Name         types.String `tfsdk:"name"`
	Stack        types.String `tfsdk:"stack"`
	Type         types.String `tfsdk:"type"`
	Source       types.String `tfsdk:"source"`
	SHA256       types.String `tfsdk:"source_sha256"`
	Memory       types.Int64  `tfsdk:"memory"`
	Environment  types.Map    `tfsdk:"environment_variables"`
}

func NewFunctionResource() resource.Resource { return withEnvironmentScope(&FunctionResource{}) }
func (r *FunctionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_function"
}
func (r *FunctionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "Deploy a Node.js 22 or 24 Function (Basic I/O, Advanced I/O, Event, Cron or Job) from a prebuilt ZIP using `ZohoCatalyst.functions.ALL` and project read access. Supports code/configuration updates, import and drift detection by downloading the archive. Names must be unique: creation checks existing Functions and requires explicit import instead of adoption. Catalyst uses name-keyed upserts; keep project/name ownership exclusive across Terraform states and external deployment tools because the API has no established atomic conditional-create operation. Updates own memory and environment variables. Secrets marked sensitive remain in state; source code is not stored in state. The provider limits compressed and expanded archives to 100 MB. Artifact/configuration read-back verifies persistence, not runtime execution or Production promotion. The provider never invokes the Function. Import with `project_id/function_id` and supply a matching source ZIP/hash afterward. Deletion is confirmed for up to two minutes. Live validation covers India Development only.", Attributes: map[string]schema.Attribute{
		"id":                    schema.StringAttribute{Computed: true, MarkdownDescription: "Function ID as a precision-preserving string.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":            schema.StringAttribute{Required: true, MarkdownDescription: "Existing project ID.", PlanModifiers: replace},
		"name":                  schema.StringAttribute{Required: true, MarkdownDescription: "Unique Function name; must match the ZIP configuration and package name. Changing it replaces the Function.", PlanModifiers: replace},
		"stack":                 schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("node22"), MarkdownDescription: "Supported runtimes: node22 and node24; defaults to node22.", PlanModifiers: replace},
		"type":                  schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("basicio"), MarkdownDescription: "Supported types: basicio, advancedio, event, cron and job; defaults to basicio. Event/cron triggers and Job Scheduling pools/schedules are separate configuration and are not created by this resource.", PlanModifiers: replace},
		"source":                schema.StringAttribute{Required: true, MarkdownDescription: "Local prebuilt ZIP containing catalyst-config.json, package.json and the entry point at its root. Build dependencies before applying."},
		"source_sha256":         schema.StringAttribute{Required: true, MarkdownDescription: "Lowercase hex SHA-256, usually filesha256(source). Changed contents update code in place. Refresh records the remote archive hash."},
		"memory":                schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(256), MarkdownDescription: "Memory allocation in MB: 128, 256 or 512; defaults to 256."},
		"environment_variables": schema.MapAttribute{Optional: true, Computed: true, Sensitive: true, ElementType: types.StringType, Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})), MarkdownDescription: "Complete desired environment-variable map. Removing keys clears them remotely. Values remain in Terraform state."},
	}}
}
func (r *FunctionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client.")
		return
	}
	r.client = client
}
func (r *FunctionResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing a Function.")
		return false
	}
	return true
}
func (r *FunctionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for key, value := range map[string]types.String{"project_id": data.ProjectID, "name": data.Name, "stack": data.Stack, "type": data.Type, "source": data.Source, "source_sha256": data.SHA256} {
		if value.IsUnknown() || value.IsNull() {
			continue
		}
		v := value.ValueString()
		valid := true
		switch key {
		case "project_id":
			valid = catalyst.ValidID(v)
		case "name":
			valid = catalyst.ValidFunctionName(v)
		case "stack":
			valid = catalyst.ValidFunctionStack(v)
		case "type":
			valid = catalyst.ValidFunctionType(v)
		case "source":
			valid = strings.TrimSpace(v) != ""
		case "source_sha256":
			valid = sha256Pattern.MatchString(v)
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(tfpath.Root(key), "Invalid Function configuration", "Provide a valid "+key+" matching the documented supported values.")
		}
	}
	if !data.Memory.IsUnknown() && !data.Memory.IsNull() {
		m := data.Memory.ValueInt64()
		if m != 128 && m != 256 && m != 512 {
			resp.Diagnostics.AddAttributeError(tfpath.Root("memory"), "Invalid memory allocation", "Use 128, 256 or 512 MB.")
		}
	}
}
func validateFunctionArchive(content []byte, name, stack, kind string) error {
	files, err := deploymentArchiveFiles(content)
	if err != nil {
		return err
	}
	read := func(name string, target any) error {
		file := files[name]
		if file == nil {
			return errors.New("archive is missing required root files")
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		decoder := json.NewDecoder(io.LimitReader(r, 1<<20))
		return decoder.Decode(target)
	}
	var config struct {
		Deployment struct{ Name, Stack, Type string }
		Execution  struct{ Main string }
	}
	var pkg struct{ Name, Main string }
	if read("catalyst-config.json", &config) != nil || read("package.json", &pkg) != nil {
		return errors.New("archive configuration files are missing or invalid")
	}
	if config.Deployment.Name != name || config.Deployment.Stack != stack || config.Deployment.Type != kind || pkg.Name != name || config.Execution.Main == "" || pkg.Main != config.Execution.Main || files[config.Execution.Main] == nil {
		return errors.New("ZIP name/runtime/type/entry point must match the Terraform configuration")
	}
	return nil
}
func (d *functionModel) api(ctx context.Context) (catalyst.FunctionDeployment, diag.Diagnostics) {
	var diags diag.Diagnostics
	variables := map[string]string{}
	diags.Append(d.Environment.ElementsAs(ctx, &variables, false)...)
	value := catalyst.FunctionDeployment{FunctionMetadata: catalyst.FunctionMetadata{Name: d.Name.ValueString(), Stack: d.Stack.ValueString(), Type: d.Type.ValueString()}}
	value.Configuration.Memory = catalyst.Integer(d.Memory.ValueInt64())
	value.Configuration.Environment.Variables = variables
	return value, diags
}
func (d *functionModel) populate(ctx context.Context, result *catalyst.FunctionDeployment, hash string) diag.Diagnostics {
	var diags diag.Diagnostics
	d.ID = types.StringValue(string(result.ID))
	d.Name = types.StringValue(result.Name)
	d.Stack = types.StringValue(result.Stack)
	d.Type = types.StringValue(result.Type)
	d.Memory = types.Int64Value(int64(result.Configuration.Memory))
	d.SHA256 = types.StringValue(hash)
	env, dg := types.MapValueFrom(ctx, types.StringType, result.Configuration.Environment.Variables)
	diags.Append(dg...)
	d.Environment = env
	return diags
}
func (r *FunctionResource) persist(ctx context.Context, data *functionModel, creating bool, diags *diag.Diagnostics, setState func()) {
	archive, err := readUploadSource(data.Source.ValueString(), data.SHA256.ValueString())
	if err == nil {
		err = validateFunctionArchive(archive, data.Name.ValueString(), data.Stack.ValueString(), data.Type.ValueString())
	}
	if err != nil {
		diags.AddError("Invalid Function source", err.Error())
		return
	}
	desired, dg := data.api(ctx)
	diags.Append(dg...)
	if diags.HasError() {
		return
	}
	id := data.ID.ValueString()
	if creating {
		id = ""
	}
	result, err := r.client.DeployFunction(ctx, data.ProjectID.ValueString(), id, desired, archive)
	if result != nil && catalyst.ValidID(string(result.ID)) {
		data.ID = types.StringValue(string(result.ID))
		setState()
	}
	if err != nil {
		diags.AddError("Unable to deploy Catalyst Function", err.Error())
		return
	}
	current, err := r.client.WaitFunctionDeployment(ctx, data.ProjectID.ValueString(), data.ID.ValueString(), desired, data.SHA256.ValueString())
	if err != nil {
		diags.AddError("Unable to verify Function deployment", "The Function ID is retained for recovery. Refresh before retrying. "+err.Error())
		return
	}
	diags.Append(data.populate(ctx, current, data.SHA256.ValueString())...)

	setState()
}
func (r *FunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *FunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *FunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	result, err := r.client.GetFunctionDeployment(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrFunctionNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst Function", err.Error())
		return
	}
	hash, err := r.client.FunctionSHA256(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrFunctionNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Function artifact", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, result, hash)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *FunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := r.client.DeleteFunction(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrFunctionNotFound) {
		return
	}
	if err == nil {
		err = r.client.WaitFunctionDeleted(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm Function deletion", err.Error())
	}
}
func (r *FunctionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids := strings.Split(req.ID, "/")
	if len(ids) != 2 || !catalyst.ValidID(ids[0]) || !catalyst.ValidID(ids[1]) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use project_id/function_id with two positive numeric IDs.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tfpath.Root("project_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tfpath.Root("id"), ids[1])...)
}
