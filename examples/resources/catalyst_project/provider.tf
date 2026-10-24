terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}
variable "organization_id" {
  type        = string
  description = "Explicit organization that owns the managed project."
}

variable "data_center" {
  type        = string
  description = "Data center that hosts the project; keep this stable after creation or import."
}

# Inject access-token or OAuth refresh credentials through the environment.
provider "catalyst" {
  organization_id = var.organization_id
  data_center     = var.data_center
  environment     = "Development"
}
