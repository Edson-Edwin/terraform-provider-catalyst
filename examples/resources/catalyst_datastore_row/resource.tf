variable "project_id" { type = string }
variable "table_id" { type = string }

# Existing table with a nullable text column named message is required.
# Own configuration/seed data only, not changing customer transactions.
resource "catalyst_datastore_row" "example" {
  project_id = var.project_id
  table_id   = var.table_id
  values_json = jsonencode({
    message = "Website configuration managed by Terraform"
  })
}
