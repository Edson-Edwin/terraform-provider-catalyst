terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_job_pool" "selected" {
  project_id = "123456789"
  id         = "987654321"
}

output "job_pool_capacity" {
  value = {
    memory_mb = data.catalyst_job_pool.selected.capacity_memory_mb
    count     = data.catalyst_job_pool.selected.capacity_count
  }
}
