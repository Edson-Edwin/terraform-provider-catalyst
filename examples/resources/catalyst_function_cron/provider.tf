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
  description = "Existing Catalyst project ID."
}
variable "function_pool_id" {
  type        = string
  description = "Existing Function Job Pool in this project."
}
variable "job_function_id" {
  type        = string
  description = "Existing Job Function ID (type job), not a legacy Cron Function."
}
variable "end_time" {
  type        = number
  description = "Positive Unix timestamp in seconds; use a future time when enabling."
}
