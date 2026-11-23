// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type AuthUsersDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &AuthUsersDataSource{}
var _ datasource.DataSourceWithValidateConfig = &AuthUsersDataSource{}

type authUsersModel struct {
	ProjectID types.String      `tfsdk:"project_id"`
	Start     types.Int64       `tfsdk:"start"`
	Limit     types.Int64       `tfsdk:"limit"`
	Users     []authUsersRecord `tfsdk:"users"`
}

type authUsersRecord struct {
	ID       string       `tfsdk:"id"`
	RoleID   types.String `tfsdk:"role_id"`
	RoleName types.String `tfsdk:"role_name"`
	UserType types.String `tfsdk:"user_type"`
	Status   types.String `tfsdk:"status"`
}

func NewAuthUsersDataSource() datasource.DataSource { return &AuthUsersDataSource{} }

func (d *AuthUsersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_users"
}

func (d *AuthUsersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read one page of access metadata for existing Catalyst application users. Requires `ZohoCatalyst.projects.users.READ`. Email addresses, personal names, and profile details are omitted. Use the exact-ID data source for a specific known user. Verified offline.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"start":      schema.Int64Attribute{Optional: true, Computed: true, MarkdownDescription: "Zero-based offset. Defaults to 0."},
			"limit":      schema.Int64Attribute{Optional: true, Computed: true, MarkdownDescription: "Page size from 1 through 200. Defaults to 50."},
			"users": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "This page's users sorted lexicographically by ID. A successful empty page returns an empty list.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":        schema.StringAttribute{Computed: true, MarkdownDescription: "Application user ID as a precision-preserving string."},
					"role_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned role ID, when reported."},
					"role_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned role name, when reported."},
					"user_type": schema.StringAttribute{Computed: true, MarkdownDescription: "Application account type, when reported."},
					"status":    schema.StringAttribute{Computed: true, MarkdownDescription: "Application account status, when reported."},
				}}},
		},
	}
}

func (d *AuthUsersDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data authUsersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID})
	if !data.Start.IsNull() && !data.Start.IsUnknown() && data.Start.ValueInt64() < 0 {
		resp.Diagnostics.AddAttributeError(path.Root("start"), "Invalid start offset", "Specify a non-negative offset.")
	}
	if !data.Limit.IsNull() && !data.Limit.IsUnknown() && (data.Limit.ValueInt64() < 1 || data.Limit.ValueInt64() > catalyst.MaxAuthUsersPageSize) {
		resp.Diagnostics.AddAttributeError(path.Root("limit"), "Invalid page size", "Specify a page size from 1 through 200.")
	}
}

func (d *AuthUsersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data authUsersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading application users.")
		return
	}
	if data.Start.IsUnknown() || data.Limit.IsUnknown() {
		resp.Diagnostics.AddError("Unknown pagination", "The start and limit must be known before reading application users.")
		return
	}
	if data.Start.IsNull() {
		data.Start = types.Int64Value(0)
	}
	if data.Limit.IsNull() {
		data.Limit = types.Int64Value(50)
	}
	users, err := d.client.ListAuthUsersPage(ctx, data.ProjectID.ValueString(), data.Start.ValueInt64(), data.Limit.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst application users", err.Error())
		return
	}
	data.Users = make([]authUsersRecord, 0, len(users))
	for _, user := range users {
		record := authUsersRecord{ID: string(user.ID), UserType: types.StringPointerValue(user.Type), Status: types.StringPointerValue(user.Status)}
		if user.Role != nil {
			record.RoleID = optionalString(string(user.Role.ID))
			record.RoleName = types.StringPointerValue(user.Role.Name)
		}
		data.Users = append(data.Users, record)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
