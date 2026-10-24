terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}
# Set CATALYST_ORGANIZATION_ID explicitly, along with credentials, region and environment.
provider "catalyst" {}
