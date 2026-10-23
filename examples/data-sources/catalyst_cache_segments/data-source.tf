data "catalyst_cache_segments" "example" {
  project_id = var.project_id
}

output "segments" {
  value = data.catalyst_cache_segments.example.segments
}
