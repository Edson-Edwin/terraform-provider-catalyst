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

var _ datasource.DataSourceWithConfigure = &JobPoolsDataSource{}
var _ datasource.DataSourceWithValidateConfig = &JobPoolsDataSource{}

type JobPoolsDataSource struct{ client *catalyst.Client }
type jobPoolModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Type types.String `tfsdk:"type"`
}
type jobPoolsModel struct {
	ProjectID types.String   `tfsdk:"project_id"`
	Pools     []jobPoolModel `tfsdk:"pools"`
}

func NewJobPoolsDataSource() datasource.DataSource { return &JobPoolsDataSource{} }
func (d *JobPoolsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_job_pools"
}
func (d *JobPoolsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List existing Job Scheduling pools in a Catalyst project. Requires the `ZohoCatalyst.jobpool.READ` OAuth scope and project access. This data source does not create pools or execute jobs. Results are sorted lexicographically by ID; projects without pools return an empty list.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID, written as a string."},
			"pools": schema.ListNestedAttribute{
				Computed: true, MarkdownDescription: "Existing job pools, sorted by ID.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Job pool ID as a string, preserving precision."},
					"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Job pool name."},
					"type": schema.StringAttribute{Computed: true, MarkdownDescription: "Job pool target type reported by Catalyst, for example Function or Webhook."},
				}},
			},
		},
	}
}
func (d *JobPoolsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *JobPoolsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data jobPoolsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.ProjectID.IsNull() || data.ProjectID.IsUnknown() {
		return
	}
	if !catalyst.ValidID(data.ProjectID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("project_id"), "Invalid project ID", "Specify a positive integer written as a string.")
	}
}
func (d *JobPoolsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data jobPoolsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !catalyst.ValidID(data.ProjectID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("project_id"), "Invalid project ID", "Specify a known positive integer written as a string.")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before listing job pools.")
		return
	}
	pools, err := d.client.ListJobPools(ctx, data.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst job pools", err.Error())
		return
	}
	data.Pools = make([]jobPoolModel, 0, len(pools))
	for _, pool := range pools {
		data.Pools = append(data.Pools, jobPoolModel{ID: types.StringValue(string(pool.ID)), Name: types.StringValue(pool.Name), Type: types.StringValue(pool.Type)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
