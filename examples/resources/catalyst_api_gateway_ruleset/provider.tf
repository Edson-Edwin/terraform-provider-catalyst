terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}
# Set CATALYST_ORGANIZATION_ID explicitly, along with credentials, region and environment.
provider "catalyst" {}
variable "project_id" {
  type        = string
  description = "ID of your existing isolated Catalyst project."
}
variable "function_id" {
  type        = string
  description = "ID of an existing Basic I/O Function."
}
variable "function_name" {
  type        = string
  description = "Name of the same Basic I/O Function."
}
