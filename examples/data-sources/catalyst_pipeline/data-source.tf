terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_pipeline" "existing" {
  project_id = "123456789"
  id         = "234567890"
}

output "pipeline" {
  value = data.catalyst_pipeline.existing.name
}
