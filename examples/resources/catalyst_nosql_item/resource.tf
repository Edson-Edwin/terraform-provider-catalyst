variable "project_id" { type = string }
variable "table_id" {
  type        = string
  description = "Existing ONLINE table with string partition key pk and string sort key sk."
}

# Configuration owned by Terraform, separate from application-owned records.
resource "catalyst_nosql_item" "site" {
  project_id = var.project_id
  table_id   = var.table_id
  keys_json  = jsonencode({ pk = { S = "terraform" }, sk = { S = "site" } })
  item_json = jsonencode({
    pk      = { S = "terraform" }
    sk      = { S = "site" }
    title   = { S = "My website" }
    enabled = { BOOL = true }
  })
}
