resource "catalyst_appsail" "example" {
  project_id            = var.project_id
  name                  = "exampleapp"
  source                = var.source_zip
  source_sha256         = var.source_sha256
  memory                = 128
  startup_command       = "node index.js"
  port                  = 9000
  catalyst_auth         = true
  environment_variables = { APP_MODE = "example" }
}
