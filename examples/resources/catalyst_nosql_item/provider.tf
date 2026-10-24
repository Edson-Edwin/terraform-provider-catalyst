terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

# Each user supplies credentials and region using CATALYST_ environment variables.
# Set CATALYST_ORGANIZATION_ID explicitly, along with credentials, region and environment.
provider "catalyst" {}
