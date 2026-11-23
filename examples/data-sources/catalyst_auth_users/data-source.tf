terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_auth_users" "first_page" {
  project_id = "123456789"
  start      = 0
  limit      = 50
}

output "user_ids" {
  value = data.catalyst_auth_users.first_page.users[*].id
}
