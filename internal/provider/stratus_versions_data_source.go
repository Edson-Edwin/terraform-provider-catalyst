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

type StratusVersionsDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &StratusVersionsDataSource{}
var _ datasource.DataSourceWithValidateConfig = &StratusVersionsDataSource{}

type stratusVersionsModel struct {
	ProjectID  types.String          `tfsdk:"project_id"`
	BucketName types.String          `tfsdk:"bucket_name"`
	ObjectKey  types.String          `tfsdk:"object_key"`
	Versions   []stratusVersionModel `tfsdk:"versions"`
}

type stratusVersionModel struct {
	ID           string       `tfsdk:"version_id"`
	Latest       bool         `tfsdk:"is_latest"`
	Size         types.Int64  `tfsdk:"size_bytes"`
	ETag         types.String `tfsdk:"etag"`
	LastModified types.String `tfsdk:"last_modified"`
}

func NewStratusVersionsDataSource() datasource.DataSource { return &StratusVersionsDataSource{} }

func (d *StratusVersionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stratus_object_versions"
}

func (d *StratusVersionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read version IDs, latest flags and available metadata for one existing Stratus object in a versioned bucket. Requires `ZohoCatalyst.buckets.objects.READ`. Reads up to 100 pages of 100 versions and returns an error if more pages remain. Verified with offline protocol tests.",
		Attributes: map[string]schema.Attribute{
			"project_id":  schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"bucket_name": schema.StringAttribute{Required: true, MarkdownDescription: "Existing lowercase Stratus bucket name."},
			"object_key":  schema.StringAttribute{Required: true, MarkdownDescription: "Complete object key, including any folder prefix."},
			"versions": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Version records sorted by ID. A successful listing with zero versions returns an empty list.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"version_id":    schema.StringAttribute{Computed: true, MarkdownDescription: "Opaque version ID returned by Catalyst."},
					"is_latest":     schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Catalyst marks this version as the latest."},
					"size_bytes":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Version size in bytes when reported."},
					"etag":          schema.StringAttribute{Computed: true, MarkdownDescription: "Opaque version ETag when reported."},
					"last_modified": schema.StringAttribute{Computed: true, MarkdownDescription: "Last-modified value in the original service format when reported."},
				}}},
		},
	}
}

func (d *StratusVersionsDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data stratusVersionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateStratusLookup(&resp.Diagnostics, data.ProjectID, data.BucketName)
	if !data.ObjectKey.IsUnknown() && !data.ObjectKey.IsNull() && !catalyst.ValidStratusObjectKey(data.ObjectKey.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("object_key"), "Invalid Stratus object key", "Specify a valid relative object key, including any folder prefix.")
	}
}

func (d *StratusVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data stratusVersionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading Stratus object versions.")
		return
	}
	versions, err := d.client.ListStratusObjectVersions(ctx, data.ProjectID.ValueString(), data.BucketName.ValueString(), data.ObjectKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Stratus object versions", err.Error())
		return
	}
	data.Versions = make([]stratusVersionModel, 0, len(versions))
	for _, version := range versions {
		data.Versions = append(data.Versions, stratusVersionModel{
			ID: version.ID, Latest: version.Latest,
			Size: types.Int64PointerValue((*int64)(version.Size)), ETag: types.StringPointerValue(version.ETag),
			LastModified: types.StringPointerValue(version.LastModified),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
