terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_stratus_object_versions" "existing" {
  project_id  = "123456789"
  bucket_name = "example-versioned-bucket"
  object_key  = "assets/config.json"
}

output "stratus_version_ids" {
  value = data.catalyst_stratus_object_versions.existing.versions[*].version_id
}
