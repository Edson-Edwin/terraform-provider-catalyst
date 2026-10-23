resource "catalyst_function_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.function_pool_id
  function_id = var.job_function_id
  name        = "terraform_function_cron"
  job_name    = "terraform_job"
  expression  = "0 0 * * *"
  timezone    = "Asia/Kolkata"
  end_time    = var.end_time
  enabled     = false
  params      = {}
}
