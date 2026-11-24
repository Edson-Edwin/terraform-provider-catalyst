resource "catalyst_circuit_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.circuit_pool_id
  circuit_id  = var.circuit_id
  name        = "terraform_circuit_cron"
  job_name    = "terraform_circuit"
  expression  = "0 9 * * *"
  end_time    = var.end_time
  enabled     = false
}
