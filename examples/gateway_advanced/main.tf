terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}
provider "catalyst" {}

variable "project_id" { type = string }
variable "function_id" { type = string }
variable "function_name" { type = string }

# Requires an existing Advanced I/O Function and exclusive ruleset ownership.
resource "catalyst_api_gateway" "example" {
  project_id = var.project_id
}
resource "catalyst_api_gateway_ruleset" "example" {
  project_id = catalyst_api_gateway.example.id
  rules = {
    backend = {
      source_endpoint = "/api/{path:(.*)}"
      target_endpoint = "/server/${var.function_name}/{path}"
      target          = "advancedio"
      function_id     = var.function_id
      method          = "ANY"
      authentication  = ["OAuth", "APIKey", "CatalystUserManagement"]
    }
  }
}
