resource "catalyst_function_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.function_pool_id
  function_id = var.job_function_id
  name        = "terraform_monthly"
  job_name    = "terraform_job"
  timezone    = "Asia/Kolkata"
  end_time    = var.end_time
  enabled     = false
  calendar_schedule = {
    repetition = "monthly"
    hour       = 1
    minute     = 30
    second     = 0
    days       = [1, 15]
  }
}
