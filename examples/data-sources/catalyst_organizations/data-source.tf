terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_organizations" "visible" {}

output "organization_ids" {
  value = data.catalyst_organizations.visible.organizations[*].id
}
