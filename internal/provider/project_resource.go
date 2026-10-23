// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"

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
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ProjectType types.String `tfsdk:"project_type"`
	Domain      types.String `tfsdk:"domain"`
	DomainName  types.String `tfsdk:"domain_name"`
	Timezone    types.String `tfsdk:"timezone"`
	Protected   types.Bool   `tfsdk:"deletion_protection"`
}

func NewProjectResource() resource.Resource { return &ProjectResource{} }
func (r *ProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}
func (r *ProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Create and delete a Catalyst project using the documented project API. Experimental: live creation is blocked in the current test account with PROJECT_CREATION_NOT_ALLOWED; full live lifecycle is not verified. Requires ZOHOCATALYST.projects.CREATE, .Read and .Delete scopes. Name changes replace the project. Deletion protection defaults to true; apply deletion_protection=false before destroying. Project deletion permanently removes the entire project and its contents, including Production applications. Use resource references for child dependencies and Terraform lifecycle.prevent_destroy for additional protection. Import by project ID; import enables local deletion protection. Creation refuses an existing name, but exclusive ownership across states/tools is still required. Ambiguous writes are not retried.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, MarkdownDescription: "Project ID as a precision-preserving string.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                schema.StringAttribute{Required: true, MarkdownDescription: "Unique project name, 1–50 characters. Renaming replaces the whole project.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"deletion_protection": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), MarkdownDescription: "Local Terraform deletion guard. Defaults to true, including after import. Apply false before destroy. Changing this flag does not modify Catalyst."},
		"project_type":        schema.StringAttribute{Computed: true, MarkdownDescription: "Project type reported by Catalyst; null if absent."},
		"domain":              schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain reported by Catalyst; null if absent."},
		"domain_name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Project domain name reported by Catalyst; null if absent."},
		"timezone":            schema.StringAttribute{Computed: true, MarkdownDescription: "Project timezone reported by Catalyst; null if absent. Timezone updates are not managed by this resource."},
	}}
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
	p, err := r.client.CreateProject(ctx, d.Name.ValueString())
	if p != nil && catalyst.ValidID(string(p.ID)) {
		d.ID = types.StringValue(string(p.ID))
		d.ProjectType = types.StringNull()
		d.Domain = types.StringNull()
		d.DomainName = types.StringNull()
		d.Timezone = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &d)...)
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
	p, err := r.client.GetManagedProject(ctx, d.ID.ValueString())
	if errors.Is(err, catalyst.ErrProjectNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst project", err.Error())
		return
	}
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
	if d.ID.ValueString() != prior.ID.ValueString() || d.Name.ValueString() != prior.Name.ValueString() {
		resp.Diagnostics.AddError("Project replacement required", "Project identity and name cannot be updated in place.")
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
	err := r.client.DeleteProject(ctx, d.ID.ValueString(), d.Name.ValueString())
	if errors.Is(err, catalyst.ErrProjectNotFound) {
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
