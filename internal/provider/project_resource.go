// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithConfigure = &ProjectResource{}
var _ resource.ResourceWithImportState = &ProjectResource{}
var _ resource.ResourceWithValidateConfig = &ProjectResource{}

type ProjectResource struct{ client *catalyst.Client }
type projectResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	ProjectType    types.String `tfsdk:"project_type"`
	Domain         types.String `tfsdk:"domain"`
	DomainName     types.String `tfsdk:"domain_name"`
	Timezone       types.String `tfsdk:"timezone"`
	Protected      types.Bool   `tfsdk:"deletion_protection"`
	Timeouts       types.Object `tfsdk:"timeouts"`
	DataCenter     types.String `tfsdk:"data_center"`
	OrganizationID types.String `tfsdk:"organization_id"`
}

func NewProjectResource() resource.Resource { return &ProjectResource{} }
func (r *ProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}
func (r *ProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Catalyst project's creation, reads, import and deletion. " +
			"Experimental: a successful live create/delete lifecycle is still pending; the tested OAuth context rejected direct creation with PROJECT_CREATION_NOT_ALLOWED and a CLI-version message. The official CLI uses console-assisted creation; a supported non-interactive route for this context needs vendor clarification. " +
			"Requires ZOHOCATALYST.projects.CREATE, .Read and .Delete scopes. " +
			"Name changes replace the entire project. Deletion protection defaults to true; apply deletion_protection=false before destroying. " +
			"Project deletion permanently removes all contents, including Production applications. " +
			"Use resource references for child dependencies and Terraform lifecycle.prevent_destroy for additional protection. " +
			"Import by project ID enables local deletion protection. Existing names are refused during creation; exclusive ownership across states and deployment tools is required. " +
			"Ambiguous writes are not retried. Created IDs are saved before waiting for readable metadata. " +
			"An explicit provider organization_id is required. State records data center and organization; changing that routing is rejected. " +
			"Loss of project visibility retains state and stops the operation instead of recreating the project.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Project ID as a precision-preserving string.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique project name, 1–50 characters. Renaming replaces the whole project.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true, Computed: true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Local Terraform deletion guard. Defaults to true, including after import. Apply false before destroy. Changing this flag does not modify Catalyst.",
			},
			"timeouts":        projectTimeoutsSchema(),
			"data_center":     schema.StringAttribute{Computed: true, MarkdownDescription: "Data center bound to this project in Terraform state. Provider routing must continue to match.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"organization_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Explicit provider organization bound to this project in Terraform state. This records routing, not authenticated caller identity.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_type":    schema.StringAttribute{Computed: true, MarkdownDescription: "Project type reported by Catalyst; null if absent."},
			"domain":          schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain reported by Catalyst; null if absent."},
			"domain_name":     schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain name reported by Catalyst; null if absent."},
			"timezone":        schema.StringAttribute{Computed: true, MarkdownDescription: "Project timezone reported by Catalyst; null if absent. Timezone updates are not managed by this resource."},
		},
	}
}
func (r *ProjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *ProjectResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing projects.")
		return false
	}
	return true
}

// Check persisted routing before any request. Legacy states acquire a binding
// only after a successful read; destructive operations require that refresh.
func (r *ProjectResource) checkScope(d *projectResourceModel, requireBound bool, diags *diag.Diagnostics) bool {
	dc, org, err := r.client.ProjectManagementScope()
	if err != nil {
		diags.AddError("Explicit project organization required", err.Error())
		return false
	}
	unbound := d.DataCenter.IsNull() || d.DataCenter.IsUnknown() || d.OrganizationID.IsNull() || d.OrganizationID.IsUnknown()
	if (!d.DataCenter.IsNull() && !d.DataCenter.IsUnknown() && d.DataCenter.ValueString() != dc) ||
		(!d.OrganizationID.IsNull() && !d.OrganizationID.IsUnknown() && d.OrganizationID.ValueString() != org) {
		diags.AddError("Project routing changed", "The provider data_center or organization_id differs from the project's saved scope. Restore the original provider routing. Changing providers does not move a project; verify any transfer independently before importing it into its new scope.")
		return false
	}
	if requireBound && unbound {
		diags.AddError("Project scope is not recorded", "Refresh and apply the existing project with its original explicit organization and data center before updating or deleting it. Unbound legacy state cannot authorize a destructive operation.")
		return false
	}
	return true
}

