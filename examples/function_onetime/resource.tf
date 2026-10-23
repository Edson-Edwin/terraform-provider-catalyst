resource "catalyst_function_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.function_pool_id
  function_id = var.job_function_id
  name        = "terraform_one_time"
  job_name    = "terraform_job"
  timezone    = "Asia/Kolkata"
  one_time_at = var.execution_time
  enabled     = false
}
