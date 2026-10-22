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

var _ datasource.DataSource = &ProjectDataSource{}
var _ datasource.DataSourceWithConfigure = &ProjectDataSource{}
var _ datasource.DataSourceWithValidateConfig = &ProjectDataSource{}

type ProjectDataSource struct{ client *catalyst.Client }

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ProjectType types.String `tfsdk:"project_type"`
	Domain      types.String `tfsdk:"domain"`
	DomainName  types.String `tfsdk:"domain_name"`
	Timezone    types.String `tfsdk:"timezone"`
}

func NewProjectDataSource() datasource.DataSource { return &ProjectDataSource{} }

func (d *ProjectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read an existing Catalyst project by its ID. Requires the `ZOHOCATALYST.projects.Read` OAuth scope and access to the project. This data source does not create or modify projects.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID, written as a string to preserve precision."},
			"name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Project name."},
			"project_type": schema.StringAttribute{Computed: true, MarkdownDescription: "Project type reported by Catalyst; null if absent."},
			"domain":       schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain URL; null if absent."},
			"domain_name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain name; null if absent."},
			"timezone":     schema.StringAttribute{Computed: true, MarkdownDescription: "Project timezone; null if absent."},
		},
	}
}

func (d *ProjectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client. Please report this provider bug.")
		return
	}
	d.client = client
}

func (d *ProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data projectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.ID.IsUnknown() || data.ID.IsNull() || !catalyst.ValidID(data.ID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Invalid project ID", "Specify a known positive integer as a string, such as \"12345678901234567\".")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading a project.")
		return
	}
	project, err := d.client.GetProject(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst project", err.Error())
		return
	}
	data.ID = types.StringValue(string(project.ID))
	data.Name = types.StringValue(project.Name)
	data.ProjectType = optionalString(project.Type)
	data.Domain = optionalString(project.DomainDetails.URL)
	data.DomainName = optionalString(project.DomainDetails.Name)
	data.Timezone = optionalString(project.Timezone)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// ValidateConfig rejects malformed IDs before Terraform makes API calls.
func (d *ProjectDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data projectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.ID.IsUnknown() || data.ID.IsNull() {
		return
	}
	if !catalyst.ValidID(data.ID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Invalid project ID", "Specify a positive integer as a string, such as \"12345678901234567\".")
	}
}
