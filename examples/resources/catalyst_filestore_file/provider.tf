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

variable "folder_id" {
  type        = string
  description = "ID of an existing File Store folder."
}
