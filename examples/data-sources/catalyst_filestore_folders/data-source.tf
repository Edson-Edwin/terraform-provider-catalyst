data "catalyst_filestore_folders" "example" {
  project_id = var.project_id
}

output "folders" {
  value = data.catalyst_filestore_folders.example.folders
}
