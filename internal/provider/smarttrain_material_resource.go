// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"net/url"
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

type SmartTrainMaterialResource struct{ client *catalyst.Client }

type smartTrainResourceModel struct {
	RoutingScope types.Object `tfsdk:"routing_scope"`
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	BotID        types.String `tfsdk:"bot_id"`
	URL          types.String `tfsdk:"url"`
	Title        types.String `tfsdk:"title"`
	Status       types.String `tfsdk:"status"`
}

func NewSmartTrainMaterialResource() resource.Resource {
	return withEnvironmentScope(&SmartTrainMaterialResource{})
}

func (r *SmartTrainMaterialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_smarttrain_material"
}

func (r *SmartTrainMaterialResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client. Please report this provider bug.")
		return
	}
	r.client = client
}

func (r *SmartTrainMaterialResource) ready(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Missing Catalyst client", "Configure provider credentials before managing SmartTrain materials.")
		return false
	}
	return true
}

func (r *SmartTrainMaterialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Manage a URL-based training material for an existing ConvoKraft SmartTrain bot in Development. Requires an explicit organization and `ZIAPlatform.Convokraft.RAG.CREATE`, `.READ`, and `.DELETE`. Use a public HTTPS URL for material you intend the bot to learn. Create waits for the material record to be visible; training continues asynchronously. A URL or title change replaces the material, and deleting a record removes its trained knowledge. Import as `project_id/bot_id/material_id`, then configure the canonical URL reported by Catalyst. Verified offline.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Material ID as an exact numeric string.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Development project ID.", PlanModifiers: replace},
			"bot_id":     schema.StringAttribute{Required: true, MarkdownDescription: "Existing Development bot ID with SmartTrain enabled.", PlanModifiers: replace},
			"url":        schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Public HTTPS URL of the HTML or text training material. Stored in Terraform state.", PlanModifiers: replace},
			"title":      schema.StringAttribute{Required: true, MarkdownDescription: "Title of the training material.", PlanModifiers: replace},
			"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "Latest training status reported by Catalyst, when available."},
		},
	}
}

func validSmartTrainURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func (r *SmartTrainMaterialResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data smartTrainResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "bot_id": data.BotID})
	if !data.URL.IsNull() && !data.URL.IsUnknown() && !validSmartTrainURL(data.URL.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("url"), "Invalid SmartTrain URL", "Specify an absolute HTTPS URL without embedded credentials or a fragment.")
	}
	if !data.Title.IsNull() && !data.Title.IsUnknown() && strings.TrimSpace(data.Title.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("title"), "Empty material title", "Specify a non-empty title.")
	}
}

func validURLMaterial(item *catalyst.SmartTrainMaterial) bool {
	return item.Source != nil && *item.Source == "URL" && validSmartTrainURL(item.URL) && item.Title != nil && strings.TrimSpace(*item.Title) != ""
}

func (d *smartTrainResourceModel) populate(item *catalyst.SmartTrainMaterial) {
	d.ID = types.StringValue(string(item.ID))
	if d.URL.IsUnknown() || d.URL.IsNull() || !catalyst.SmartTrainURLMatches(d.URL.ValueString(), item.URL) {
		d.URL = types.StringValue(item.URL)
	}
	d.Title = types.StringPointerValue(item.Title)
	d.Status = types.StringPointerValue(item.Status)
}

func (r *SmartTrainMaterialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data smartTrainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	project, bot, source := data.ProjectID.ValueString(), data.BotID.ValueString(), data.URL.ValueString()
	item, err := r.client.CreateSmartTrainMaterial(ctx, project, bot, source, data.Title.ValueString())
	if item != nil {
		data.ID, data.Status = types.StringValue(string(item.ID)), types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to create SmartTrain material", err.Error())
		return
	}
	if !validURLMaterial(item) || !catalyst.SmartTrainURLMatches(source, item.URL) || *item.Title != data.Title.ValueString() {
		resp.Diagnostics.AddError("SmartTrain material readback differs", "Review the created record before retrying. Terraform retained the returned ID for recovery.")
		return
	}
	data.populate(item)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SmartTrainMaterialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data smartTrainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	item, err := r.client.GetSmartTrainMaterial(ctx, data.ProjectID.ValueString(), data.BotID.ValueString(), data.ID.ValueString())
	if errors.Is(err, catalyst.ErrSmartTrainMaterialNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read SmartTrain material", err.Error())
		return
	}
	if !validURLMaterial(item) {
		resp.Diagnostics.AddError("Unsupported SmartTrain material", "The resource requires a URL material with a valid HTTPS URL and title.")
		return
	}
	data.populate(item)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SmartTrainMaterialResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("SmartTrain replacement required", "Changing material inputs requires replacement of the material record.")
}

func (r *SmartTrainMaterialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data smartTrainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	project, bot, id := data.ProjectID.ValueString(), data.BotID.ValueString(), data.ID.ValueString()
	item, err := r.client.GetSmartTrainMaterial(ctx, project, bot, id)
	if errors.Is(err, catalyst.ErrSmartTrainMaterialNotFound) {
		return
	}
	if err == nil && (!validURLMaterial(item) || !catalyst.SmartTrainURLMatches(data.URL.ValueString(), item.URL)) {
		resp.Diagnostics.AddError("Unable to verify SmartTrain material ownership", "Catalyst returned a material with a different URL or source type. Refresh and review it before deletion.")
		return
	}
	if err == nil {
		err = r.client.DeleteSmartTrainMaterial(ctx, project, bot, id)
	}
	if err == nil {
		_, err = r.client.WaitSmartTrainMaterial(ctx, project, bot, id, false)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm SmartTrain material deletion", err.Error())
	}
}

func (r *SmartTrainMaterialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids := strings.Split(req.ID, "/")
	if len(ids) != 3 || !catalyst.ValidID(ids[0]) || !catalyst.ValidID(ids[1]) || !catalyst.ValidID(ids[2]) {
		resp.Diagnostics.AddError("Invalid SmartTrain import identifier", "Use project_id/bot_id/material_id with three positive numeric IDs.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("bot_id"), ids[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[2])...)
}
