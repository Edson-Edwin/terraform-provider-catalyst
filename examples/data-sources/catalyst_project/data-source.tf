data "catalyst_project" "example" {
  id = var.project_id

  lifecycle {
    postcondition {
      condition     = var.required_timezone == null || self.timezone == var.required_timezone
      error_message = "Set the required project timezone in Catalyst General Settings."
    }
  }
}

output "project_name" {
  value = data.catalyst_project.example.name
}

output "project_domain" {
  value = data.catalyst_project.example.domain
}
