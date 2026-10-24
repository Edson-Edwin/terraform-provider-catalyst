// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &GatewayResource{}
var _ resource.ResourceWithImportState = &GatewayResource{}
var _ resource.ResourceWithValidateConfig = &GatewayResource{}

type GatewayResource struct{ client *catalyst.Client }
type gatewayModel struct {
	RoutingScope types.Object `tfsdk:"routing_scope"`
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	Enabled      types.Bool   `tfsdk:"enabled"`
}

func NewGatewayResource() resource.Resource { return withEnvironmentScope(&GatewayResource{}) }
func (r *GatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_gateway"
}
func (r *GatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage the project's API Gateway enabled setting. Enabling Gateway changes request routing: existing Function/Web Client endpoints need matching Gateway rules. Requires ZohoCatalyst.apigateway.READ/UPDATE and project read access. This resource owns only the enabled setting; use catalyst_api_gateway_ruleset for routes. Destroy disables Gateway and restores Security Rules routing. Set the ruleset project_id to this resource's id so Terraform destroys rules before disabling Gateway. Never share ownership across independent states or console/deployment tools. Import using the project ID. Operations wait up to five minutes for pending migrations. Live validation covers India Development only.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Project ID identifying this implicit project-level configuration.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing project ID; changing it replaces this configuration resource.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"enabled":    schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), MarkdownDescription: "Enable Gateway routing; defaults to true. Remove the dependent ruleset resource from configuration before disabling: its read API is unavailable while Gateway is disabled."},
		}}
}
func (r *GatewayResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *GatewayResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data gatewayModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if !data.ProjectID.IsNull() && !data.ProjectID.IsUnknown() && !catalyst.ValidID(data.ProjectID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("project_id"), "Invalid project ID", "Use a positive numeric string.")
	}
}
func (r *GatewayResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing Gateway.")
		return false
	}
	return true
}
func (r *GatewayResource) persist(ctx context.Context, data *gatewayModel, diags *diag.Diagnostics, setState func()) {
	if _, err := r.client.GetProject(ctx, data.ProjectID.ValueString()); err != nil {
		diags.AddError("Unable to access Gateway project", err.Error())
		return
	}
	if _, err := r.client.GetGateway(ctx, data.ProjectID.ValueString()); err != nil {
		diags.AddError("Unable to read Gateway", err.Error())
		return
	}
	data.ID = types.StringValue(data.ProjectID.ValueString())
	setState()
	if diags.HasError() {
		return
	}
	if err := r.client.SetGatewayEnabled(ctx, data.ProjectID.ValueString(), data.Enabled.ValueBool()); err != nil {
		diags.AddError("Unable to configure Gateway", err.Error())
		return
	}
	setState()
}
func (r *GatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data gatewayModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *GatewayResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data gatewayModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *GatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data gatewayModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	current, err := r.client.GetGateway(ctx, data.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Gateway", err.Error())
		return
	}
	data.ID = types.StringValue(data.ProjectID.ValueString())
	data.Enabled = types.BoolValue(*current.Enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *GatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data gatewayModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if err := r.client.SetGatewayEnabled(ctx, data.ProjectID.ValueString(), false); err != nil {
		resp.Diagnostics.AddError("Unable to disable Gateway", err.Error())
	}
}
func (r *GatewayResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !catalyst.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use the positive numeric project ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), req.ID)...)
}
