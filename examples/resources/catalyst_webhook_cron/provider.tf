terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

provider "catalyst" {}

variable "project_id" {
  type        = string
  description = "Existing development project ID."
}

variable "job_pool_id" {
  type        = string
  description = "Existing Webhook job pool ID in that project."
}

variable "end_time" {
  type        = number
  description = "Future Unix timestamp in seconds at which the schedule ends."
}
