data "catalyst_project_import_job" "example" {
  id = var.import_job_id
}

output "import_status" {
  value = data.catalyst_project_import_job.example.status
}

output "created_project_id" {
  value = data.catalyst_project_import_job.example.project_id
}
