// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ProjectImportJobDataSource struct{ infrastructureDetailClient }

type projectImportJobModel struct {
	ID        types.String `tfsdk:"id"`
	Status    types.String `tfsdk:"status"`
	ProjectID types.String `tfsdk:"project_id"`
}

func NewProjectImportJobDataSource() datasource.DataSource { return &ProjectImportJobDataSource{} }

func (d *ProjectImportJobDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_import_job"
}

func (d *ProjectImportJobDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Read the current status of an existing Catalyst IaC project import job. Requires Development, an explicit organization, and `ZohoCatalyst.project.import.READ`. Use the job ID from your CLI or console import. The resulting project ID is available once the job is Completed. Terraform performs a single lookup. Verified offline against the official CLI contract.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Required: true, MarkdownDescription: "Existing IaC import job ID as a positive numeric string."},
			"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "Exact status reported by Catalyst, such as Scheduled, Processing, Completed or Error_Processing."},
			"project_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Resulting project ID when status is Completed. Null while the job is pending or failed."},
		},
	}
}

func (d *ProjectImportJobDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data projectImportJobModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	validateCatalystIDs(&resp.Diagnostics, map[string]types.String{"id": data.ID})
}

func (d *ProjectImportJobDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data projectImportJobModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Missing Catalyst client", "Configure provider credentials before reading project import status.")
		return
	}
	job, err := d.client.GetProjectImportJob(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Catalyst project import status", err.Error())
		return
	}
	data.Status = types.StringValue(job.Status)
	data.ProjectID = types.StringNull()
	if job.ProjectID != "" {
		data.ProjectID = types.StringValue(string(job.ProjectID))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
