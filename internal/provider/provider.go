// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/http"
	"os"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &CatalystProvider{}

// CatalystProvider is the Terraform provider for Zoho Catalyst.
type CatalystProvider struct {
	version    string
	httpClient *http.Client
}

type providerModel struct {
	DataCenter     types.String `tfsdk:"data_center"`
	Environment    types.String `tfsdk:"environment"`
	OrganizationID types.String `tfsdk:"organization_id"`
	AccessToken    types.String `tfsdk:"access_token"`
	ClientID       types.String `tfsdk:"client_id"`
	ClientSecret   types.String `tfsdk:"client_secret"`
	RefreshToken   types.String `tfsdk:"refresh_token"`
	RedirectURI    types.String `tfsdk:"redirect_uri"`
}

func (p *CatalystProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "catalyst"
	resp.Version = p.version
}

func (p *CatalystProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Discover Zoho Catalyst projects, job pools, Data Store tables, File Store folders, Cache segments, Stratus buckets and Functions, and manage webhook crons, File Store files, Node.js Functions and AppSail services. Authenticate using either an access token or OAuth refresh credentials. Environment variables are recommended for secrets.",
		Attributes: map[string]schema.Attribute{
			"data_center":     schema.StringAttribute{Optional: true, MarkdownDescription: "Data center: `us`, `eu`, `in`, `au`, `ca`, `jp`, or `sa`. Falls back to `CATALYST_DATA_CENTER`, then `us`."},
			"environment":     schema.StringAttribute{Optional: true, MarkdownDescription: "`Development` or `Production`. Falls back to `CATALYST_ENVIRONMENT`, then `Development`."},
			"organization_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Optional organization ID for the `CATALYST-ORG` header. Falls back to `CATALYST_ORGANIZATION_ID`."},
			"access_token":    schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Existing OAuth access token. Falls back to `CATALYST_ACCESS_TOKEN`. Cannot be combined with refresh credentials. Does not refresh automatically."},
			"client_id":       schema.StringAttribute{Optional: true, MarkdownDescription: "OAuth client ID. Falls back to `CATALYST_CLIENT_ID`. Requires client_secret and refresh_token."},
			"client_secret":   schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "OAuth client secret. Falls back to `CATALYST_CLIENT_SECRET`."},
			"refresh_token":   schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "OAuth refresh token. Falls back to `CATALYST_REFRESH_TOKEN`. Access tokens are refreshed automatically and cached in memory."},
			"redirect_uri":    schema.StringAttribute{Optional: true, MarkdownDescription: "Optional registered redirect URI for OAuth refresh requests. Falls back to `CATALYST_REDIRECT_URI`. Omit for a Self Client."},
		},
	}
}

func (p *CatalystProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config := catalyst.Config{}
	fields := []struct {
		name, env string
		value     types.String
		target    *string
	}{
		{"data_center", "CATALYST_DATA_CENTER", data.DataCenter, &config.DataCenter},
		{"environment", "CATALYST_ENVIRONMENT", data.Environment, &config.Environment},
		{"organization_id", "CATALYST_ORGANIZATION_ID", data.OrganizationID, &config.OrganizationID},
		{"access_token", "CATALYST_ACCESS_TOKEN", data.AccessToken, &config.AccessToken},
		{"client_id", "CATALYST_CLIENT_ID", data.ClientID, &config.ClientID},
		{"client_secret", "CATALYST_CLIENT_SECRET", data.ClientSecret, &config.ClientSecret},
		{"refresh_token", "CATALYST_REFRESH_TOKEN", data.RefreshToken, &config.RefreshToken},
		{"redirect_uri", "CATALYST_REDIRECT_URI", data.RedirectURI, &config.RedirectURI},
	}
	for _, field := range fields {
		if field.value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(field.name), "Unknown provider configuration", "This value must be known before Catalyst can be configured. Use an input variable or environment variable instead of an apply-time resource value.")
			continue
		}
		if field.value.IsNull() {
			*field.target = os.Getenv(field.env)
		} else {
			*field.target = field.value.ValueString()
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	client, err := catalyst.New(config, p.httpClient)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Catalyst configuration", err.Error())
		return
	}
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CatalystProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewProjectDataSource, NewProjectsDataSource, NewJobPoolsDataSource, NewTablesDataSource, NewFoldersDataSource, NewCacheSegmentsDataSource, NewBucketsDataSource, NewFunctionsDataSource}
}

func (p *CatalystProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewWebhookCronResource, NewFilestoreFileResource, NewFunctionResource, NewAppSailResource}
}

// New constructs a provider factory for the plugin server and tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &CatalystProvider{version: version} }
}
