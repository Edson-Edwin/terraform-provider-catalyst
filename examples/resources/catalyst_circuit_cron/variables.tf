terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}

variable "project_id" {
  type        = string
  description = "An existing Development project in an eligible data center."
}

variable "circuit_pool_id" {
  type        = string
  description = "Existing Circuit Job Pool ID."
}

variable "circuit_id" {
  type        = string
  description = "Existing Circuit ID."
}

variable "end_time" {
  type        = number
  description = "Fixed Unix timestamp in seconds at which the schedule ends."
}
