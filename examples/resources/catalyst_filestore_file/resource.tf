# The folder must already exist. All local source bytes are checked at apply.
resource "catalyst_filestore_file" "example" {
  project_id    = var.project_id
  folder_id     = var.folder_id
  name          = "settings.json"
  source        = "${path.module}/settings.json"
  source_sha256 = filesha256("${path.module}/settings.json")
}
