terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}

variable "project_id" {
  type        = string
  description = "Existing Development project ID."
}

variable "bot_id" {
  type        = string
  description = "Existing Development bot with SmartTrain enabled."
}

variable "material_url" {
  type        = string
  sensitive   = true
  description = "Public HTTPS URL of the training material."
}
