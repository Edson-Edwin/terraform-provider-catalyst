data "catalyst_projects" "example" {}

output "project_ids_by_name" {
  value = { for project in data.catalyst_projects.example.projects : project.name => project.id... }
}
