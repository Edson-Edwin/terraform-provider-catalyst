// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type BrowserGridsDataSource struct{ infrastructureDetailClient }

type browserGridsModel struct {
	ProjectID types.String       `tfsdk:"project_id"`
	GridID    types.String       `tfsdk:"grid_id"`
	Grids     []browserGridModel `tfsdk:"grids"`
}

type browserGridModel struct {
	ID            string      `tfsdk:"id"`
	Name          string      `tfsdk:"name"`
	MemoryMB      types.Int64 `tfsdk:"memory_mb"`
	EndpointType  types.Int64 `tfsdk:"endpoint_type"`
	ConfigType    types.Int64 `tfsdk:"config_type"`
	MaxNodes      types.Int64 `tfsdk:"max_nodes"`
	MaxSessions   types.Int64 `tfsdk:"max_sessions"`
	MaxConcurrent types.Int64 `tfsdk:"max_concurrent"`
}

func NewBrowserGridsDataSource() datasource.DataSource { return &BrowserGridsDataSource{} }

func (d *BrowserGridsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_browser_grids"
}

func (d *BrowserGridsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read existing SmartBrowz Browser Grid configuration. Requires an explicit organization and `ZohoCatalyst.grid.read`. Returns IDs, names, configuration codes and capacity settings. Credentials, browser connection endpoints, browser sessions, and profile information are excluded. Verified offline.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID as a string."},
			"grid_id":    schema.StringAttribute{Optional: true, MarkdownDescription: "Existing grid's numeric ID. Supply it to return only that grid."},
			"grids": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Visible grids sorted lexicographically by ID.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Grid ID as a precision-preserving string."},
					"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "Configured grid name."},
					"memory_mb":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Configured node memory in MB, when reported."},
					"endpoint_type":  schema.Int64Attribute{Computed: true, MarkdownDescription: "Raw Catalyst endpoint type code, when reported."},
					"config_type":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Raw Catalyst configuration type code, when reported."},
					"max_nodes":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Configured maximum node count, when reported."},
					"max_sessions":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Configured maximum sessions per node, when reported."},
					"max_concurrent": schema.Int64Attribute{Computed: true, MarkdownDescription: "Configured maximum concurrent sessions across the grid, when reported."},
				}}},
		},
	}
}

func (d *BrowserGridsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data browserGridsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "grid_id": data.GridID})
}

func (d *BrowserGridsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data browserGridsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading Browser Grid metadata.")
		return
	}
	grids, err := d.client.ReadBrowserGrids(ctx, data.ProjectID.ValueString(), data.GridID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst Browser Grid metadata", err.Error())
		return
	}
	data.Grids = make([]browserGridModel, 0, len(grids))
	for _, grid := range grids {
		data.Grids = append(data.Grids, browserGridModel{
			ID: string(grid.ID), Name: grid.Name,
			MemoryMB: types.Int64PointerValue((*int64)(grid.MemoryMB)), EndpointType: types.Int64PointerValue((*int64)(grid.EndpointType)),
			ConfigType: types.Int64PointerValue((*int64)(grid.ConfigType)), MaxNodes: types.Int64PointerValue((*int64)(grid.MaxNodes)),
			MaxSessions: types.Int64PointerValue((*int64)(grid.MaxSessions)), MaxConcurrent: types.Int64PointerValue((*int64)(grid.MaxConcurrent)),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
