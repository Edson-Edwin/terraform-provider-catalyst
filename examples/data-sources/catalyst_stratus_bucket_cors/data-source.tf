terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_stratus_bucket_cors" "existing" {
  project_id  = "123456789"
  bucket_name = "example-bucket"
}

output "bucket_cors_rules" {
  value = data.catalyst_stratus_bucket_cors.existing.rules
}
