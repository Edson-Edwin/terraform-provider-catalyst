resource "catalyst_webhook_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.job_pool_id
  name        = "terraform_webhook_monthly"
  job_name    = "terraform_job"
  url         = "https://example.com/terraform-provider-test"
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
