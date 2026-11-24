terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}

variable "project_id" {
  type        = string
  description = "An existing Catalyst project with SmartBrowz access."
}
