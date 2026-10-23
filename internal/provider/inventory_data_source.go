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

var _ datasource.DataSourceWithConfigure = &InventoryDataSource{}
var _ datasource.DataSourceWithValidateConfig = &InventoryDataSource{}

// InventoryDataSource shares the identical read-only contract of project inventories.
// Each constructor fixes its API, Terraform type, collection and OAuth scope.
type InventoryDataSource struct {
	client                          *catalyst.Client
	suffix, collection, description string
	list                            func(*catalyst.Client, context.Context, string) ([]catalyst.NamedResource, error)
}
type inventoryItemModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}
type bucketItemModel struct {
	Name types.String `tfsdk:"name"`
	URL  types.String `tfsdk:"url"`
}

func NewTablesDataSource() datasource.DataSource {
	return &InventoryDataSource{suffix: "datastore_tables", collection: "tables", description: "List existing Data Store tables. Requires `ZohoCatalyst.tables.READ`. Returns table identities only; it does not read rows or manage table schemas.", list: (*catalyst.Client).ListTables}
}
func NewFoldersDataSource() datasource.DataSource {
	return &InventoryDataSource{suffix: "filestore_folders", collection: "folders", description: "List existing File Store folders. Requires `ZohoCatalyst.folders.READ`. It does not download files or create folders.", list: (*catalyst.Client).ListFolders}
}
func NewCacheSegmentsDataSource() datasource.DataSource {
	return &InventoryDataSource{suffix: "cache_segments", collection: "segments", description: "List existing Cache segments using the official Catalyst CLI's read endpoint. Requires `ZohoCatalyst.segments.ALL`. It does not read cached values. Segment creation is console-only in the published API documentation.", list: (*catalyst.Client).ListCacheSegments}
}
func NewBucketsDataSource() datasource.DataSource {
	return &InventoryDataSource{suffix: "stratus_buckets", collection: "buckets", description: "List existing Stratus buckets. Requires `ZohoCatalyst.buckets.READ` and Stratus availability in the project. It does not read objects or manage buckets. Bucket names are their documented identities."}
}
func (d *InventoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.suffix
}
func (d *InventoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Name reported by Catalyst."},
	}
	order := "ID"
	if d.collection == "buckets" {
		order = "name"
		attributes["url"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Bucket HTTPS endpoint reported by Catalyst. The provider does not make requests to this endpoint."}
	} else {
		attributes["id"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Numeric ID represented as a string without loss of precision."}
	}
	resp.Schema = schema.Schema{MarkdownDescription: d.description + " Results are sorted lexicographically by " + order + ". An empty project returns an empty list; denied access or malformed responses return an error.", Attributes: map[string]schema.Attribute{
		"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID, written as a string."},
		d.collection: schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Existing infrastructure, sorted by " + order + ".", NestedObject: schema.NestedAttributeObject{Attributes: attributes}},
	}}
}
func (d *InventoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *InventoryDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("project_id"), &id)...)
	if resp.Diagnostics.HasError() || id.IsNull() || id.IsUnknown() {
		return
	}
	if !catalyst.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("project_id"), "Invalid project ID", "Specify a positive integer written as a string.")
	}
}
func (d *InventoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("project_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !catalyst.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("project_id"), "Invalid project ID", "Specify a known positive integer written as a string.")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading infrastructure.")
		return
	}
	var result any
	if d.collection == "buckets" {
		items, err := d.client.ListBuckets(ctx, id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to list Catalyst buckets", err.Error())
			return
		}
		models := make([]bucketItemModel, 0, len(items))
		for _, item := range items {
			models = append(models, bucketItemModel{Name: types.StringValue(item.Name), URL: types.StringValue(item.URL)})
		}
		result = models
	} else {
		items, err := d.list(d.client, ctx, id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to list Catalyst infrastructure", err.Error())
			return
		}
		models := make([]inventoryItemModel, 0, len(items))
		for _, item := range items {
			models = append(models, inventoryItemModel{ID: types.StringValue(string(item.ID)), Name: types.StringValue(item.Name)})
		}
		result = models
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(d.collection), result)...)
}

func NewFunctionsDataSource() datasource.DataSource {
	return &InventoryDataSource{suffix: "functions", collection: "functions", description: "List existing serverless Function identities. Requires `ZohoCatalyst.functions.ALL` and project access. Uses the official CLI's metadata API. It does not deploy or invoke Functions and does not expose environment variables.", list: (*catalyst.Client).ListFunctionIdentities}
}
