// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type infrastructureDetailClient struct{ client *catalyst.Client }

func (d *infrastructureDetailClient) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*catalyst.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", "The provider supplied an incompatible Catalyst client.")
		return
	}
	d.client = client
}

type JobPoolDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &JobPoolDataSource{}
var _ datasource.DataSourceWithValidateConfig = &JobPoolDataSource{}

type jobPoolDetailsModel struct {
	ProjectID        types.String `tfsdk:"project_id"`
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	CapacityMemoryMB types.Int64  `tfsdk:"capacity_memory_mb"`
	CapacityCount    types.Int64  `tfsdk:"capacity_count"`
}

func NewJobPoolDataSource() datasource.DataSource { return &JobPoolDataSource{} }

func (d *JobPoolDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_job_pool"
}

func (d *JobPoolDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read an existing Job Scheduling pool by ID. Requires `ZohoCatalyst.jobpool.READ`. Capacity fields are null when Catalyst omits them. This read uses the official lookup contract and has offline verification.",
		Attributes: map[string]schema.Attribute{
			"project_id":         schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"id":                 schema.StringAttribute{Required: true, MarkdownDescription: "Existing job pool's positive numeric ID as a string."},
			"name":               schema.StringAttribute{Computed: true, MarkdownDescription: "Current job pool name."},
			"type":               schema.StringAttribute{Computed: true, MarkdownDescription: "Job pool target type reported by Catalyst."},
			"capacity_memory_mb": schema.Int64Attribute{Computed: true, MarkdownDescription: "Configured pool memory in MB when available."},
			"capacity_count":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Configured target count when available."},
		},
	}
}

func (d *JobPoolDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data jobPoolDetailsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"project_id": data.ProjectID, "id": data.ID})
}

func (d *JobPoolDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data jobPoolDetailsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading a job pool.")
		return
	}
	pool, err := d.client.GetJobPool(ctx, data.ProjectID.ValueString(), data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst job pool", err.Error())
		return
	}
	data.Name = types.StringValue(pool.Name)
	data.Type = types.StringValue(pool.Type)
	data.CapacityMemoryMB = types.Int64PointerValue(pool.MemoryMB)
	data.CapacityCount = types.Int64PointerValue(pool.Count)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

type StratusCORSDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &StratusCORSDataSource{}
var _ datasource.DataSourceWithValidateConfig = &StratusCORSDataSource{}

type stratusCORSModel struct {
	ProjectID  types.String           `tfsdk:"project_id"`
	BucketName types.String           `tfsdk:"bucket_name"`
	Rules      []stratusCORSRuleModel `tfsdk:"rules"`
}

type stratusCORSRuleModel struct {
	Domain         string   `tfsdk:"domain"`
	AllowedMethods []string `tfsdk:"allowed_methods"`
}

func NewStratusCORSDataSource() datasource.DataSource { return &StratusCORSDataSource{} }

func (d *StratusCORSDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stratus_bucket_cors"
}

func (d *StratusCORSDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read the cross-origin resource sharing (CORS) rules for an existing Stratus bucket using the official JavaScript SDK read contract. Requires project access and permission to read bucket CORS. This data source has offline verification.",
		Attributes: map[string]schema.Attribute{
			"project_id":  schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"bucket_name": schema.StringAttribute{Required: true, MarkdownDescription: "Existing lowercase Stratus bucket name."},
			"rules": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Rules sorted by domain and method list. A successful response without rules returns an empty list.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"domain":          schema.StringAttribute{Computed: true, MarkdownDescription: "Origin reported by Catalyst."},
					"allowed_methods": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Allowed HTTP methods sorted lexicographically."},
				}}},
		},
	}
}

func (d *StratusCORSDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data stratusCORSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateStratusLookup(&resp.Diagnostics, data.ProjectID, data.BucketName)
}

func (d *StratusCORSDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data stratusCORSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading Stratus CORS rules.")
		return
	}
	rules, err := d.client.ListStratusCORS(ctx, data.ProjectID.ValueString(), data.BucketName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Stratus bucket CORS", err.Error())
		return
	}
	data.Rules = make([]stratusCORSRuleModel, 0, len(rules))
	for _, rule := range rules {
		data.Rules = append(data.Rules, stratusCORSRuleModel{Domain: rule.Domain, AllowedMethods: rule.AllowedMethods})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

type StratusBucketDataSource struct{ infrastructureDetailClient }

var _ datasource.DataSourceWithConfigure = &StratusBucketDataSource{}
var _ datasource.DataSourceWithValidateConfig = &StratusBucketDataSource{}

type stratusBucketModel struct {
	ProjectID         types.String `tfsdk:"project_id"`
	BucketName        types.String `tfsdk:"bucket_name"`
	URL               types.String `tfsdk:"url"`
	VersioningEnabled types.Bool   `tfsdk:"versioning_enabled"`
}

func NewStratusBucketDataSource() datasource.DataSource { return &StratusBucketDataSource{} }

func (d *StratusBucketDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stratus_bucket"
}

func (d *StratusBucketDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read the regional URL and versioning status of an existing Stratus bucket. Requires `ZohoCatalyst.buckets.READ` and project access. Both versioned and unversioned buckets can be inspected.",
		Attributes: map[string]schema.Attribute{
			"project_id":         schema.StringAttribute{Required: true, MarkdownDescription: "Existing Catalyst project's positive numeric ID as a string."},
			"bucket_name":        schema.StringAttribute{Required: true, MarkdownDescription: "Existing lowercase Stratus bucket name."},
			"url":                schema.StringAttribute{Computed: true, MarkdownDescription: "Verified regional HTTPS bucket endpoint."},
			"versioning_enabled": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether object versioning is enabled on the bucket."},
		},
	}
}

func (d *StratusBucketDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data stratusBucketModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateStratusLookup(&resp.Diagnostics, data.ProjectID, data.BucketName)
}

func (d *StratusBucketDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data stratusBucketModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading Stratus bucket metadata.")
		return
	}
	bucket, err := d.client.GetStratusBucketDetails(ctx, data.ProjectID.ValueString(), data.BucketName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst Stratus bucket", err.Error())
		return
	}
	data.URL = types.StringValue(bucket.URL)
	data.VersioningEnabled = types.BoolPointerValue(bucket.Meta.Versioning)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func validateStratusLookup(diags *diag.Diagnostics, projectID, bucketName types.String) {
	validateCatalystIDs(diags, map[string]types.String{"project_id": projectID})
	if !bucketName.IsUnknown() && !bucketName.IsNull() && !catalyst.ValidStratusBucketName(bucketName.ValueString()) {
		diags.AddAttributeError(path.Root("bucket_name"), "Invalid Stratus bucket name", "Specify an existing lowercase bucket name using letters, numbers and hyphens.")
	}
}
