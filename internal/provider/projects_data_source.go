// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSourceWithConfigure = &ProjectsDataSource{}

type ProjectsDataSource struct{ client *catalyst.Client }
type projectsModel struct {
	Projects []projectModel `tfsdk:"projects"`
}

func NewProjectsDataSource() datasource.DataSource { return &ProjectsDataSource{} }
func (d *ProjectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}
func (d *ProjectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List projects visible to the authenticated user in the provider's organization and data center. Requires the `ZOHOCATALYST.projects.Read` OAuth scope. Results are sorted lexicographically by ID; an account with no visible projects returns an empty list.",
		Attributes: map[string]schema.Attribute{
			"projects": schema.ListNestedAttribute{
				Computed: true, MarkdownDescription: "Visible projects, sorted by ID. IDs are strings to preserve precision.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Project ID."},
					"name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Project name."},
					"project_type": schema.StringAttribute{Computed: true, MarkdownDescription: "Project type; null if absent."},
					"domain":       schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain URL; null if absent."},
					"domain_name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain name; null if absent."},
					"timezone":     schema.StringAttribute{Computed: true, MarkdownDescription: "Project timezone; null if absent."},
				}},
			},
		},
	}
}
func (d *ProjectsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *ProjectsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before listing projects.")
		return
	}
	projects, err := d.client.ListProjects(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst projects", err.Error())
		return
	}
	data := projectsModel{Projects: make([]projectModel, 0, len(projects))}
	for _, project := range projects {
		data.Projects = append(data.Projects, projectState(&project))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func projectState(project *catalyst.Project) projectModel {
	return projectModel{
		ID: types.StringValue(string(project.ID)), Name: types.StringValue(project.Name),
		ProjectType: optionalString(project.Type), Domain: optionalString(project.DomainDetails.URL),
		DomainName: optionalString(project.DomainDetails.Name), Timezone: optionalString(project.Timezone),
	}
}
