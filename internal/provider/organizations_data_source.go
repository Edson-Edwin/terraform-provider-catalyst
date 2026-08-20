// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type OrganizationsDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &OrganizationsDataSource{}

type organizationsModel struct {
	Organizations []organizationModel `tfsdk:"organizations"`
}

type organizationModel struct {
	ID      string     `tfsdk:"id"`
	Name    string     `tfsdk:"name"`
	Default types.Bool `tfsdk:"is_default"`
}

func NewOrganizationsDataSource() datasource.DataSource { return &OrganizationsDataSource{} }

func (d *OrganizationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organizations"
}

func (d *OrganizationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List Catalyst organizations visible to the authenticated account in the configured data center, using the official CLI's organization lookup. Configure a provider without a specific organization for account-wide discovery. Requires OAuth access to the organization list. The minimum scope still needs live verification.",
		Attributes: map[string]schema.Attribute{
			"organizations": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Visible organization identities sorted lexicographically by ID.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Organization ID as a precision-preserving string."},
					"name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Organization name reported by Catalyst."},
					"is_default": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Catalyst reports the organization as the default, when available."},
				}}},
		},
	}
}

func (d *OrganizationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before listing organizations.")
		return
	}
	organizations, err := d.client.ListOrganizations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Catalyst organizations", err.Error())
		return
	}
	data := organizationsModel{Organizations: make([]organizationModel, 0, len(organizations))}
	for _, organization := range organizations {
		data.Organizations = append(data.Organizations, organizationModel{
			ID: string(organization.ID), Name: organization.Name, Default: types.BoolPointerValue(organization.Default),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
