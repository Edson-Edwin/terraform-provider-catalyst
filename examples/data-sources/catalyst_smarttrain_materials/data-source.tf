terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_smarttrain_materials" "existing" {
  project_id = "123456789"
  bot_id     = "234567890"
}

output "smarttrain_materials" {
  value = data.catalyst_smarttrain_materials.existing.materials
}
