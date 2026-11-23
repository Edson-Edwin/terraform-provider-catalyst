terraform {
  required_providers {
    catalyst = {
      source = "edson-edwin/catalyst"
    }
  }
}

data "catalyst_datastream_channels" "existing" {
  project_id = "123456789"
}

output "channel_ids" {
  value = data.catalyst_datastream_channels.existing.channels[*].id
}
