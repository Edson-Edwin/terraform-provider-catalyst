// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type WebClientVersionsDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &WebClientVersionsDataSource{}
var _ datasource.DataSourceWithValidateConfig = &WebClientVersionsDataSource{}

type webClientVersionsModel struct {
	ProjectID types.String            `tfsdk:"project_id"`
	Versions  []webClientVersionModel `tfsdk:"versions"`
}

type webClientVersionModel struct {
	HistoryID string     `tfsdk:"history_id"`
	Version   string     `tfsdk:"version"`
	Live      types.Bool `tfsdk:"live"`
}

func NewWebClientVersionsDataSource() datasource.DataSource { return &WebClientVersionsDataSource{} }

func (d *WebClientVersionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_web_client_versions"
}

func (d *WebClientVersionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List existing Catalyst Web Client Hosting versions using the official CLI read contract. Requires `ZohoCatalyst.webapp.ALL` and project access. Reads metadata only. Verified with offline protocol tests.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"versions": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Version history sorted lexicographically by history ID.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"history_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Deployment history ID as a precision-preserving string."},
					"version":    schema.StringAttribute{Computed: true, MarkdownDescription: "Application version reported by Catalyst."},
					"live":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the version is live. Null when Catalyst omits status."},
				}}},
		},
	}
}

func (d *WebClientVersionsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data webClientVersionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID})
}

func (d *WebClientVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data webClientVersionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading Web Client Hosting history.")
		return
	}
	versions, err := d.client.ListWebClientVersions(ctx, data.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst Web Client Hosting versions", err.Error())
		return
	}
	data.Versions = make([]webClientVersionModel, 0, len(versions))
	for _, version := range versions {
		data.Versions = append(data.Versions, webClientVersionModel{
			HistoryID: string(version.HistoryID), Version: version.Version, Live: types.BoolPointerValue(version.Live),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
