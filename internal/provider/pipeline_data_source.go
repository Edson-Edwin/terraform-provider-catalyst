// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type PipelineDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &PipelineDataSource{}
var _ datasource.DataSourceWithValidateConfig = &PipelineDataSource{}

type pipelineModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Status    types.String `tfsdk:"status"`
}

func NewPipelineDataSource() datasource.DataSource { return &PipelineDataSource{} }

func (d *PipelineDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (d *PipelineDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read an existing Catalyst pipeline by ID. Requires `ZohoCatalyst.pipeline.READ` and project access. Returns the pipeline identity, name and status. Uses the documented read API without triggering the pipeline. Verified offline.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"id":         schema.StringAttribute{Required: true, MarkdownDescription: "Existing pipeline's positive numeric ID as a string."},
			"name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Pipeline name reported by Catalyst."},
			"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "Pipeline status, when reported."},
		},
	}
}

func (d *PipelineDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data pipelineModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "id": data.ID})
}

func (d *PipelineDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data pipelineModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading a pipeline.")
		return
	}
	pipeline, err := d.client.GetPipeline(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst pipeline", err.Error())
		return
	}
	data.Name = types.StringValue(pipeline.Name)
	data.Status = types.StringPointerValue(pipeline.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
