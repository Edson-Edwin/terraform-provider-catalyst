// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = &CatalystProvider{}

// CatalystProvider is the Terraform provider for Zoho Catalyst.
type CatalystProvider struct {
	version string
}

func (p *CatalystProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "catalyst"
	resp.Version = p.version
}

func (p *CatalystProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Catalyst provider is under development. This initial skeleton has no configuration attributes, resources, or data sources and does not connect to Zoho APIs.",
	}
}

func (p *CatalystProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
	// OAuth configuration and the shared Catalyst API client will be added next.
}

func (p *CatalystProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func (p *CatalystProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}

// New constructs a provider factory for the plugin server and tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &CatalystProvider{version: version}
	}
}
