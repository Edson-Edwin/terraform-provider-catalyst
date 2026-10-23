// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSourceWithConfigure = &AuthUserDataSource{}
var _ datasource.DataSourceWithValidateConfig = &AuthUserDataSource{}

type AuthUserDataSource struct{ client *catalyst.Client }
type authUserModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	UserID    types.String `tfsdk:"user_id"`
	RoleID    types.String `tfsdk:"role_id"`
	RoleName  types.String `tfsdk:"role_name"`
	UserType  types.String `tfsdk:"user_type"`
	Status    types.String `tfsdk:"status"`
}

func NewAuthUserDataSource() datasource.DataSource { return &AuthUserDataSource{} }
func (d *AuthUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_user"
}
func (d *AuthUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Read an existing application's user access metadata. Requires ZohoCatalyst.projects.users.READ. Does not store the user's email, name, organization, locale or profile in Terraform state. This is user-role discovery, not authentication setup, role provisioning or permission management.", Attributes: map[string]schema.Attribute{
		"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
		"user_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Existing application user's positive numeric ID as a string."},
		"role_id":    schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned role ID as a precision-preserving string."},
		"role_name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned role's current name."},
		"user_type":  schema.StringAttribute{Computed: true, MarkdownDescription: "Account type reported by Catalyst."},
		"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "Account status reported by Catalyst; not managed."},
	}}
}
func (d *AuthUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client.")
		return
	}
	d.client = client
}
func validateAuthIDs(diags *diag.Diagnostics, values map[string]types.String) {
	for name, v := range values {
		if !v.IsUnknown() && !v.IsNull() && !catalyst.ValidID(v.ValueString()) {
			diags.AddAttributeError(path.Root(name), "Invalid Catalyst ID", "Specify a positive integer written as a string.")
		}
	}
}
func (d *AuthUserDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data authUserModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateAuthIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "user_id": data.UserID})
}
func (d *AuthUserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data authUserModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading user access.")
		return
	}
	u, err := d.client.GetAuthUser(ctx, data.ProjectID.ValueString(), data.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst application user", err.Error())
		return
	}
	data.RoleID = types.StringValue(string(u.RoleID))
	data.RoleName = types.StringValue(u.RoleName)
	data.UserType = types.StringValue(u.UserType)
	data.Status = types.StringValue(u.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
