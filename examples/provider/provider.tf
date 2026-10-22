terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

# Set CATALYST_DATA_CENTER to your account's region (us, eu, in, au, ca, jp, sa).
# Set CATALYST_ACCESS_TOKEN, OR CATALYST_CLIENT_ID, CATALYST_CLIENT_SECRET,
# and CATALYST_REFRESH_TOKEN. Credentials are only needed for actual API reads.
provider "catalyst" {}
