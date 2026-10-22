data "catalyst_project" "example" {
  id = var.project_id
}

output "project_name" {
  value = data.catalyst_project.example.name
}

output "project_domain" {
  value = data.catalyst_project.example.domain
}
