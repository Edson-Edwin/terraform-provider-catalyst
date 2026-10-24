// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Keep the routing boundary outside service-specific lifecycle implementations.
// Projects are deliberately excluded: a project spans both environments.
type environmentResource interface {
	resource.Resource
	resource.ResourceWithConfigure
	resource.ResourceWithImportState
	resource.ResourceWithValidateConfig
}

type environmentScopedResource struct {
	inner  environmentResource
	client *catalyst.Client
}

var _ resource.ResourceWithModifyPlan = &environmentScopedResource{}

func withEnvironmentScope(inner environmentResource) resource.Resource {
	return &environmentScopedResource{inner: inner}
}

func (r *environmentScopedResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	r.inner.Metadata(ctx, req, resp)
}

func (r *environmentScopedResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.inner.Schema(ctx, req, resp)
	resp.Schema.Attributes["routing_scope"] = schema.SingleNestedAttribute{
		Computed:            true,
		MarkdownDescription: "Saved data center, environment and explicit organization routing. Provider routing must match before reads, plans or mutations. Legacy state binds only after a successful refresh with the original routing. This records configuration, not authenticated caller identity.",
		Attributes: map[string]schema.Attribute{
			"data_center":     schema.StringAttribute{Computed: true},
			"environment":     schema.StringAttribute{Computed: true},
			"organization_id": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *environmentScopedResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.inner.Configure(ctx, req, resp)
	if req.ProviderData != nil {
		r.client, _ = req.ProviderData.(*catalyst.Client)
	}
}

func (r *environmentScopedResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.inner.ValidateConfig(ctx, req, resp)
}

func (r *environmentScopedResource) scope(diags *diag.Diagnostics) types.Object {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure the provider before managing resources.")
		return types.ObjectNull(nil)
	}
	dc, env, org, err := r.client.ResourceRoutingScope()
	if err != nil {
		diags.AddError("Explicit resource organization required", err.Error())
		return types.ObjectNull(nil)
	}
	return types.ObjectValueMust(map[string]attr.Type{
		"data_center": types.StringType, "environment": types.StringType, "organization_id": types.StringType,
	}, map[string]attr.Value{
		"data_center": types.StringValue(dc), "environment": types.StringValue(env), "organization_id": types.StringValue(org),
	})
}

func (r *environmentScopedResource) check(ctx context.Context, state tfsdk.State, requireBound bool, diags *diag.Diagnostics) types.Object {
	expected := r.scope(diags)
	if diags.HasError() || state.Raw.IsNull() {
		return expected
	}
	var saved types.Object
	diags.Append(state.GetAttribute(ctx, path.Root("routing_scope"), &saved)...)
	if diags.HasError() {
		return expected
	}
	if saved.IsNull() {
		if requireBound {
			diags.AddError("Resource routing is not recorded", "Refresh this resource using its original data center, environment and explicit organization before planning an update or deletion. Do not change routing during legacy state migration.")
		}
	} else if !saved.Equal(expected) {
		diags.AddError("Resource routing changed", "The provider data center, environment or organization differs from this resource's saved routing_scope. Restore the original provider configuration. Changing routing does not migrate or promote resources; use separate provider aliases and states for separate environments.")
	}
	return expected
}

func bindResourceScope(ctx context.Context, state *tfsdk.State, scope types.Object, diags *diag.Diagnostics) {
	// Preserve binding even after a partial write returns an ID and an error.
	if !state.Raw.IsNull() {
		diags.Append(state.SetAttribute(ctx, path.Root("routing_scope"), scope)...)
	}
}

func checkPlannedScope(ctx context.Context, plan tfsdk.Plan, expected types.Object, diags *diag.Diagnostics) {
	var planned types.Object
	diags.Append(plan.GetAttribute(ctx, path.Root("routing_scope"), &planned)...)
	if !diags.HasError() && !planned.Equal(expected) {
		diags.AddError("Planned resource routing changed", "The saved plan does not match the current provider data center, environment and organization. Restore the original routing and generate a new reviewed plan before applying.")
	}
}

func (r *environmentScopedResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.client == nil { // Terraform validate may plan without configured clients.
		return
	}
	scope := r.check(ctx, req.State, true, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("routing_scope"), scope)...)
}

func (r *environmentScopedResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	scope := r.scope(&resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		checkPlannedScope(ctx, req.Plan, scope, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	r.inner.Create(ctx, req, resp)
	bindResourceScope(ctx, &resp.State, scope, &resp.Diagnostics)
}

func (r *environmentScopedResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	scope := r.check(ctx, req.State, false, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.inner.Read(ctx, req, resp)
	if !resp.Diagnostics.HasError() {
		bindResourceScope(ctx, &resp.State, scope, &resp.Diagnostics)
	}
}

func (r *environmentScopedResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	scope := r.check(ctx, req.State, true, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		checkPlannedScope(ctx, req.Plan, scope, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	r.inner.Update(ctx, req, resp)
	bindResourceScope(ctx, &resp.State, scope, &resp.Diagnostics)
}

func (r *environmentScopedResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.check(ctx, req.State, true, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.inner.Delete(ctx, req, resp)
}

func (r *environmentScopedResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	scope := r.scope(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.inner.ImportState(ctx, req, resp)
	if !resp.Diagnostics.HasError() {
		bindResourceScope(ctx, &resp.State, scope, &resp.Diagnostics)
	}
}
