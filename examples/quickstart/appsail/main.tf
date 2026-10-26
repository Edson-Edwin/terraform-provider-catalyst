terraform {
  required_version = ">= 1.13.0"
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

variable "project_id" {
  type        = string
  description = "An existing isolated Development project that you own."
  validation {
    condition     = can(regex("^[1-9][0-9]*$", var.project_id))
    error_message = "Use a positive numeric Catalyst project ID."
  }
}

variable "app_name" {
  type        = string
  description = "A unique trial app name; the launcher generates and remembers it."
}

variable "source_zip" {
  type        = string
  description = "The launcher packs the dependency-free demonstration app."
}

provider "catalyst" {
  environment           = "Development"
  allowed_environments  = ["Development"]
  validation_project_id = var.project_id
}

resource "catalyst_appsail" "trial" {
  project_id      = var.project_id
  name            = var.app_name
  stack           = "node24"
  source          = var.source_zip
  source_sha256   = filesha256(var.source_zip)
  memory          = 128
  startup_command = "node index.js"
  port            = 9000
  # This constant-response demo has no credentials or application data.
  # Disabling app authentication makes its demonstration URL publicly accessible.
  catalyst_auth = false
}

output "demo_url" {
  description = "Public demonstration endpoint. Startup may take time after apply."
  value       = catalyst_appsail.trial.url
}

output "demo_app_name" {
  value = catalyst_appsail.trial.name
}
