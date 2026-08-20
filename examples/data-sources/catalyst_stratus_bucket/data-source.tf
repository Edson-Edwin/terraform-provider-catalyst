terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_stratus_bucket" "existing" {
  project_id  = "123456789"
  bucket_name = "example-bucket"
}

output "bucket_versioning_enabled" {
  value = data.catalyst_stratus_bucket.existing.versioning_enabled
}
