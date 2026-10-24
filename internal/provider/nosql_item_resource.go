// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"

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

type NoSQLItemResource struct{ client *catalyst.Client }
type noSQLItemModel struct {
	RoutingScope types.Object         `tfsdk:"routing_scope"`
	ID           types.String         `tfsdk:"id"`
	ProjectID    types.String         `tfsdk:"project_id"`
	TableID      types.String         `tfsdk:"table_id"`
	Keys         jsontypes.Normalized `tfsdk:"keys_json"`
	Item         jsontypes.Normalized `tfsdk:"item_json"`
}

var _ resource.ResourceWithConfigure = &NoSQLItemResource{}
var _ resource.ResourceWithValidateConfig = &NoSQLItemResource{}
var _ resource.ResourceWithImportState = &NoSQLItemResource{}

func NewNoSQLItemResource() resource.Resource { return withEnvironmentScope(&NoSQLItemResource{}) }
func (r *NoSQLItemResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_nosql_item"
}
func (r *NoSQLItemResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a persistent configuration/seed item in an existing ONLINE NoSQL table. This does not provision tables or indexes and is not intended for application-owned transactional data. Both JSON fields use Catalyst typed attributes (S/N/B/BOOL/NULL/M/L/SS/SN/SB); jsonencode is recommended. item_json includes the configured primary keys and owns the complete item: removed non-key attributes are deleted remotely. Primary-key/project/table changes replace the item. Creation refuses existing keys and uses a server-side condition to prevent duplicate overwrite. Read uses consistent_read=true; per-item operation status is checked even on HTTP 200. Ambiguous writes are never replayed. Destroy deletes only the managed item and confirms absence; table/access failures retain state. Values and encoded keys remain in Terraform state, despite being sensitive. The provider limits each JSON field to 256 KiB. Import as project_id/table_id/base64url(keys_json), without base64 padding. Requires NoSQL table metadata read and item create/read/update/delete scopes. Live validation is limited to India Development.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, Sensitive: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Composite identity containing base64url-encoded primary keys. Encoding is not encryption; secure Terraform state."},
		"project_id": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Existing project ID."},
		"table_id":   schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Existing ONLINE NoSQL table ID."},
		"keys_json":  schema.StringAttribute{Required: true, Sensitive: true, CustomType: jsontypes.NormalizedType{}, PlanModifiers: replace, MarkdownDescription: "Typed JSON object containing exactly the table partition key and optional sort key. Values must match item_json."},
		"item_json":  schema.StringAttribute{Required: true, Sensitive: true, CustomType: jsontypes.NormalizedType{}, MarkdownDescription: "Complete desired typed JSON item, including primary keys. Attributes removed from configuration are removed remotely. Keep application-owned records outside this resource."},
	}}
}
func (r *NoSQLItemResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client.")
		return
	}
	r.client = client
}
func (r *NoSQLItemResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing a NoSQL item.")
		return false
	}
	return true
}
func (r *NoSQLItemResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var d noSQLItemModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for name, value := range map[string]types.String{"project_id": d.ProjectID, "table_id": d.TableID} {
		if !value.IsNull() && !value.IsUnknown() && !catalyst.ValidID(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid identifier", "Use a positive integer written as a string.")
		}
	}
	for name, value := range map[string]jsontypes.Normalized{"keys_json": d.Keys, "item_json": d.Item} {
		if !value.IsNull() && !value.IsUnknown() {
			if _, err := catalyst.ParseNoSQLItem(value.ValueString()); err != nil {
				resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid typed NoSQL JSON", err.Error())
			}
		}
	}
}
func (d *noSQLItemModel) populate(value string) {
	// Preserve configured formatting/set ordering when the typed item is equivalent.
	previous, err := catalyst.CanonicalNoSQLItem(d.Item.ValueString())
	if err == nil && previous == value {
		return
	}
	d.Item = jsontypes.NewNormalizedValue(value)
}
func (r *NoSQLItemResource) persist(ctx context.Context, d *noSQLItemModel, creating bool, diags *diag.Diagnostics, setState func()) {
	id, err := catalyst.NoSQLItemIdentity(d.ProjectID.ValueString(), d.TableID.ValueString(), d.Keys.ValueString())
	if err != nil {
		diags.AddError("Invalid NoSQL item identity", err.Error())
		return
	}
	if !creating && d.ID.ValueString() != id {
		diags.AddError("NoSQL item replacement required", "Project, table and primary keys must match the stored item identity.")
		return
	}
	accepted, err := r.client.WriteNoSQLItem(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.Keys.ValueString(), d.Item.ValueString(), creating)
	if accepted {
		d.ID = types.StringValue(id)
		setState()
	}
	if err != nil {
		diags.AddError("Unable to write NoSQL item", err.Error())
		return
	}
	if err = r.client.WaitNoSQLItem(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.Keys.ValueString(), d.Item.ValueString(), false); err != nil {
		diags.AddError("Unable to verify NoSQL item", "The item identity is retained for recovery: "+err.Error())
		return
	}
	current, err := r.client.ReadNoSQLItem(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.Keys.ValueString())
	if err != nil {
		diags.AddError("Unable to read persisted NoSQL item", err.Error())
		return
	}
	d.populate(current)
	setState()
}
func (r *NoSQLItemResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var d noSQLItemModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &d, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &d)...) })
}
func (r *NoSQLItemResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var d noSQLItemModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &d, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &d)...) })
}
func (r *NoSQLItemResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var d noSQLItemModel
	resp.Diagnostics.Append(req.State.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	value, err := r.client.ReadNoSQLItem(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.Keys.ValueString())
	if errors.Is(err, catalyst.ErrNoSQLItemNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read NoSQL item", err.Error())
		return
	}
	d.populate(value)
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *NoSQLItemResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var d noSQLItemModel
	resp.Diagnostics.Append(req.State.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if err := r.client.DeleteNoSQLItem(ctx, d.ProjectID.ValueString(), d.TableID.ValueString(), d.Keys.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to confirm NoSQL item deletion", err.Error())
	}
}
func (r *NoSQLItemResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	project, table, keys, err := catalyst.ParseNoSQLItemIdentity(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid NoSQL item import", err.Error())
		return
	}
	id, err := catalyst.NoSQLItemIdentity(project, table, keys)
	if err != nil {
		resp.Diagnostics.AddError("Invalid NoSQL item identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), project)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("table_id"), table)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("keys_json"), jsontypes.NewNormalizedValue(keys))...)
}
