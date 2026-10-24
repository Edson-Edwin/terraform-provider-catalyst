// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &AuthUserRoleResource{}
var _ resource.ResourceWithValidateConfig = &AuthUserRoleResource{}
var _ resource.ResourceWithImportState = &AuthUserRoleResource{}

type AuthUserRoleResource struct{ client *catalyst.Client }
type authUserRoleModel struct {
	RoutingScope  types.Object `tfsdk:"routing_scope"`
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	UserID        types.String `tfsdk:"user_id"`
	RoleID        types.String `tfsdk:"role_id"`
	DestroyRoleID types.String `tfsdk:"destroy_role_id"`
	RoleName      types.String `tfsdk:"role_name"`
}

func NewAuthUserRoleResource() resource.Resource {
	return withEnvironmentScope(&AuthUserRoleResource{})
}
func (r *AuthUserRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_user_role"
}
func (r *AuthUserRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Assign an existing application end-user to an existing role through the official SDK's user-update API. Requires ZohoCatalyst.projects.users.READ and .UPDATE. Experimental: no disposable application user was available for live write verification; offline lifecycle tests do not establish live compatibility. Does not invite/delete users, provision roles, configure authentication, or edit table permissions. Destroy assigns the explicit destroy_role_id fallback; it never deletes the user. Use one resource/state per project/user across all tools because each user has one role and Catalyst provides no conditional role update. User profiles are not stored in state. Import as project_id/user_id/destroy_role_id; import only reads the current assignment.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Assignment identity: project_id/user_id.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":      schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID as a string. Changes require replacement.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"user_id":         schema.StringAttribute{Required: true, MarkdownDescription: "Existing end-user's positive numeric ID as a string. Administrator/collaborator accounts are refused. Changes require replacement.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"role_id":         schema.StringAttribute{Required: true, MarkdownDescription: "Existing role to assign. Changes update the user's role in place."},
		"destroy_role_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing fallback role assigned on destroy. Choose a least-privilege role such as App User. Apply changes to this value before destroying. Updating this field alone does not change user access."},
		"role_name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Current assigned role name reported by Catalyst."},
	}}
}
func (r *AuthUserRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *AuthUserRoleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data authUserRoleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateAuthIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "user_id": data.UserID, "role_id": data.RoleID, "destroy_role_id": data.DestroyRoleID})
}
func (r *AuthUserRoleResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing user roles.")
		return false
	}
	return true
}
func (r *AuthUserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data authUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	// Verify the existing end-user before recording ownership. Keep recovery state
	// for any subsequent ambiguous write/readback failure, so destroy can reset it.
	u, err := r.client.GetAuthUser(ctx, data.ProjectID.ValueString(), data.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst application user", err.Error())
		return
	}
	if u.UserType != "App User" {
		resp.Diagnostics.AddError("Unsupported user type", "Administrator and collaborator accounts cannot be managed by an end-user role assignment.")
		return
	}
	data.ID = types.StringValue(data.ProjectID.ValueString() + "/" + data.UserID.ValueString())
	data.RoleName = types.StringValue(u.RoleName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err = r.client.SetAuthUserRole(ctx, data.ProjectID.ValueString(), data.UserID.ValueString(), data.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to assign Catalyst user role", err.Error())
		return
	}
	data.RoleName = types.StringValue(u.RoleName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *AuthUserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data authUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	u, err := r.client.GetAuthUser(ctx, data.ProjectID.ValueString(), data.UserID.ValueString())
	if errors.Is(err, catalyst.ErrAuthUserNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst user role", err.Error())
		return
	}
	if u.UserType != "App User" {
		resp.Diagnostics.AddError("Unsupported user type", "The account is no longer an application end-user; inspect its access manually. Ownership is retained.")
		return
	}
	data.RoleID = types.StringValue(string(u.RoleID))
	data.RoleName = types.StringValue(u.RoleName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *AuthUserRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior authUserRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if !data.ProjectID.Equal(prior.ProjectID) || !data.UserID.Equal(prior.UserID) {
		resp.Diagnostics.AddError("Role assignment replacement required", "Project and user IDs cannot be changed in place.")
		return
	}
	u, err := r.client.SetAuthUserRole(ctx, data.ProjectID.ValueString(), data.UserID.ValueString(), data.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to assign Catalyst user role", err.Error())
		return
	}
	data.ID = prior.ID
	data.RoleName = types.StringValue(u.RoleName)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *AuthUserRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data authUserRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	_, err := r.client.SetAuthUserRole(ctx, data.ProjectID.ValueString(), data.UserID.ValueString(), data.DestroyRoleID.ValueString())
	if err != nil && !errors.Is(err, catalyst.ErrAuthUserNotFound) {
		resp.Diagnostics.AddError("Unable to reset Catalyst user role", err.Error())
	}
}
func (r *AuthUserRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || !catalyst.ValidID(parts[0]) || !catalyst.ValidID(parts[1]) || !catalyst.ValidID(parts[2]) {
		resp.Diagnostics.AddError("Invalid assignment import ID", "Use project_id/user_id/destroy_role_id, with positive numeric IDs. The final component selects the fallback role to assign when this resource is destroyed.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[0]+"/"+parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("destroy_role_id"), parts[2])...)
}
