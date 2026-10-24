// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/http"
	"os"
	"strings"

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
	DataCenter               types.String `tfsdk:"data_center"`
	Environment              types.String `tfsdk:"environment"`
	OrganizationID           types.String `tfsdk:"organization_id"`
	AccessToken              types.String `tfsdk:"access_token"`
	ClientID                 types.String `tfsdk:"client_id"`
	ClientSecret             types.String `tfsdk:"client_secret"`
	RefreshToken             types.String `tfsdk:"refresh_token"`
	RedirectURI              types.String `tfsdk:"redirect_uri"`
	AllowedOrganizationIDs   types.Set    `tfsdk:"allowed_organization_ids"`
	ForbiddenOrganizationIDs types.Set    `tfsdk:"forbidden_organization_ids"`
	ValidationProjectID      types.String `tfsdk:"validation_project_id"`
	AllowedDataCenters       types.Set    `tfsdk:"allowed_data_centers"`
	AllowedEnvironments      types.Set    `tfsdk:"allowed_environments"`
}

func (p *CatalystProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "catalyst"
	resp.Version = p.version
}

func (p *CatalystProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Discover Zoho Catalyst projects, job pools, database schemas, File Store folders, Cache segments, Stratus buckets, Functions and application user access metadata. Manage Data Store configuration rows, NoSQL configuration items, Stratus objects/metadata with optional multipart transfers, managed Node/Java SE/Python AppSail services, Node.js Functions, expanded Gateway rulesets and Webhook/Function/AppSail schedules. Existing end-user role assignment is experimental and not live-verified. Authentication setup, role definitions, permissions and Connections provisioning are not implemented. Project management is experimental and depends on account API eligibility; table/schema and bucket provisioning remain manual prerequisites. Authenticate using an access token or OAuth refresh credentials. Environment variables are recommended for secrets.",
		Attributes: map[string]schema.Attribute{
			"data_center":                schema.StringAttribute{Optional: true, MarkdownDescription: "Data center: `us`, `eu`, `in`, `au`, `ca`, `jp`, or `sa`. Falls back to `CATALYST_DATA_CENTER`, then `us` in Development. Production requires an explicit value."},
			"environment":                schema.StringAttribute{Optional: true, MarkdownDescription: "`Development` or `Production`. Falls back to `CATALYST_ENVIRONMENT`, then `Development`."},
			"organization_id":            schema.StringAttribute{Optional: true, MarkdownDescription: "Organization ID for the `CATALYST-ORG` header. Required when managing resources; optional for data sources. Falls back to `CATALYST_ORGANIZATION_ID`."},
			"access_token":               schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Existing OAuth access token. Falls back to `CATALYST_ACCESS_TOKEN`. Cannot be combined with refresh credentials. Does not refresh automatically."},
			"client_id":                  schema.StringAttribute{Optional: true, MarkdownDescription: "OAuth client ID. Falls back to `CATALYST_CLIENT_ID`. Requires client_secret and refresh_token."},
			"client_secret":              schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "OAuth client secret. Falls back to `CATALYST_CLIENT_SECRET`."},
			"refresh_token":              schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "OAuth refresh token. Falls back to `CATALYST_REFRESH_TOKEN`. Access tokens are refreshed automatically and cached in memory."},
			"redirect_uri":               schema.StringAttribute{Optional: true, MarkdownDescription: "Optional registered redirect URI for OAuth refresh requests. Falls back to `CATALYST_REDIRECT_URI`. Omit for a Self Client."},
			"allowed_organization_ids":   schema.SetAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Allowed configured organization IDs. Requires explicit organization_id and conflicts with forbidden_organization_ids when both are nonempty. Falls back to comma-separated CATALYST_ALLOWED_ORGANIZATION_IDS. This checks routing configuration, not the authenticated caller's identity."},
			"forbidden_organization_ids": schema.SetAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Forbidden configured organization IDs. Requires explicit organization_id. Falls back to comma-separated CATALYST_FORBIDDEN_ORGANIZATION_IDS. An explicit empty set suppresses the environment fallback."},
			"validation_project_id":      schema.StringAttribute{Optional: true, MarkdownDescription: "Optional existing project to read during provider configuration. Checks credentials and project access in the selected region, environment and organization before exposing the client to resources. Requires project-read scope. Falls back to CATALYST_VALIDATION_PROJECT_ID. This is an access check, not caller-identity verification."},
			"allowed_data_centers":       schema.SetAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Optional data center allowlist. Requires an explicit data_center and rejects destinations outside the set before authentication or API access. Falls back to comma-separated CATALYST_ALLOWED_DATA_CENTERS. An explicit empty set disables the fallback. This is a local routing guardrail, not an authorization boundary."},
			"allowed_environments":       schema.SetAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Optional environment allowlist containing Development or Production. Requires an explicit environment. Falls back to comma-separated CATALYST_ALLOWED_ENVIRONMENTS. An explicit empty set disables the fallback."},
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
	var validationProjectID string
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
		{"validation_project_id", "CATALYST_VALIDATION_PROJECT_ID", data.ValidationProjectID, &validationProjectID},
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
	for _, field := range []struct {
		name, env string
		value     types.Set
		target    *[]string
	}{
		{"allowed_organization_ids", "CATALYST_ALLOWED_ORGANIZATION_IDS", data.AllowedOrganizationIDs, &config.AllowedOrganizationIDs},
		{"forbidden_organization_ids", "CATALYST_FORBIDDEN_ORGANIZATION_IDS", data.ForbiddenOrganizationIDs, &config.ForbiddenOrganizationIDs},
		{"allowed_data_centers", "CATALYST_ALLOWED_DATA_CENTERS", data.AllowedDataCenters, &config.AllowedDataCenters},
		{"allowed_environments", "CATALYST_ALLOWED_ENVIRONMENTS", data.AllowedEnvironments, &config.AllowedEnvironments},
	} {
		if field.value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(field.name), "Unknown provider configuration", "Configuration policy sets must be known before Catalyst can be configured.")
		} else if field.value.IsNull() {
			if value := os.Getenv(field.env); value != "" {
				*field.target = strings.Split(value, ",")
				for i, id := range *field.target {
					(*field.target)[i] = strings.TrimSpace(id)
				}
			}
		} else {
			for _, id := range field.value.Elements() {
				if id.IsUnknown() || id.IsNull() {
					resp.Diagnostics.AddAttributeError(path.Root(field.name), "Invalid configuration policy", "Every policy value must be a known, non-null string.")
				}
			}
			if !resp.Diagnostics.HasError() {
				resp.Diagnostics.Append(field.value.ElementsAs(ctx, field.target, false)...)
			}
		}
	}
	if validationProjectID != "" && !catalyst.ValidID(validationProjectID) {
		resp.Diagnostics.AddAttributeError(path.Root("validation_project_id"), "Invalid validation project", "validation_project_id must be a positive integer written as a string.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	client, err := catalyst.New(config, p.httpClient)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Catalyst configuration", err.Error())
		return
	}
	if validationProjectID != "" {
		if _, err := client.GetProject(ctx, validationProjectID); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("validation_project_id"), "Catalyst access validation failed", err.Error())
			return
		}
	}
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CatalystProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewProjectDataSource, NewProjectsDataSource, NewJobPoolsDataSource, NewTablesDataSource, NewFoldersDataSource, NewCacheSegmentsDataSource, NewBucketsDataSource, NewFunctionsDataSource, NewNoSQLTablesDataSource, NewNoSQLTableDataSource, NewDataStoreTableDataSource, NewAuthUserDataSource}
}

func (p *CatalystProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewWebhookCronResource, NewFilestoreFileResource, NewFunctionResource, NewAppSailResource, NewGatewayResource, NewGatewayRulesetResource, NewStratusObjectResource, NewFunctionCronResource, NewAppSailCronResource, NewProjectResource, NewNoSQLItemResource, NewAuthUserRoleResource, NewDataStoreRowResource}
}

// New constructs a provider factory for the plugin server and tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &CatalystProvider{version: version} }
}
