variable "project_id" { type = string }
variable "datastore_table_id" { type = string }
variable "nosql_table_id" {
  type        = string
  description = "Existing ONLINE NoSQL table with string keys pk and sk."
}
variable "bucket_name" {
  type        = string
  description = "Existing private, authenticated, unversioned Stratus bucket."
}
variable "application_zip" {
  type        = string
  description = "Absolute path to the prebuilt website ZIP. Build dependencies before Terraform apply."
}
variable "name" {
  type    = string
  default = "fullstackwebsite"
}
variable "stack" {
  type    = string
  default = "node24"
}
variable "startup_command" {
  type    = string
  default = "node index.js"
}
variable "catalyst_auth" {
  type        = bool
  default     = true
  description = "Keep Catalyst login enabled, or explicitly set false for a public app after reviewing application authentication."
}
variable "application_environment" {
  type      = map(string)
  default   = {}
  sensitive = true
}
variable "datastore_config_values_json" {
  type        = string
  default     = null
  sensitive   = true
  description = "Optional scalar configuration row for existing application columns. Null skips row provisioning. Values remain in Terraform state."
}

data "catalyst_project" "existing" { id = var.project_id }
data "catalyst_datastore_table" "records" {
  project_id = data.catalyst_project.existing.id
  id         = var.datastore_table_id
}
data "catalyst_nosql_table" "configuration" {
  project_id = data.catalyst_project.existing.id
  id         = var.nosql_table_id
}
resource "catalyst_datastore_row" "site" {
  count       = nonsensitive(var.datastore_config_values_json == null) ? 0 : 1
  project_id  = data.catalyst_project.existing.id
  table_id    = data.catalyst_datastore_table.records.id
  values_json = var.datastore_config_values_json
}

resource "catalyst_nosql_item" "site" {
  project_id = data.catalyst_project.existing.id
  table_id   = data.catalyst_nosql_table.configuration.id
  keys_json  = jsonencode({ pk = { S = "terraform" }, sk = { S = "site" } })
  item_json = jsonencode({
    pk            = { S = "terraform" }
    sk            = { S = "site" }
    title         = { S = var.name }
    assets_bucket = { S = var.bucket_name }
  })
  lifecycle {
    precondition {
      condition = (
        try(jsondecode(data.catalyst_nosql_table.configuration.schema_json).partition_key.column_name == "pk", false) &&
        try(jsondecode(data.catalyst_nosql_table.configuration.schema_json).partition_key.data_type == "S", false) &&
        try(jsondecode(data.catalyst_nosql_table.configuration.schema_json).sort_key.column_name == "sk", false) &&
        try(jsondecode(data.catalyst_nosql_table.configuration.schema_json).sort_key.data_type == "S", false)
      )
      error_message = "This example requires string partition key pk and string sort key sk."
    }
  }
}

resource "catalyst_stratus_object" "asset" {
  project_id    = data.catalyst_project.existing.id
  bucket_name   = var.bucket_name
  key           = "website/site.json"
  source        = "${path.module}/assets/site.json"
  source_sha256 = filesha256("${path.module}/assets/site.json")
  content_type  = "application/json"
  metadata      = { managed_by = "terraform", purpose = "website" }
}

resource "catalyst_appsail" "website" {
  project_id      = data.catalyst_project.existing.id
  name            = var.name
  stack           = var.stack
  platform        = startswith(var.stack, "java") ? "javase" : ""
  source          = var.application_zip
  source_sha256   = filesha256(var.application_zip)
  startup_command = var.startup_command
  port            = 9000
  memory          = 256
  catalyst_auth   = var.catalyst_auth
  environment_variables = merge(var.application_environment, {
    DATASTORE_TABLE_ID      = data.catalyst_datastore_table.records.id
    DATASTORE_CONFIG_ROW_ID = try(catalyst_datastore_row.site[0].row_id, "")
    NOSQL_TABLE_ID          = data.catalyst_nosql_table.configuration.id
    STRATUS_BUCKET          = catalyst_stratus_object.asset.bucket_name
    SITE_CONFIG_PK          = "terraform"
    SITE_CONFIG_SK          = "site"
  })
  depends_on = [catalyst_nosql_item.site]
}

output "website_url" { value = catalyst_appsail.website.url }
output "datastore_schema" { value = jsondecode(data.catalyst_datastore_table.records.schema_json) }
