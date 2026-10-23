variable "project_id" { type = string }

data "catalyst_nosql_tables" "existing" { project_id = var.project_id }

output "tables" { value = data.catalyst_nosql_tables.existing.tables }
