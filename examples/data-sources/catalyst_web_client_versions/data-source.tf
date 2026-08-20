terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_web_client_versions" "existing" {
  project_id = "123456789"
}

output "web_client_versions" {
  value = data.catalyst_web_client_versions.existing.versions
}
