variable "project_id" { type = string }
variable "table_id" { type = string }

data "catalyst_datastore_table" "site" {
  project_id = var.project_id
  id         = var.table_id
}

output "schema" { value = jsondecode(data.catalyst_datastore_table.site.schema_json) }
