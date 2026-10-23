# Build your Function ZIP before Terraform applies. Its configuration and
# package.json must both name this Function "example_http".
resource "catalyst_function" "example" {
  project_id            = var.project_id
  name                  = "example_http"
  stack                 = "node24"
  type                  = "advancedio"
  source                = var.function_zip
  source_sha256         = var.function_zip_sha256
  memory                = 256
  environment_variables = { APP_MODE = "example" }
}
