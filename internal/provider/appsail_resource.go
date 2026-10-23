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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &AppSailResource{}
var _ resource.ResourceWithImportState = &AppSailResource{}
var _ resource.ResourceWithValidateConfig = &AppSailResource{}

type AppSailResource struct{ client *catalyst.Client }
type appsailModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Name        types.String `tfsdk:"name"`
	Stack       types.String `tfsdk:"stack"`
	Command     types.String `tfsdk:"startup_command"`
	Port        types.Int64  `tfsdk:"port"`
	Auth        types.Bool   `tfsdk:"catalyst_auth"`
	Source      types.String `tfsdk:"source"`
	SHA256      types.String `tfsdk:"source_sha256"`
	Memory      types.Int64  `tfsdk:"memory"`
	Environment types.Map    `tfsdk:"environment_variables"`
}

func NewAppSailResource() resource.Resource { return &AppSailResource{} }
func (r *AppSailResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_appsail"
}
func (r *AppSailResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "Deploy a Node.js 22 or 24 AppSail from a prebuilt ZIP using ZohoCatalyst.appsail read/create/update/delete scopes and project read access. Supports code, memory, startup command, port, authentication and complete environment-map updates, import and artifact drift detection. Names must be unique. Catalyst uses name-keyed upserts: keep project/name ownership exclusive across Terraform states and external tools; no atomic conditional-create primitive has been established. Compressed and expanded archives are limited to 100 MB. Sensitive environment values remain in Terraform state; source code does not. The provider verifies configuration and artifact persistence without invoking the app or proving runtime health. Authentication defaults to enabled. Import with project_id/appsail_id, then supply matching source/hash. Deletion is confirmed for up to two minutes. Live validation is limited to India Development.", Attributes: map[string]schema.Attribute{
		"id":                    schema.StringAttribute{Computed: true, MarkdownDescription: "AppSail ID as a precision-preserving string.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":            schema.StringAttribute{Required: true, MarkdownDescription: "Existing project ID.", PlanModifiers: replace},
		"name":                  schema.StringAttribute{Required: true, MarkdownDescription: "Unique alphanumeric AppSail name starting with a letter, up to 100 characters. Changing it replaces the service.", PlanModifiers: replace},
		"stack":                 schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("node22"), MarkdownDescription: "Supported runtimes: node22 or node24.", PlanModifiers: replace},
		"startup_command":       schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("node index.js"), MarkdownDescription: "Command used to start the application; defaults to node index.js."},
		"port":                  schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(9000), MarkdownDescription: "Application port: 80, 443, or 1025 through 65535. Defaults to 9000. Bind the app to the configured port or X_ZOHO_CATALYST_LISTEN_PORT."},
		"catalyst_auth":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), MarkdownDescription: "Require Catalyst authentication for application access. Defaults to true."},
		"source":                schema.StringAttribute{Required: true, MarkdownDescription: "Local prebuilt application ZIP with package.json at its root. Include application files and built dependencies before applying."},
		"source_sha256":         schema.StringAttribute{Required: true, MarkdownDescription: "Lowercase hex SHA-256, usually filesha256(source). Changed contents update code in place. Refresh records the remote archive hash."},
		"memory":                schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(128), MarkdownDescription: "Memory allocation in MB: 128, 256, 512, 1024 or 2048; defaults to 128."},
		"environment_variables": schema.MapAttribute{Optional: true, Computed: true, Sensitive: true, ElementType: types.StringType, Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})), MarkdownDescription: "Complete desired environment-variable map. Removing keys clears them remotely. Values remain in Terraform state."},
	}}
}
func (r *AppSailResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *AppSailResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing an AppSail.")
		return false
	}
	return true
}
func (r *AppSailResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data appsailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for key, value := range map[string]types.String{"project_id": data.ProjectID, "name": data.Name, "stack": data.Stack, "startup_command": data.Command, "source": data.Source, "source_sha256": data.SHA256} {
		if value.IsUnknown() || value.IsNull() {
			continue
		}
		v := value.ValueString()
		valid := true
		switch key {
		case "project_id":
			valid = catalyst.ValidID(v)
		case "name":
			valid = catalyst.ValidAppSailName(v)
		case "stack":
			valid = v == "node22" || v == "node24"
		case "startup_command":
			valid = strings.TrimSpace(v) != "" && !strings.ContainsRune(v, 0)
		case "source":
			valid = strings.TrimSpace(v) != ""
		case "source_sha256":
			valid = sha256Pattern.MatchString(v)
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(tfpath.Root(key), "Invalid AppSail configuration", "Provide a valid "+key+" matching the documented supported values.")
		}
	}
	if !data.Memory.IsUnknown() && !data.Memory.IsNull() {
		m := data.Memory.ValueInt64()
		if m != 128 && m != 256 && m != 512 && m != 1024 && m != 2048 {
			resp.Diagnostics.AddAttributeError(tfpath.Root("memory"), "Invalid memory allocation", "Use 128, 256, 512, 1024 or 2048 MB.")
		}
	}
	if !data.Port.IsNull() && !data.Port.IsUnknown() {
		p := data.Port.ValueInt64()
		if p != 80 && p != 443 && (p < 1025 || p > 65535) {
			resp.Diagnostics.AddAttributeError(tfpath.Root("port"), "Invalid application port", "Use 80, 443, or 1025 through 65535.")
		}
	}

}
func validateAppSailArchive(content []byte) error {
	files, err := deploymentArchiveFiles(content)
	if err != nil {
		return err
	}
	file := files["package.json"]
	if file == nil {
		return errors.New("application ZIP must contain package.json at its root")
	}
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	var pkg map[string]json.RawMessage
	raw, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &pkg) != nil || pkg == nil {
		return errors.New("application package.json must be a JSON object")
	}
	return nil
}
func (d *appsailModel) api(ctx context.Context) (catalyst.AppSailDeployment, diag.Diagnostics) {
	var diags diag.Diagnostics
	variables := map[string]string{}
	diags.Append(d.Environment.ElementsAs(ctx, &variables, false)...)
	value := catalyst.AppSailDeployment{AppSailMetadata: catalyst.AppSailMetadata{Name: d.Name.ValueString(), Stack: d.Stack.ValueString()}}
	auth := d.Auth.ValueBool()
	value.Configuration.Auth = &auth
	value.Configuration.Command = d.Command.ValueString()
	value.Configuration.Port = catalyst.Integer(d.Port.ValueInt64())
	value.Configuration.Memory = catalyst.Integer(d.Memory.ValueInt64())
	value.Configuration.Environment.Variables = variables
	return value, diags
}
func (d *appsailModel) populate(ctx context.Context, result *catalyst.AppSailDeployment, hash string) diag.Diagnostics {
	var diags diag.Diagnostics
	d.ID = types.StringValue(string(result.ID))
	d.Name = types.StringValue(result.Name)
	d.Stack = types.StringValue(result.Stack)
	d.Command = types.StringValue(result.Configuration.Command)
	d.Port = types.Int64Value(int64(result.Configuration.Port))
	d.Auth = types.BoolValue(*result.Configuration.Auth)
	d.Memory = types.Int64Value(int64(result.Configuration.Memory))
	d.SHA256 = types.StringValue(hash)
	env, dg := types.MapValueFrom(ctx, types.StringType, result.Configuration.Environment.Variables)
	diags.Append(dg...)
	d.Environment = env
	return diags
}
func (r *AppSailResource) persist(ctx context.Context, data *appsailModel, creating bool, diags *diag.Diagnostics, setState func()) {
	archive, err := readUploadSource(data.Source.ValueString(), data.SHA256.ValueString())
	if err == nil {
		err = validateAppSailArchive(archive)
	}
	if err != nil {
		diags.AddError("Invalid AppSail source", err.Error())
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
	result, err := r.client.DeployAppSail(ctx, data.ProjectID.ValueString(), id, desired, archive)
	if result != nil && catalyst.ValidID(string(result.ID)) {
		data.ID = types.StringValue(string(result.ID))
		setState()
	}
	if err != nil {
		diags.AddError("Unable to deploy Catalyst AppSail", err.Error())
		return
	}
	// Metadata and code propagate independently after a successful upsert.
	current, err := r.client.WaitAppSailDeployment(ctx, data.ProjectID.ValueString(), data.ID.ValueString(), desired, data.SHA256.ValueString())
	if err != nil {
		diags.AddError("Unable to verify AppSail deployment", "The AppSail ID is retained for recovery. Refresh before retrying. "+err.Error())
		return
	}
	diags.Append(data.populate(ctx, current, data.SHA256.ValueString())...)

	setState()
}
func (r *AppSailResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data appsailModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *AppSailResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data appsailModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *AppSailResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data appsailModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	result, err := r.client.GetAppSailDeployment(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrAppSailNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst AppSail", err.Error())
		return
	}
	hash, err := r.client.AppSailSHA256(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrAppSailNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read AppSail artifact", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, result, hash)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *AppSailResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data appsailModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := r.client.DeleteAppSail(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrAppSailNotFound) {
		return
	}
	if err == nil {
		err = r.client.WaitAppSailDeleted(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm AppSail deletion", err.Error())
	}
}
func (r *AppSailResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids := strings.Split(req.ID, "/")
	if len(ids) != 2 || !catalyst.ValidID(ids[0]) || !catalyst.ValidID(ids[1]) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use project_id/appsail_id with two positive numeric IDs.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tfpath.Root("project_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, tfpath.Root("id"), ids[1])...)
}
