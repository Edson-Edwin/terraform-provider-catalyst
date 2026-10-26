terraform {
  required_version = ">= 1.13.0"
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

provider "catalyst" {
  environment          = "Development"
  allowed_environments = ["Development"]
}

data "catalyst_projects" "visible" {}

output "visible_projects" {
  description = "Projects visible to your own OAuth identity; keep this output private."
  value       = { for project in data.catalyst_projects.visible.projects : project.id => project.name }
}
