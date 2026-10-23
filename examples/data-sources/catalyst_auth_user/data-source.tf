variable "project_id" { type = string }
variable "user_id" { type = string }

data "catalyst_auth_user" "example" {
  project_id = var.project_id
  user_id    = var.user_id
}

output "role_id" {
  value = data.catalyst_auth_user.example.role_id
}
