resource "catalyst_project" "example" {
  name                = "ExampleWebsite"
  deletion_protection = true

  timeouts = {
    create = "5m"
    read   = "2m"
    update = "2m"
    delete = "10m"
  }
}
