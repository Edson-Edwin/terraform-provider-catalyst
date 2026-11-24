resource "catalyst_smarttrain_material" "example" {
  project_id = var.project_id
  bot_id     = var.bot_id
  title      = "Terraform onboarding guide"
  url        = var.material_url
}
