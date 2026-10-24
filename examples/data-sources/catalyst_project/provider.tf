terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

# Each user supplies credentials and region using CATALYST_ environment variables.
provider "catalyst" {}

variable "project_id" {
  type        = string
  description = "ID of an existing Catalyst project accessible to your OAuth credentials."
}

variable "required_timezone" {
  type        = string
  default     = null
  description = "Optional expected project timezone. Configure it in Catalyst General Settings; this example only verifies it."
}
