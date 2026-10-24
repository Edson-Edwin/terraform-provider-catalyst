terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}
# Set CATALYST_ORGANIZATION_ID explicitly, along with credentials, region and environment.
provider "catalyst" {}

variable "project_id" {
  type        = string
  description = "Existing Catalyst project ID."
}
variable "function_name" {
  type        = string
  description = "Unique name matching the ZIP configuration and package name."
}
variable "source_zip" {
  type        = string
  description = "Prebuilt Node.js 24 Job Function ZIP with its dependencies and correct handler signature."
}
