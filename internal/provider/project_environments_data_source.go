// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ProjectEnvironmentsDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &ProjectEnvironmentsDataSource{}
var _ datasource.DataSourceWithValidateConfig = &ProjectEnvironmentsDataSource{}

type projectEnvironmentsModel struct {
	ProjectID    types.String              `tfsdk:"project_id"`
	Environments []projectEnvironmentModel `tfsdk:"environments"`
}

type projectEnvironmentModel struct {
	ID      string       `tfsdk:"id"`
	Name    string       `tfsdk:"name"`
	Status  types.String `tfsdk:"status"`
	Type    types.Int64  `tfsdk:"type"`
	Default types.Bool   `tfsdk:"is_default"`
}

func NewProjectEnvironmentsDataSource() datasource.DataSource {
	return &ProjectEnvironmentsDataSource{}
}

func (d *ProjectEnvironmentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_environments"
}

func (d *ProjectEnvironmentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List existing environments and their reported status using the Catalyst CLI's environment lookup. The official CLI uses `ZohoCatalyst.projects.ALL` for this lookup. Environment activation and promotion are separate workflows. Verified offline.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"environments": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Existing environments sorted lexicographically by ID.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Environment ID as a precision-preserving string."},
					"name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Environment name reported by Catalyst."},
					"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "Raw status, when reported."},
					"type":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Numeric environment type, when reported."},
					"is_default": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Catalyst reports this environment as the default, when available."},
				}}},
		},
	}
}

func (d *ProjectEnvironmentsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data projectEnvironmentsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID})
}

func (d *ProjectEnvironmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data projectEnvironmentsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading project environments.")
		return
	}
	environments, err := d.client.ListProjectEnvironments(ctx, data.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst project environments", err.Error())
		return
	}
	data.Environments = make([]projectEnvironmentModel, 0, len(environments))
	for _, environment := range environments {
		data.Environments = append(data.Environments, projectEnvironmentModel{
			ID: string(environment.ID), Name: environment.Name, Status: types.StringPointerValue(environment.Status),
			Type: types.Int64PointerValue((*int64)(environment.Type)), Default: types.BoolPointerValue(environment.Default),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
