data "catalyst_stratus_buckets" "example" {
  project_id = var.project_id
}

output "buckets" {
  value = data.catalyst_stratus_buckets.example.buckets
}
