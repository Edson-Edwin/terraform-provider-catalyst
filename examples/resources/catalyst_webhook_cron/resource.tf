resource "catalyst_webhook_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.job_pool_id
  name        = "terraform_example"
  job_name    = "terraform_example"
  description = "Disabled webhook schedule managed by Terraform"

  expression = "0 9 * * 1-5"
  timezone   = "Asia/Kolkata"
  end_time   = var.end_time
  enabled    = false

  url    = "https://example.com/your-webhook"
  method = "get"
}
