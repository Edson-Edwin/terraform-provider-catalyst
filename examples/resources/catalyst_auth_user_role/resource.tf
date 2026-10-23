variable "project_id" { type = string }
variable "user_id" { type = string }
variable "role_id" { type = string }
variable "fallback_role_id" { type = string }

# Experimental: existing application end-user and roles are prerequisites.
# This resource does not configure login, create roles or edit permissions.
resource "catalyst_auth_user_role" "example" {
  project_id      = var.project_id
  user_id         = var.user_id
  role_id         = var.role_id
  destroy_role_id = var.fallback_role_id
}
