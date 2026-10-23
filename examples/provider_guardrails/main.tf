terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

variable "data_center" {
  type        = string
  description = "Explicit data center hosting the Production project."
}

variable "organization_id" {
  type        = string
  description = "Organization targeted by this configuration."
}

variable "approved_data_centers" {
  type        = set(string)
  description = "Independently maintained data center allowlist."
  validation {
    condition     = length(var.approved_data_centers) > 0
    error_message = "Supply at least one approved data center for this guarded example."
  }
}

variable "approved_organization_ids" {
  type        = set(string)
  description = "Independently maintained organization allowlist."
  validation {
    condition     = length(var.approved_organization_ids) > 0
    error_message = "Supply at least one approved organization for this guarded example."
  }
}

variable "validation_project_id" {
  type        = string
  description = "Existing project readable with the supplied credentials."
}

# Inject CATALYST_ACCESS_TOKEN, or the complete OAuth refresh credential trio.
provider "catalyst" {
  data_center              = var.data_center
  environment              = "Production"
  allowed_data_centers     = var.approved_data_centers
  allowed_environments     = ["Production"]
  organization_id          = var.organization_id
  allowed_organization_ids = var.approved_organization_ids
  validation_project_id    = var.validation_project_id
}

# This example only reads. A successful preflight does not prove write access.
data "catalyst_project" "target" {
  id = var.validation_project_id
}
