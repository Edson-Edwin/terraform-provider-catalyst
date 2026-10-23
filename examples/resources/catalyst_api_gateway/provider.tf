terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}
provider "catalyst" {}
variable "project_id" {
  type        = string
  description = "ID of your existing isolated Catalyst project."
}
