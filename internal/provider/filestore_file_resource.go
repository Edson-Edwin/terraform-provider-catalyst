// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &FilestoreFileResource{}
var _ resource.ResourceWithImportState = &FilestoreFileResource{}
var _ resource.ResourceWithValidateConfig = &FilestoreFileResource{}
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type FilestoreFileResource struct{ client *catalyst.Client }
type filestoreFileModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	FolderID  types.String `tfsdk:"folder_id"`
	Name      types.String `tfsdk:"name"`
	Source    types.String `tfsdk:"source"`
	SHA256    types.String `tfsdk:"source_sha256"`
	Size      types.Int64  `tfsdk:"size"`
}

const filestoreDeprecation = "Zoho has announced File Store sunset on 31 October 2026 and does not provide it to new projects/accounts. Use catalyst_stratus_object and data.catalyst_stratus_buckets for new storage. Plan migration of existing files and permissions separately; this provider does not migrate them. See https://docs.catalyst.zoho.com/en/announcements/2026/filestore-sunset/."

func NewFilestoreFileResource() resource.Resource { return &FilestoreFileResource{} }
func (r *FilestoreFileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_filestore_file"
}
func (r *FilestoreFileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{DeprecationMessage: filestoreDeprecation, MarkdownDescription: "Deprecated: Zoho has announced File Store sunset on 31 October 2026; use Stratus for new storage. See the [vendor announcement](https://docs.catalyst.zoho.com/en/announcements/2026/filestore-sunset/). Manage a file in an existing Catalyst File Store folder (maximum 100 MB). Requires `ZohoCatalyst.files.CREATE`, `ZohoCatalyst.files.READ`, `ZohoCatalyst.files.DELETE`, project read access and `ZohoCatalyst.folders.READ`. Files are immutable through the documented API: changing any input replaces the file and changes its ID. Every refresh downloads the file to verify its SHA-256; file contents are never stored in Terraform state. Folder creation is not supported. Catalyst recommends Stratus for new storage; this resource supports existing File Store workloads. Import with `project_id/folder_id/file_id`, then set `source` and `source_sha256` in configuration. Deletion is polled for up to two minutes. This provider does not delete folders or other files.", Attributes: map[string]schema.Attribute{
		"id":            schema.StringAttribute{Computed: true, MarkdownDescription: "File ID as a precision-preserving string.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Existing project's positive numeric ID.", PlanModifiers: replace},
		"folder_id":     schema.StringAttribute{Required: true, MarkdownDescription: "Existing File Store folder's positive numeric ID.", PlanModifiers: replace},
		"name":          schema.StringAttribute{Required: true, MarkdownDescription: "Remote file name, including extension, without directory separators.", PlanModifiers: replace},
		"source":        schema.StringAttribute{Required: true, MarkdownDescription: "Path to a local regular file available when Terraform applies. Use a path relative to the root module or an absolute path. The path is stored in state; file contents are not.", PlanModifiers: replace},
		"source_sha256": schema.StringAttribute{Required: true, MarkdownDescription: "Lowercase hex SHA-256 of the desired contents. Set to `filesha256(...)` for the source path. Apply verifies the local contents match this hash before uploading. Refresh records the remote hash so content drift plans replacement.", PlanModifiers: replace},
		"size":          schema.Int64Attribute{Computed: true, MarkdownDescription: "Remote file size in bytes."},
	}}
}
func (r *FilestoreFileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *FilestoreFileResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data filestoreFileModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for name, value := range map[string]types.String{"project_id": data.ProjectID, "folder_id": data.FolderID} {
		if !value.IsNull() && !value.IsUnknown() && !catalyst.ValidID(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid identifier", "Specify a positive numeric ID written as a string.")
		}
	}
	if !data.Name.IsNull() && !data.Name.IsUnknown() && !catalyst.ValidFilename(data.Name.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid file name", "Specify a file name without directory separators or control characters.")
	}
	if !data.Source.IsNull() && !data.Source.IsUnknown() && strings.TrimSpace(data.Source.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("source"), "Missing source path", "Specify the path to a local regular file.")
	}
	if !data.SHA256.IsNull() && !data.SHA256.IsUnknown() && !sha256Pattern.MatchString(data.SHA256.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("source_sha256"), "Invalid SHA-256", "Use filesha256(source_path), which returns 64 lowercase hexadecimal characters.")
	}
}
func (r *FilestoreFileResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing a file.")
		return false
	}
	return true
}
func readUploadSource(source, expectedHash string) ([]byte, error) {
	// Check before opening so directories, device files and FIFOs cannot become
	// uploads or block apply. Recheck the opened descriptor before reading.
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("source must be an accessible regular file")
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, errors.New("could not open source file")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > catalyst.MaxFileBytes {
		return nil, errors.New("source must be a regular file no larger than 100 MB")
	}
	content, err := io.ReadAll(io.LimitReader(file, catalyst.MaxFileBytes+1))
	if err != nil || int64(len(content)) > catalyst.MaxFileBytes {
		return nil, errors.New("could not read source within the 100 MB limit")
	}
	hash := sha256.Sum256(content)
	if hex.EncodeToString(hash[:]) != expectedHash {
		return nil, errors.New("source contents no longer match source_sha256; regenerate the Terraform plan before applying")
	}
	return content, nil
}
func (r *FilestoreFileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data filestoreFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	content, err := readUploadSource(data.Source.ValueString(), data.SHA256.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read upload source", err.Error())
		return
	}
	result, err := r.client.UploadFile(ctx, data.ProjectID.ValueString(), data.FolderID.ValueString(), data.Name.ValueString(), content)
	if result != nil && catalyst.ValidID(string(result.ID)) {
		data.ID = types.StringValue(string(result.ID))
		data.Size = types.Int64Value(int64(len(content)))
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to upload Catalyst file", err.Error())
		return
	}
	if result.Name != data.Name.ValueString() || int64(*result.Size) != int64(len(content)) {
		resp.Diagnostics.AddError("Unexpected uploaded file metadata", "Catalyst returned a different name or size. The file ID was retained in state for recovery.")
		return
	}
	hash, verifyErr := r.client.FileSHA256(ctx, data.ProjectID.ValueString(), data.FolderID.ValueString(), data.ID.ValueString())
	if verifyErr != nil {
		resp.Diagnostics.AddError("Unable to verify uploaded file", verifyErr.Error())
		return
	}
	if hash != data.SHA256.ValueString() {
		resp.Diagnostics.AddError("Uploaded file contents differ", "The downloaded SHA-256 does not match the source. The file ID was retained in state for recovery.")
		return
	}
	data.Size = types.Int64Value(int64(*result.Size))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *FilestoreFileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data filestoreFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	file, err := r.client.GetFile(ctx, data.ProjectID.ValueString(), data.FolderID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrFileNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst file", err.Error())
		return
	}
	hash, err := r.client.FileSHA256(ctx, data.ProjectID.ValueString(), data.FolderID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrFileNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to verify Catalyst file contents", err.Error())
		return
	}
	data.Name = types.StringValue(file.Name)
	data.Size = types.Int64Value(int64(*file.Size))
	data.SHA256 = types.StringValue(hash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *FilestoreFileResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected file update", "File changes require replacement. Please report this provider bug.")
}
func (r *FilestoreFileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data filestoreFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	err := r.client.DeleteFile(ctx, data.ProjectID.ValueString(), data.FolderID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrFileNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete Catalyst file", err.Error())
		return
	}
	if err = r.client.WaitFileDeleted(ctx, data.ProjectID.ValueString(), data.FolderID.ValueString(), data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to confirm file deletion", err.Error())
	}
}
func (r *FilestoreFileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids := strings.Split(req.ID, "/")
	if len(ids) != 3 || !catalyst.ValidID(ids[0]) || !catalyst.ValidID(ids[1]) || !catalyst.ValidID(ids[2]) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use project_id/folder_id/file_id with three positive numeric IDs.")
		return
	}
	for index, name := range []string{"project_id", "folder_id", "id"} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), ids[index])...)
	}
}
