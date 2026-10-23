resource "catalyst_function_cron" "example" {
  project_id       = var.project_id
  job_pool_id      = var.function_pool_id
  function_id      = var.job_function_id
  name             = "terraform_periodic"
  job_name         = "terraform_job"
  interval_seconds = 21600
  end_time         = var.end_time
  enabled          = false
}
