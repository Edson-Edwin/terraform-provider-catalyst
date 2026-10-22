data "catalyst_job_pools" "example" {
  project_id = var.project_id
}

output "pools" {
  value = data.catalyst_job_pools.example.pools
}
