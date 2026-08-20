// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type SmartTrainMaterialsDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &SmartTrainMaterialsDataSource{}
var _ datasource.DataSourceWithValidateConfig = &SmartTrainMaterialsDataSource{}

type smartTrainMaterialsModel struct {
	ProjectID types.String              `tfsdk:"project_id"`
	BotID     types.String              `tfsdk:"bot_id"`
	Materials []smartTrainMaterialModel `tfsdk:"materials"`
}

type smartTrainMaterialModel struct {
	ID     string       `tfsdk:"id"`
	Title  types.String `tfsdk:"title"`
	Source types.String `tfsdk:"source"`
	Status types.String `tfsdk:"status"`
}

func NewSmartTrainMaterialsDataSource() datasource.DataSource {
	return &SmartTrainMaterialsDataSource{}
}

func (d *SmartTrainMaterialsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_smarttrain_materials"
}

func (d *SmartTrainMaterialsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List existing ConvoKraft SmartTrain material metadata for one bot in Development. Requires an explicit organization and `ZIAPlatform.Convokraft.RAG.READ`. The data source omits source URLs, material contents, and creator/modifier profiles. Verified offline against Zoho's full sample request path.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"bot_id":     schema.StringAttribute{Required: true, MarkdownDescription: "Existing developed ConvoKraft bot's positive numeric ID as a string."},
			"materials": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Existing material metadata sorted lexicographically by ID.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":     schema.StringAttribute{Computed: true, MarkdownDescription: "Material ID as a precision-preserving string."},
					"title":  schema.StringAttribute{Computed: true, MarkdownDescription: "Material title, when reported."},
					"source": schema.StringAttribute{Computed: true, MarkdownDescription: "Material source type, when reported."},
					"status": schema.StringAttribute{Computed: true, MarkdownDescription: "Training status, when reported."},
				}}},
		},
	}
}

func (d *SmartTrainMaterialsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data smartTrainMaterialsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "bot_id": data.BotID})
}

func (d *SmartTrainMaterialsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data smartTrainMaterialsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading SmartTrain materials.")
		return
	}
	materials, err := d.client.ListSmartTrainMaterials(ctx, data.ProjectID.ValueString(), data.BotID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst SmartTrain materials", err.Error())
		return
	}
	data.Materials = make([]smartTrainMaterialModel, 0, len(materials))
	for _, item := range materials {
		data.Materials = append(data.Materials, smartTrainMaterialModel{
			ID: string(item.ID), Title: types.StringPointerValue(item.Title),
			Source: types.StringPointerValue(item.Source), Status: types.StringPointerValue(item.Status),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
