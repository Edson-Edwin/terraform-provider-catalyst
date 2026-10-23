// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"maps"
	"strings"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &StratusObjectResource{}
var _ resource.ResourceWithImportState = &StratusObjectResource{}
var _ resource.ResourceWithValidateConfig = &StratusObjectResource{}

type StratusObjectResource struct{ client *catalyst.Client }
type stratusObjectModel struct {
	MultipartMiB types.Int64  `tfsdk:"multipart_part_size_mb"`
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	Bucket       types.String `tfsdk:"bucket_name"`
	Key          types.String `tfsdk:"key"`
	Source       types.String `tfsdk:"source"`
	SHA256       types.String `tfsdk:"source_sha256"`
	ContentType  types.String `tfsdk:"content_type"`
	Size         types.Int64  `tfsdk:"size"`
	ETag         types.String `tfsdk:"etag"`
	Metadata     types.Map    `tfsdk:"metadata"`
}

func NewStratusObjectResource() resource.Resource { return &StratusObjectResource{} }
func (r *StratusObjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stratus_object"
}
func (r *StratusObjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage an object in an existing unversioned Stratus bucket. Uploads local bytes and content type, supports in-place content updates, import, SHA-256 drift detection and confirmed deletion. Files are limited to 100 MiB. Single upload is the default; multipart_part_size_mb opts into multipart transfers. Versioned keys and TTL are not supported. Owns the complete custom metadata map; removing entries clears them remotely. The key must belong exclusively to this resource and have no version history. Destroy permanently removes the managed key. Creation checks absence and uses overwrite=false; existing keys require explicit import. Temporary signatures remain in memory; transfers use the expected regional Stratus origin without OAuth headers. Source contents are not stored in state. Requires bucket read and object read/create/update/delete scopes; metadata updates need ZohoCatalyst.buckets.objects.UPDATE. Import as project_id/bucket_name/object_key, retaining nested slashes. Multipart transfer settings are local and must be supplied again after import. Incomplete sessions are reported with an upload ID; a public abort/resume lifecycle has not been established. Live validation is limited to India Development.",
		Attributes: map[string]schema.Attribute{
			"multipart_part_size_mb": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), MarkdownDescription: "0 (default) uses one upload; 5–20 opts into multipart uploads with that part size in MiB. The 100 MiB source limit still applies. Incomplete sessions are reported with their upload ID; no public abort contract is established. Changing this local transfer option reuploads the object."},
			"id":                     schema.StringAttribute{Computed: true, MarkdownDescription: "Composite project_id/bucket_name/object_key identity.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id":             schema.StringAttribute{Required: true, MarkdownDescription: "Existing project ID.", PlanModifiers: replace},
			"bucket_name":            schema.StringAttribute{Required: true, MarkdownDescription: "Existing unversioned bucket's lowercase DNS name.", PlanModifiers: replace},
			"key":                    schema.StringAttribute{Required: true, MarkdownDescription: "Object key, up to 1024 UTF-8 bytes. Nested paths are supported; absolute paths, dot segments, control characters and backslashes are rejected.", PlanModifiers: replace},
			"source":                 schema.StringAttribute{Required: true, MarkdownDescription: "Local regular file, up to 100 MB. Build or prepare it before applying."},
			"source_sha256":          schema.StringAttribute{Required: true, MarkdownDescription: "Lowercase hex SHA-256, usually filesha256(source). Refresh downloads bytes to detect changes independently of ETag."},
			"content_type":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("application/octet-stream"), MarkdownDescription: "Canonical MIME content type; defaults to application/octet-stream."},
			"size":                   schema.Int64Attribute{Computed: true, MarkdownDescription: "Remote object size in bytes."},
			"metadata":               schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType, Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})), MarkdownDescription: "Complete custom metadata map. Keys/values use ASCII letters, digits, spaces, underscores and hyphens; total size is limited to 2047 characters including delimiters. Empty clears all metadata. Values remain in Terraform state; do not put secrets here."},
			"etag":                   schema.StringAttribute{Computed: true, MarkdownDescription: "Opaque ETag returned by Stratus; source_sha256 verifies actual content."},
		}}
}
func (r *StratusObjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *StratusObjectResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing Stratus objects.")
		return false
	}
	return true
}
func (r *StratusObjectResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data stratusObjectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.MultipartMiB.IsUnknown() && !data.MultipartMiB.IsNull() {
		v := data.MultipartMiB.ValueInt64()
		if v != 0 && (v < 5 || v > 20) {
			resp.Diagnostics.AddAttributeError(path.Root("multipart_part_size_mb"), "Invalid multipart part size", "Use 0 for single upload or a part size of 5–20 MiB.")
		}
	}
	for key, value := range map[string]types.String{"project_id": data.ProjectID, "bucket_name": data.Bucket, "key": data.Key, "source": data.Source, "source_sha256": data.SHA256, "content_type": data.ContentType} {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		v := value.ValueString()
		valid := false
		switch key {
		case "project_id":
			valid = catalyst.ValidID(v)
		case "bucket_name":
			valid = catalyst.ValidStratusBucketName(v)
		case "key":
			valid = catalyst.ValidStratusObjectKey(v)
		case "source":
			valid = strings.TrimSpace(v) != ""
		case "source_sha256":
			valid = sha256Pattern.MatchString(v)
		case "content_type":
			valid = catalyst.ValidObjectContentType(v)
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(path.Root(key), "Invalid Stratus object configuration", "Provide a valid "+key+" matching the documented supported values.")
		}
	}
	if !data.Metadata.IsNull() && !data.Metadata.IsUnknown() {
		var metadata map[string]string
		resp.Diagnostics.Append(data.Metadata.ElementsAs(ctx, &metadata, false)...)
		if !resp.Diagnostics.HasError() && !catalyst.ValidStratusMetadata(metadata) {
			resp.Diagnostics.AddAttributeError(path.Root("metadata"), "Invalid Stratus metadata", "Use ASCII letters, digits, spaces, underscores or hyphens, within 2047 total characters including delimiters.")
		}
	}

}
func (d *stratusObjectModel) populate(value *catalyst.StratusObject, hash string) {
	if d.MultipartMiB.IsNull() || d.MultipartMiB.IsUnknown() {
		d.MultipartMiB = types.Int64Value(0)
	}
	values := map[string]attr.Value{}
	for key, v := range value.Metadata {
		values[key] = types.StringValue(v)
	}
	d.Metadata = types.MapValueMust(types.StringType, values)
	d.ID = types.StringValue(d.ProjectID.ValueString() + "/" + d.Bucket.ValueString() + "/" + d.Key.ValueString())
	d.SHA256 = types.StringValue(hash)
	d.ContentType = types.StringValue(value.ContentType)
	d.Size = types.Int64Value(int64(*value.Size))
	d.ETag = types.StringValue(value.ETag)
}
func (r *StratusObjectResource) persist(ctx context.Context, data *stratusObjectModel, creating bool, diags *diag.Diagnostics, setState func()) {
	var metadata map[string]string
	diags.Append(data.Metadata.ElementsAs(ctx, &metadata, false)...)
	if diags.HasError() {
		return
	}
	if !catalyst.ValidStratusMetadata(metadata) {
		diags.AddError("Invalid Stratus metadata", "Use supported characters within the 2047 character limit.")
		return
	}

	body, err := readUploadSource(data.Source.ValueString(), data.SHA256.ValueString())
	if err != nil {
		diags.AddError("Invalid object source", err.Error())
		return
	}
	var accepted bool
	if data.MultipartMiB.ValueInt64() == 0 {
		accepted, err = r.client.PutStratusObject(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString(), data.ContentType.ValueString(), body, creating)
	} else {
		accepted, err = r.client.PutStratusObjectMultipart(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString(), data.ContentType.ValueString(), body, creating, data.MultipartMiB.ValueInt64())
	}
	if accepted {
		data.ID = types.StringValue(data.ProjectID.ValueString() + "/" + data.Bucket.ValueString() + "/" + data.Key.ValueString())
		setState()
		if diags.HasError() {
			return
		}
	}
	if err != nil {
		diags.AddError("Unable to upload Stratus object", err.Error()+" If the upload outcome is uncertain, inspect the key and import it only if you own it before retrying.")
		return
	}
	current, err := r.client.WaitStratusObject(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString(), data.ContentType.ValueString(), data.SHA256.ValueString(), int64(len(body)))
	if err != nil {
		diags.AddError("Unable to verify Stratus object", "The confirmed upload identity is retained for recovery. "+err.Error())
		return
	}
	if !maps.Equal(current.Metadata, metadata) {
		if err = r.client.PutStratusObjectMetadata(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString(), metadata); err != nil {
			diags.AddError("Unable to update Stratus metadata", "The object identity is retained for recovery: "+err.Error())
			return
		}
		current, err = r.client.WaitStratusObject(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString(), data.ContentType.ValueString(), data.SHA256.ValueString(), int64(len(body)), metadata)
		if err != nil {
			diags.AddError("Unable to verify Stratus metadata", "The object identity is retained for recovery: "+err.Error())
			return
		}
	}
	data.populate(current, data.SHA256.ValueString())
	setState()
}
func (r *StratusObjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data stratusObjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *StratusObjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data stratusObjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *StratusObjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data stratusObjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	current, hash, err := r.client.ReadStratusObject(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString())
	if errors.Is(err, catalyst.ErrStratusObjectNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Stratus object", err.Error())
		return
	}
	data.populate(current, hash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *StratusObjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data stratusObjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if err := r.client.DeleteStratusObject(ctx, data.ProjectID.ValueString(), data.Bucket.ValueString(), data.Key.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to confirm Stratus deletion", err.Error())
	}
}
func (r *StratusObjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || !catalyst.ValidID(parts[0]) || !catalyst.ValidStratusBucketName(parts[1]) || !catalyst.ValidStratusObjectKey(parts[2]) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use project_id/bucket_name/object_key; retain slashes in nested keys.")
		return
	}
	for name, value := range map[string]string{"id": req.ID, "project_id": parts[0], "bucket_name": parts[1], "key": parts[2]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), value)...)
	}
}
