// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"slices"
)

var _ resource.ResourceWithConfigure = &GatewayRulesetResource{}
var _ resource.ResourceWithImportState = &GatewayRulesetResource{}
var _ resource.ResourceWithValidateConfig = &GatewayRulesetResource{}

type GatewayRulesetResource struct{ client *catalyst.Client }
type gatewayRulesetModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Rules     types.Map    `tfsdk:"rules"`
}
type gatewayRuleModel struct {
	Source         types.String `tfsdk:"source_endpoint"`
	Endpoint       types.String `tfsdk:"target_endpoint"`
	TargetID       types.String `tfsdk:"function_id"`
	Method         types.String `tfsdk:"method"`
	Authentication types.Set    `tfsdk:"authentication"`
}

var gatewayRuleTypes = map[string]attr.Type{"source_endpoint": types.StringType, "target_endpoint": types.StringType, "function_id": types.StringType, "method": types.StringType, "authentication": types.SetType{ElemType: types.StringType}}

func NewGatewayRulesetResource() resource.Resource { return &GatewayRulesetResource{} }
func (r *GatewayRulesetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_gateway_ruleset"
}
func (r *GatewayRulesetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Own the complete API Gateway ruleset for an enabled project. Supports Basic I/O Function targets with OAuth authentication or explicitly unauthenticated routes, without throttling. Updating replaces the complete ruleset; omitted rules are deleted. Creation requires an empty ruleset; import existing supported rules using the project ID. Unsupported target/authentication/throttling/system policies fail safely instead of being discarded. Requires Gateway API read/create/update/delete scopes. Use project_id = catalyst_api_gateway.example.id to order enablement and destruction. Destroy removes all owned rules but does not disable Gateway. Reads of a disabled Gateway return an error and retain state. Do not share ruleset ownership across states, console users or other deployment tools: the bulk API has no established conditional-write primitive. Changes wait up to five minutes per migration, inspect asynchronous job failures and require consecutive matching reads. No routes or Functions are invoked. Live evidence is limited to India Development; client/Advanced I/O/system routes and configured throttling remain unsupported.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Project ID identifying the single ruleset.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Enabled Gateway's project ID; reference catalyst_api_gateway.id to establish lifecycle ordering.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"rules": schema.MapNestedAttribute{Required: true, MarkdownDescription: "Complete ruleset keyed by unique rule name. Use an empty map to remove all rules. Names and source/method combinations must be unique.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"source_endpoint": schema.StringAttribute{Required: true, MarkdownDescription: "Custom request path beginning with /, without query or fragment."},
				"target_endpoint": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Basic I/O target path /server/function_name/execute."},
				"function_id":     schema.StringAttribute{Required: true, MarkdownDescription: "Existing Basic I/O Function's positive numeric ID."},
				"method":          schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("GET"), MarkdownDescription: "GET (default), POST, PUT, DELETE, PATCH or ANY. ANY cannot overlap other methods for the same source."},
				"authentication":  schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, Default: setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{types.StringValue("OAuth")})), MarkdownDescription: "Defaults to [\"OAuth\"]. An explicit empty set makes the route unauthenticated. Other authentication modes are not yet supported."},
			}}},
		}}
}
func (r *GatewayRulesetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *GatewayRulesetResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing rules.")
		return false
	}
	return true
}
func (d *gatewayRulesetModel) api(ctx context.Context) ([]catalyst.GatewayRule, diag.Diagnostics) {
	var diags diag.Diagnostics
	var input map[string]gatewayRuleModel
	diags.Append(d.Rules.ElementsAs(ctx, &input, false)...)
	result := make([]catalyst.GatewayRule, 0, len(input))
	if diags.HasError() {
		return nil, diags
	}
	for name, v := range input {
		auth := []string{}
		diags.Append(v.Authentication.ElementsAs(ctx, &auth, false)...)
		slices.Sort(auth)
		result = append(result, catalyst.GatewayRule{Name: name, Source: v.Source.ValueString(), Endpoint: v.Endpoint.ValueString(), TargetID: catalyst.ID(v.TargetID.ValueString()), Target: "basicio", Method: v.Method.ValueString(), Authentication: auth})
	}
	return result, diags
}
func (r *GatewayRulesetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data gatewayRulesetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.ProjectID.IsUnknown() && !data.ProjectID.IsNull() && !catalyst.ValidID(data.ProjectID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("project_id"), "Invalid project ID", "Use a positive numeric string.")
	}
	if data.Rules.IsNull() || data.Rules.IsUnknown() {
		return
	}
	if len(data.Rules.Elements()) > 1000 {
		resp.Diagnostics.AddAttributeError(path.Root("rules"), "Too many rules", "Gateway supports at most 1000 rules.")
	}
	var input map[string]gatewayRuleModel
	resp.Diagnostics.Append(data.Rules.ElementsAs(ctx, &input, true)...)
	for name, v := range input {
		if v.Source.IsUnknown() || v.Endpoint.IsUnknown() || v.TargetID.IsUnknown() || v.Method.IsNull() || v.Method.IsUnknown() || v.Authentication.IsNull() || v.Authentication.IsUnknown() {
			continue
		}
		auth := []string{}
		resp.Diagnostics.Append(v.Authentication.ElementsAs(ctx, &auth, true)...)
		rule := catalyst.GatewayRule{Name: name, Source: v.Source.ValueString(), Endpoint: v.Endpoint.ValueString(), TargetID: catalyst.ID(v.TargetID.ValueString()), Target: "basicio", Method: v.Method.ValueString(), Authentication: auth}
		if !catalyst.SupportedGatewayRule(rule) {
			resp.Diagnostics.AddAttributeError(path.Root("rules").AtMapKey(name), "Unsupported Gateway rule", "Use a Basic I/O Function target, documented method, valid paths and OAuth authentication or an explicit empty set.")
		}
	}
}
func (d *gatewayRulesetModel) populate(ctx context.Context, rules []catalyst.GatewayRule) diag.Diagnostics {
	var diags diag.Diagnostics
	values := map[string]gatewayRuleModel{}
	for _, rule := range rules {
		if !catalyst.SupportedGatewayRule(rule) {
			diags.AddError("Unsupported Gateway policy", "The remote ruleset contains an unsupported target, authentication, throttling or system policy. State is retained; no policy was discarded.")
			return diags
		}
		auth, dg := types.SetValueFrom(ctx, types.StringType, rule.Authentication)
		diags.Append(dg...)
		values[rule.Name] = gatewayRuleModel{Source: types.StringValue(rule.Source), Endpoint: types.StringValue(rule.Endpoint), TargetID: types.StringValue(string(rule.TargetID)), Method: types.StringValue(rule.Method), Authentication: auth}
	}
	value, dg := types.MapValueFrom(ctx, types.ObjectType{AttrTypes: gatewayRuleTypes}, values)
	diags.Append(dg...)
	d.Rules = value
	d.ID = types.StringValue(d.ProjectID.ValueString())
	return diags
}
func (r *GatewayRulesetResource) persist(ctx context.Context, data *gatewayRulesetModel, creating bool, diags *diag.Diagnostics, setState func()) {
	desired, dg := data.api(ctx)
	diags.Append(dg...)
	if diags.HasError() {
		return
	}
	err := r.client.SubmitGatewayRules(ctx, data.ProjectID.ValueString(), desired, creating, func() error {
		data.ID = types.StringValue(data.ProjectID.ValueString())
		setState()
		if diags.HasError() {
			return errors.New("unable to preserve ruleset identity before mutation")
		}
		return nil
	})
	if err != nil {
		diags.AddError("Unable to configure Gateway rules", err.Error())
		return
	}
	setState()
}
func (r *GatewayRulesetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data gatewayRulesetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, true, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *GatewayRulesetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data gatewayRulesetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	r.persist(ctx, &data, false, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &data)...) })
}
func (r *GatewayRulesetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data gatewayRulesetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	state, err := r.client.WaitGatewayIdle(ctx, data.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Gateway status", err.Error())
		return
	}
	if !*state.Enabled {
		resp.Diagnostics.AddError("Gateway is disabled", "Enable Gateway before refreshing, importing or deleting its ruleset. The unavailable rules endpoint does not prove that rules are absent.")
		return
	}
	rules, err := r.client.ListGatewayRules(ctx, data.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Gateway rules", err.Error())
		return
	}
	resp.Diagnostics.Append(data.populate(ctx, rules)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *GatewayRulesetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data gatewayRulesetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	if err := r.client.SubmitGatewayRules(ctx, data.ProjectID.ValueString(), []catalyst.GatewayRule{}, false, nil); err != nil {
		resp.Diagnostics.AddError("Unable to remove Gateway rules", err.Error())
	}
}
func (r *GatewayRulesetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !catalyst.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid import identifier", "Use the positive numeric project ID of an enabled Gateway.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), req.ID)...)
}
