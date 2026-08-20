terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_project_environments" "existing" {
  project_id = "123456789"
}

output "project_environments" {
  value = data.catalyst_project_environments.existing.environments
}
