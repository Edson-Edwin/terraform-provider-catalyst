variable "project_id" {
  type        = string
  description = "Existing Catalyst project ID."
}
variable "bucket_name" {
  type        = string
  description = "Existing unversioned Stratus bucket name."
}
variable "object_source" {
  type        = string
  description = "Path to an existing local file, up to 100 MB."
}
resource "catalyst_stratus_object" "example" {
  project_id    = var.project_id
  bucket_name   = var.bucket_name
  key           = "assets/example.bin"
  source        = var.object_source
  source_sha256 = filesha256(var.object_source)
  content_type  = "application/octet-stream"
}
