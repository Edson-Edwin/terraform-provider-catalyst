resource "catalyst_appsail_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.appsail_pool_id
  appsail_id  = var.appsail_id
  name        = "terraform_appsail_cron"
  job_name    = "terraform_job"
  expression  = "0 0 * * *"
  end_time    = var.end_time
  enabled     = false
  path        = "/health"
  method      = "get"
  params      = {}
}
