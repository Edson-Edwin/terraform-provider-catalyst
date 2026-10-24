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
variable "appsail_pool_id" {
  type        = string
  description = "Existing AppSail Job Pool in this project."
}
variable "appsail_id" {
  type        = string
  description = "Existing deployed AppSail ID in this project."
}
variable "end_time" {
  type        = number
  description = "Positive Unix timestamp in seconds; use a future time when enabling."
}
