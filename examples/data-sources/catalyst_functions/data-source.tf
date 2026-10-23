data "catalyst_functions" "example" {
  project_id = var.project_id
}

output "functions" {
  value = data.catalyst_functions.example.functions
}
