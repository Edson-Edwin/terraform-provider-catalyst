// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &DataStoreRowResource{}
var _ resource.ResourceWithValidateConfig = &DataStoreRowResource{}
var _ resource.ResourceWithImportState = &DataStoreRowResource{}

type DataStoreRowResource struct{ client *catalyst.Client }
type dataStoreRowModel struct {
	RoutingScope types.Object         `tfsdk:"routing_scope"`
	ID           types.String         `tfsdk:"id"`
	ProjectID    types.String         `tfsdk:"project_id"`
	TableID      types.String         `tfsdk:"table_id"`
	RowID        types.String         `tfsdk:"row_id"`
	Values       jsontypes.Normalized `tfsdk:"values_json"`
}

func NewDataStoreRowResource() resource.Resource {
	return withEnvironmentScope(&DataStoreRowResource{})
}
func (r *DataStoreRowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datastore_row"
}
func (r *DataStoreRowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage one persistent configuration/seed row in an existing Data Store table. Not for application-owned customer transactions. Supports scalar string/number/boolean/null columns, generated ROWID, import, in-place updates and drift detection. Owns columns present in values_json; removing a previously managed column explicitly clears it to null. Other columns are not adopted except on import, which initially reads all scalar application columns. System fields/profiles are excluded. Project/table changes replace the row. Ambiguous creates are never retried; if the response loses ROWID, inspect the table and import before retrying. Destroy deletes only the generated/imported row and confirms absence. Values remain in sensitive Terraform state. Requires table/schema READ and row CREATE/READ/UPDATE/DELETE OAuth scopes. Import as project_id/table_id/row_id. Table, schema, indexes and permissions are not provisioned.", Attributes: map[string]schema.Attribute{
		"id":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Composite project/table/row identity."},
		"project_id":  schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Existing project ID."},
		"table_id":    schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Existing Data Store table ID."},
		"row_id":      schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Server-generated ROWID, preserved as a string."},
		"values_json": schema.StringAttribute{Required: true, Sensitive: true, CustomType: jsontypes.NormalizedType{}, MarkdownDescription: "Nonempty scalar JSON object of owned application columns, within 256 KiB. System fields are forbidden. Removing a managed field clears it to null."},
	}}
}
func (r *DataStoreRowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client.")
		return
	}
	r.client = c
}
func (r *DataStoreRowResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var d dataStoreRowModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &d)...)
	validateAuthIDs(&resp.Diagnostics, map[string]types.String{"project_id": d.ProjectID, "table_id": d.TableID})
	if !d.Values.IsUnknown() && !d.Values.IsNull() {
		if _, err := catalyst.ParseDataStoreValues(d.Values.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("values_json"), "Invalid Data Store values", err.Error())
		}
	}
}
func (r *DataStoreRowResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing a Data Store row.")
		return false
	}
	return true
}
func (d *dataStoreRowModel) populate(row *catalyst.DataStoreRow) error {
	selection := ""
	if !d.Values.IsNull() && !d.Values.IsUnknown() {
		selection = d.Values.ValueString()
	}
	value, err := catalyst.DataStoreProjection(row, selection)
	if err != nil {
		return err
	}
	if selection == "" || !catalyst.DataStoreValuesEquivalent(selection, value) {
		d.Values = jsontypes.NewNormalizedValue(value)
	}
	d.RowID = types.StringValue(string(row.ID))
	d.ID = types.StringValue(d.ProjectID.ValueString() + "/" + d.TableID.ValueString() + "/" + string(row.ID))
	return nil
}
func (r *DataStoreRowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var d dataStoreRowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	row, err := r.client.CreateDataStoreRow(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.Values.ValueString())
	if row != nil && catalyst.ValidID(string(row.ID)) {
		d.RowID = types.StringValue(string(row.ID))
		d.ID = types.StringValue(d.ProjectID.ValueString() + "/" + d.TableID.ValueString() + "/" + string(row.ID))
		resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Data Store row", err.Error())
		return
	}
	row, err = r.client.WaitDataStoreRow(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.RowID.ValueString(), d.Values.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to verify Data Store row", err.Error())
		return
	}
	if err = d.populate(row); err != nil {
		resp.Diagnostics.AddError("Unable to map Data Store row", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *DataStoreRowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var d dataStoreRowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	row, err := r.client.GetDataStoreRow(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.RowID.ValueString())
	if errors.Is(err, catalyst.ErrDataStoreRowNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Data Store row", err.Error())
		return
	}
	if err = d.populate(row); err != nil {
		resp.Diagnostics.AddError("Unable to map Data Store row", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *DataStoreRowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var d, old dataStoreRowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &d)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if !d.ProjectID.Equal(old.ProjectID) || !d.TableID.Equal(old.TableID) {
		resp.Diagnostics.AddError("Row replacement required", "Project and table cannot change in place.")
		return
	}
	if err := r.client.UpdateDataStoreRow(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), old.RowID.ValueString(), d.Values.ValueString(), old.Values.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to update Data Store row", err.Error())
		return
	}
	d.RowID = old.RowID
	d.ID = old.ID
	row, err := r.client.WaitDataStoreRow(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.RowID.ValueString(), d.Values.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to verify Data Store row", err.Error())
		return
	}
	if err = d.populate(row); err != nil {
		resp.Diagnostics.AddError("Unable to map Data Store row", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *DataStoreRowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var d dataStoreRowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if err := r.client.DeleteDataStoreRow(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.RowID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Data Store row", err.Error())
	}
}
func (r *DataStoreRowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	p := strings.Split(req.ID, "/")
	if len(p) != 3 || !catalyst.ValidID(p[0]) || !catalyst.ValidID(p[1]) || !catalyst.ValidID(p[2]) {
		resp.Diagnostics.AddError("Invalid Data Store row import ID", "Use project_id/table_id/row_id with positive numeric IDs.")
		return
	}
	for k, v := range map[string]string{"id": req.ID, "project_id": p[0], "table_id": p[1], "row_id": p[2]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(k), v)...)
	}
}
