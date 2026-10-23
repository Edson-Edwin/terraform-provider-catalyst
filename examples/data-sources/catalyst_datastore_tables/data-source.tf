data "catalyst_datastore_tables" "example" {
  project_id = var.project_id
}

output "tables" {
  value = data.catalyst_datastore_tables.example.tables
}
