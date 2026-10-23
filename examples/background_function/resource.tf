# Deploys the handler only. It does not create a Job Pool, cron or invocation.
resource "catalyst_function" "example" {
  project_id    = var.project_id
  name          = var.function_name
  stack         = "node24"
  type          = "job"
  source        = var.source_zip
  source_sha256 = filesha256(var.source_zip)
  memory        = 128
}
