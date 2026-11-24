terraform {
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}

variable "import_job_id" {
  type        = string
  description = "Job ID returned after submitting an IaC import in the Catalyst CLI or console."
}
