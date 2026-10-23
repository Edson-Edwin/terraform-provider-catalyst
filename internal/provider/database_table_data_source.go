// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"encoding/json"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type DatabaseTableDataSource struct {
	client *catalyst.Client
	nosql  bool
}
type databaseTableModel struct {
	ProjectID types.String         `tfsdk:"project_id"`
	ID        types.String         `tfsdk:"id"`
	Name      types.String         `tfsdk:"name"`
	Schema    jsontypes.Normalized `tfsdk:"schema_json"`
}

var _ datasource.DataSourceWithConfigure = &DatabaseTableDataSource{}
var _ datasource.DataSourceWithValidateConfig = &DatabaseTableDataSource{}

func NewNoSQLTableDataSource() datasource.DataSource     { return &DatabaseTableDataSource{nosql: true} }
func NewDataStoreTableDataSource() datasource.DataSource { return &DatabaseTableDataSource{} }
func (d *DatabaseTableDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	suffix := "datastore_table"
	if d.nosql {
		suffix = "nosql_table"
	}
	resp.TypeName = req.ProviderTypeName + "_" + suffix
}
func (d *DatabaseTableDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	description := "Read an existing Data Store table and column metadata. schema_json contains table_id, table_name and columns sorted by column_id (column_id, table_id, column_name, data_type, is_mandatory, is_unique, search_index_enabled and max_length). Defaults, creator profiles and row values are excluded. Requires table/column READ access. Table and column creation remain console prerequisites; this data source does not provision schemas."
	if d.nosql {
		description = "Read an existing NoSQL table's identity, status, primary keys, additional sort keys, TTL settings and index identities/statuses. schema_json contains the allowlisted public metadata fields, excluding items, metrics and creator profiles. Requires NoSQL table READ access. This data source does not create tables, indexes or TTL configuration."
	}
	resp.Schema = schema.Schema{MarkdownDescription: description + " Denied access, null, incomplete or mismatched responses return an error. Live validation is limited to India Development.", Attributes: map[string]schema.Attribute{
		"project_id":  schema.StringAttribute{Required: true, MarkdownDescription: "Existing project ID, written as a string."},
		"id":          schema.StringAttribute{Required: true, MarkdownDescription: "Existing table ID, written as a string."},
		"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Verified table name reported by Catalyst."},
		"schema_json": schema.StringAttribute{Computed: true, CustomType: jsontypes.NormalizedType{}, MarkdownDescription: "Allowlisted table metadata as JSON. Use jsondecode to read its fields. Does not contain application data or default values."},
	}}
}
func (d *DatabaseTableDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client.")
		return
	}
	d.client = c
}
func (d *DatabaseTableDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var m databaseTableModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for name, value := range map[string]types.String{"project_id": m.ProjectID, "id": m.ID} {
		if !value.IsNull() && !value.IsUnknown() && !catalyst.ValidID(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid table identifier", "Use a positive numeric string.")
		}
	}
}
func (d *DatabaseTableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m databaseTableModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading table metadata.")
		return
	}
	if !catalyst.ValidID(m.ProjectID.ValueString()) || !catalyst.ValidID(m.ID.ValueString()) {
		resp.Diagnostics.AddError("Invalid table identity", "Project and table IDs must be known positive numeric strings.")
		return
	}
	var metadata any
	if d.nosql {
		table, err := d.client.GetNoSQLTable(ctx, m.ProjectID.ValueString(), m.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to read NoSQL table", err.Error())
			return
		}
		m.Name = types.StringValue(table.Name)
		metadata = table
	} else {
		table, err := d.client.GetDataStoreSchema(ctx, m.ProjectID.ValueString(), m.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to read Data Store table", err.Error())
			return
		}
		m.Name = types.StringValue(table.Name)
		metadata = table
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode schema", "The table metadata could not be represented as JSON.")
		return
	}
	m.Schema = jsontypes.NewNormalizedValue(string(raw))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
