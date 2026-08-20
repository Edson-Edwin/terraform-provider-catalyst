terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_slate_apps" "existing" {
  project_id = "123456789"
}

output "slate_apps" {
  value = data.catalyst_slate_apps.existing.apps
}
