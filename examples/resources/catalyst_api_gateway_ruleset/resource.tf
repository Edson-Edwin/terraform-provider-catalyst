resource "catalyst_api_gateway" "example" {
  project_id = var.project_id
  enabled    = true
}

resource "catalyst_api_gateway_ruleset" "example" {
  project_id = catalyst_api_gateway.example.id
  rules = {
    protected = {
      source_endpoint = "/example"
      target_endpoint = "/server/${var.function_name}/execute"
      function_id     = var.function_id
      method          = "GET"
      authentication  = ["OAuth"]
    }
  }
}
