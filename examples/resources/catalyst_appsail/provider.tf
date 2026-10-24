terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

# Each user supplies credentials and region using CATALYST_ environment variables.
# Set CATALYST_ORGANIZATION_ID explicitly, along with credentials, region and environment.
provider "catalyst" {}

variable "project_id" {
  type        = string
  description = "ID of an existing Catalyst project accessible to your OAuth credentials."
}

variable "source_zip" {
  type        = string
  description = "Path to the prebuilt application ZIP."
}
variable "source_sha256" {
  type        = string
  description = "Lowercase SHA-256 of the ZIP, for example filesha256(var.source_zip) from a calling module."
}
