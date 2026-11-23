// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type DataStreamChannelsDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &DataStreamChannelsDataSource{}
var _ datasource.DataSourceWithValidateConfig = &DataStreamChannelsDataSource{}

type dataStreamChannelsModel struct {
	ProjectID types.String             `tfsdk:"project_id"`
	ChannelID types.String             `tfsdk:"channel_id"`
	Channels  []dataStreamChannelModel `tfsdk:"channels"`
}

type dataStreamChannelModel struct {
	ID             string      `tfsdk:"id"`
	Name           string      `tfsdk:"name"`
	ControlType    types.Int64 `tfsdk:"control_type"`
	RetentionHours types.Int64 `tfsdk:"retention_hours"`
}

func NewDataStreamChannelsDataSource() datasource.DataSource { return &DataStreamChannelsDataSource{} }

func (d *DataStreamChannelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datastream_channels"
}

func (d *DataStreamChannelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List existing Data Streams channels or look up one channel by ID. Requires `ZohoCatalyst.datastreams.channel.READ`. Returns configuration metadata without reading messages, generating subscriber tokens, or publishing data. Verified offline.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID as a string."},
			"channel_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Existing channel's numeric ID. When supplied, the result contains only that channel."},
			"channels": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Visible channels sorted lexicographically by ID. An empty successful inventory returns an empty list.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Channel ID as a precision-preserving string."},
					"name":            schema.StringAttribute{Computed: true, MarkdownDescription: "Channel name reported by Catalyst."},
					"control_type":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Raw access-control type code, when reported."},
					"retention_hours": schema.Int64Attribute{Computed: true, MarkdownDescription: "Stream retention duration in hours, when reported."},
				}}},
		},
	}
}

func (d *DataStreamChannelsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data dataStreamChannelsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "channel_id": data.ChannelID})
}

func (d *DataStreamChannelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data dataStreamChannelsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading Data Streams channels.")
		return
	}

	var channels []catalyst.DataStreamChannel
	if data.ChannelID.IsNull() {
		items, err := d.client.ListDataStreamChannels(ctx, data.ProjectID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to list Catalyst Data Streams channels", err.Error())
			return
		}
		channels = items
	} else {
		channel, err := d.client.GetDataStreamChannel(ctx, data.ProjectID.ValueString(), data.ChannelID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to read Catalyst Data Streams channel", err.Error())
			return
		}
		channels = []catalyst.DataStreamChannel{*channel}
	}
	data.Channels = make([]dataStreamChannelModel, 0, len(channels))
	for _, channel := range channels {
		data.Channels = append(data.Channels, dataStreamChannelModel{
			ID: string(channel.ID), Name: channel.Name,
			ControlType:    types.Int64PointerValue((*int64)(channel.ControlType)),
			RetentionHours: types.Int64PointerValue((*int64)(channel.RetentionHours)),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