func (r *ProjectResource) bindScope(d *projectResourceModel) {
	dc, org, _ := r.client.ProjectManagementScope() // checkScope already validated it.
	d.DataCenter, d.OrganizationID = types.StringValue(dc), types.StringValue(org)
}

func projectVisibilityError(diags *diag.Diagnostics) {
	diags.AddError("Project visibility lost; state retained", "The project is absent from this caller's reads and listings. It may have been deleted or access may have been revoked. Restore the original credentials, organization and data center, then refresh. Only after an administrator independently confirms deletion should you remove the exact resource binding from state. Terraform will not automatically forget or recreate this project.")
}
func (r *ProjectResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var d projectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &d)...)
	if !d.Name.IsNull() && !d.Name.IsUnknown() && !catalyst.ValidProjectName(d.Name.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid project name", "Use 1–50 characters without surrounding whitespace or control characters.")
	}
}
func (d *projectResourceModel) populate(p *catalyst.Project) {
	d.ID = types.StringValue(string(p.ID))
	d.Name = types.StringValue(p.Name)
	optional := func(s string) types.String {
		if s == "" {
			return types.StringNull()
		}
		return types.StringValue(s)
	}
	d.ProjectType = optional(p.Type)
	d.Domain = optional(p.DomainDetails.URL)
	d.DomainName = optional(p.DomainDetails.Name)
	d.Timezone = optional(p.Timezone)
	if d.Protected.IsNull() || d.Protected.IsUnknown() {
		d.Protected = types.BoolValue(true)
	}
}
func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var d projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if !r.checkScope(&d, false, &resp.Diagnostics) {
		return
	}
	r.bindScope(&d)
	ctx, cancel := projectOperationContext(ctx, d.Timeouts, "create", 5*time.Minute, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.CreateProject(ctx, d.Name.ValueString())
	if p != nil && catalyst.ValidID(string(p.ID)) {
		d.ID = types.StringValue(string(p.ID))
		d.ProjectType = types.StringNull()
		d.Domain = types.StringNull()
		d.DomainName = types.StringNull()
		d.Timezone = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
	}
	if p != nil && catalyst.ValidID(string(p.ID)) && (err == nil || errors.Is(err, catalyst.ErrProjectMetadataIncomplete)) {
		p, err = r.client.WaitProjectReady(ctx, string(p.ID), d.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Catalyst project", err.Error())
		return
	}
	d.populate(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var d projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if !r.checkScope(&d, false, &resp.Diagnostics) {
		return
	}
	ctx, cancel := projectOperationContext(ctx, d.Timeouts, "read", 2*time.Minute, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.GetManagedProject(ctx, d.ID.ValueString())
	if errors.Is(err, catalyst.ErrProjectNotFound) {
		projectVisibilityError(&resp.Diagnostics)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst project", err.Error())
		return
	}
	r.bindScope(&d)
	d.populate(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var d, prior projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &d)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if !r.checkScope(&prior, true, &resp.Diagnostics) {
		return
	}
	if !r.checkScope(&d, false, &resp.Diagnostics) {
		return
	}
	r.bindScope(&d)
	if d.ID.ValueString() != prior.ID.ValueString() || d.Name.ValueString() != prior.Name.ValueString() {
		resp.Diagnostics.AddError("Project replacement required", "Project identity and name cannot be updated in place.")
		return
	}
	ctx, cancel := projectOperationContext(ctx, d.Timeouts, "update", 2*time.Minute, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.client.GetManagedProject(ctx, d.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to verify Catalyst project", err.Error())
		return
	}
	d.populate(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
}
func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var d projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &d)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.Protected.IsNull() || d.Protected.IsUnknown() || d.Protected.ValueBool() {
		resp.Diagnostics.AddError("Project deletion is protected", "Apply deletion_protection=false before destroying this project. Deletion permanently removes all project contents, including Production applications.")
		return
	}
	if !r.ready(&resp.Diagnostics) {
		return
	}
	if !r.checkScope(&d, true, &resp.Diagnostics) {
		return
	}
	ctx, cancel := projectOperationContext(ctx, d.Timeouts, "delete", 5*time.Minute, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProject(ctx, d.ID.ValueString(), d.Name.ValueString())
	if errors.Is(err, catalyst.ErrProjectNotFound) {
		projectVisibilityError(&resp.Diagnostics)
		return
	}
	if err == nil {
		err = r.client.WaitProjectDeleted(ctx, d.ID.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm project deletion", err.Error())
	}
}
func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !catalyst.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid project ID", "Import using a positive numeric project ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("deletion_protection"), true)...)
}
