# All 13 registered resources and 12 data sources in one configuration.
# Optional objects default to null: no writes occur until you configure them.
# Start with an existing isolated Development project. See README.md and
# terraform.tfvars.example for prerequisites and inputs. This is a catalog of
# supported subsets, not automatic provisioning of every Catalyst service.
terraform {
  required_version = ">= 1.3.0"
  required_providers {
    catalyst = { source = "Edson-Edwin/catalyst" }
  }
}

# Credentials: CATALYST_ACCESS_TOKEN OR the three CATALYST_CLIENT_ID,
# CATALYST_CLIENT_SECRET and CATALYST_REFRESH_TOKEN environment variables.
provider "catalyst" {
  data_center = "in"
  environment = "Development"
}

variable "project_id" {
  type        = string
  description = "Existing isolated India Development project; all features except experimental_project use it."
}

variable "application_environment" {
  type        = map(string)
  default     = {}
  sensitive   = true
  description = "Complete environment map for AppSail and Functions. Values remain in Terraform state. Do not include provider credentials."
}

variable "appsail" {
  type = object({
    name            = string
    source          = string
    stack           = optional(string, "node24")
    startup_command = optional(string, "node index.js")
    memory          = optional(number, 256)
    port            = optional(number, 9000)
    catalyst_auth   = optional(bool, true)
  })
  default     = null
  description = "Prebuilt application ZIP. Node, Java SE and Python are supported; Java/Python need an explicit startup command."
}

variable "functions" {
  type = map(object({
    source = string
    type   = optional(string, "basicio")
    stack  = optional(string, "node24")
    memory = optional(number, 256)
  }))
  default     = {}
  description = "Map keys are Function names and must match ZIP/package configuration. Types: basicio, advancedio, event, cron, job; stacks: node22/node24."
}

variable "datastore" {
  type = object({
    table_id = string
    message  = string
  })
  default     = null
  description = "Existing Data Store table with a nullable text column named message; creates one Terraform-owned configuration row."
}

variable "nosql_table_id" {
  type        = string
  default     = null
  description = "Existing ONLINE NoSQL table with string partition key pk and string sort key sk; creates one configuration item."
}

variable "stratus" {
  type = object({
    bucket_name            = string
    source                 = string
    key                    = optional(string, "terraform-example/site.json")
    content_type           = optional(string, "application/json")
    multipart_part_size_mb = optional(number, 0)
    metadata               = optional(map(string), { managed_by = "terraform" })
  })
  default     = null
  description = "Existing private authenticated unversioned bucket; unique object key and local source <=100 MiB. Multipart: 0 disables, 5..20 enables."
}

variable "gateway" {
  type = object({
    function_name = string
  })
  default     = null
  description = "Select a basicio/advancedio key from functions. Owns the project's COMPLETE custom ruleset; use only a dedicated project with no existing rules."
}

variable "webhook_schedule" {
  type = object({
    job_pool_id = string
    url         = string
    end_time    = number
  })
  default     = null
  description = "Existing Webhook pool, owned HTTPS endpoint and fixed future Unix-seconds end time. Creates a disabled daily schedule."
}

variable "function_schedule" {
  type = object({
    job_pool_id   = string
    function_name = string
    end_time      = number
  })
  default     = null
  description = "Existing Function pool and a type=job key in functions. Creates a disabled daily schedule; end_time is fixed Unix seconds."
}

variable "appsail_schedule" {
  type = object({
    job_pool_id = string
    end_time    = number
    path        = optional(string, "/health")
  })
  default     = null
  description = "Existing AppSail pool; requires appsail configuration. Creates a disabled daily schedule. end_time is fixed Unix seconds."
}

variable "authentication" {
  type = object({
    user_id = string
    assignment = optional(object({
      role_id         = string
      destroy_role_id = string
    }))
  })
  default     = null
  description = "Existing application user metadata. Optional assignment is EXPERIMENTAL/offline-only: use a disposable end-user and existing roles; destroy assigns the fallback role."
}

variable "legacy_filestore" {
  type = object({
    folder_id = string
    source    = string
    name      = optional(string, "terraform-example.json")
  })
  default     = null
  description = "DEPRECATED File Store: existing folder and local file. Opt in only for legacy applications."
}

variable "experimental_project" {
  type        = object({ name = string })
  default     = null
  description = "EXPERIMENTAL additional EMPTY project; current account creation was rejected. Does not replace project_id or create database/bucket prerequisites. Deletion guard stays enabled."
}

# Discovery: these read existing infrastructure; they do not provision it.
data "catalyst_projects" "account" {}
data "catalyst_project" "current" { id = var.project_id }
data "catalyst_job_pools" "current" { project_id = var.project_id }
data "catalyst_datastore_tables" "current" { project_id = var.project_id }
data "catalyst_nosql_tables" "current" { project_id = var.project_id }
data "catalyst_stratus_buckets" "current" { project_id = var.project_id }
data "catalyst_cache_segments" "current" { project_id = var.project_id }
data "catalyst_functions" "current" { project_id = var.project_id }

data "catalyst_datastore_table" "configuration" {
  count      = var.datastore == null ? 0 : 1
  project_id = var.project_id
  id         = var.datastore.table_id
}

data "catalyst_nosql_table" "configuration" {
  count      = var.nosql_table_id == null ? 0 : 1
  project_id = var.project_id
  id         = var.nosql_table_id
}

data "catalyst_filestore_folders" "legacy" {
  count      = var.legacy_filestore == null ? 0 : 1
  project_id = var.project_id
}

data "catalyst_auth_user" "existing" {
  count      = var.authentication == null ? 0 : 1
  project_id = var.project_id
  user_id    = var.authentication.user_id
  depends_on = [catalyst_auth_user_role.experimental]
}

# Database configuration data only; application transactions belong outside TF.
resource "catalyst_datastore_row" "configuration" {
  count       = var.datastore == null ? 0 : 1
  project_id  = var.project_id
  table_id    = data.catalyst_datastore_table.configuration[0].id
  values_json = jsonencode({ message = var.datastore.message })
}

resource "catalyst_nosql_item" "configuration" {
  count      = var.nosql_table_id == null ? 0 : 1
  project_id = var.project_id
  table_id   = data.catalyst_nosql_table.configuration[0].id
  keys_json  = jsonencode({ pk = { S = "terraform" }, sk = { S = "all_services_example" } })
  item_json = jsonencode({
    pk      = { S = "terraform" }
    sk      = { S = "all_services_example" }
    title   = { S = "Terraform configuration" }
    enabled = { BOOL = true }
  })
  lifecycle {
    precondition {
      condition = (
        try(jsondecode(data.catalyst_nosql_table.configuration[0].schema_json).partition_key.column_name == "pk", false) &&
        try(jsondecode(data.catalyst_nosql_table.configuration[0].schema_json).partition_key.data_type == "S", false) &&
        try(jsondecode(data.catalyst_nosql_table.configuration[0].schema_json).sort_key.column_name == "sk", false) &&
        try(jsondecode(data.catalyst_nosql_table.configuration[0].schema_json).sort_key.data_type == "S", false)
      )
      error_message = "Use an existing ONLINE table with string keys pk and sk."
    }
  }
}

resource "catalyst_stratus_object" "configuration" {
  count                  = var.stratus == null ? 0 : 1
  project_id             = var.project_id
  bucket_name            = var.stratus.bucket_name
  key                    = var.stratus.key
  source                 = var.stratus.source
  source_sha256          = filesha256(var.stratus.source)
  content_type           = var.stratus.content_type
  metadata               = var.stratus.metadata
  multipart_part_size_mb = var.stratus.multipart_part_size_mb
}

resource "catalyst_appsail" "website" {
  count           = var.appsail == null ? 0 : 1
  project_id      = var.project_id
  name            = var.appsail.name
  stack           = var.appsail.stack
  platform        = startswith(var.appsail.stack, "java") ? "javase" : ""
  source          = var.appsail.source
  source_sha256   = filesha256(var.appsail.source)
  startup_command = var.appsail.startup_command
  memory          = var.appsail.memory
  port            = var.appsail.port
  catalyst_auth   = var.appsail.catalyst_auth
  environment_variables = merge(var.application_environment, {
    DATASTORE_TABLE_ID      = try(data.catalyst_datastore_table.configuration[0].id, "")
    DATASTORE_CONFIG_ROW_ID = try(catalyst_datastore_row.configuration[0].row_id, "")
    NOSQL_TABLE_ID          = try(data.catalyst_nosql_table.configuration[0].id, "")
    STRATUS_BUCKET          = try(catalyst_stratus_object.configuration[0].bucket_name, "")
    STRATUS_OBJECT_KEY      = try(catalyst_stratus_object.configuration[0].key, "")
    SITE_CONFIG_PK          = "terraform"
    SITE_CONFIG_SK          = "all_services_example"
  })
  depends_on = [catalyst_nosql_item.configuration]
}

resource "catalyst_function" "application" {
  for_each              = var.functions
  project_id            = var.project_id
  name                  = each.key
  stack                 = each.value.stack
  type                  = each.value.type
  source                = each.value.source
  source_sha256         = filesha256(each.value.source)
  memory                = each.value.memory
  environment_variables = var.application_environment
}

# Complete custom ruleset ownership. Do not combine with another ruleset owner.
resource "catalyst_api_gateway" "application" {
  count      = var.gateway == null ? 0 : 1
  project_id = var.project_id
  enabled    = true
}

resource "catalyst_api_gateway_ruleset" "application" {
  count      = var.gateway == null ? 0 : 1
  project_id = catalyst_api_gateway.application[0].id
  rules = {
    backend = {
      source_endpoint = "/terraform-example"
      target_endpoint = "/server/${var.gateway.function_name}/execute"
      function_id     = try(catalyst_function.application[var.gateway.function_name].id, null)
      target          = try(var.functions[var.gateway.function_name].type, "basicio")
      method          = "GET"
      authentication  = ["OAuth"]
    }
  }
  lifecycle {
    precondition {
      condition     = try(contains(["basicio", "advancedio"], var.functions[var.gateway.function_name].type), false)
      error_message = "gateway.function_name must select a basicio/advancedio entry in functions; Advanced I/O code must serve /execute."
    }
  }
}

# Schedules are deliberately disabled. These blocks do not create Job Pools.
resource "catalyst_webhook_cron" "daily" {
  count       = var.webhook_schedule == null ? 0 : 1
  project_id  = var.project_id
  job_pool_id = var.webhook_schedule.job_pool_id
  name        = "tf_example_webhook"
  job_name    = "tf_webhook"
  expression  = "0 9 * * *"
  timezone    = "Asia/Kolkata"
  end_time    = var.webhook_schedule.end_time
  enabled     = false
  url         = var.webhook_schedule.url
  method      = "get"
}

resource "catalyst_function_cron" "daily" {
  count       = var.function_schedule == null ? 0 : 1
  project_id  = var.project_id
  job_pool_id = var.function_schedule.job_pool_id
  function_id = try(catalyst_function.application[var.function_schedule.function_name].id, null)
  name        = "tf_example_function"
  job_name    = "tf_function"
  expression  = "0 9 * * *"
  timezone    = "Asia/Kolkata"
  end_time    = var.function_schedule.end_time
  enabled     = false
  params      = {}
  lifecycle {
    precondition {
      condition     = try(var.functions[var.function_schedule.function_name].type == "job", false)
      error_message = "function_schedule.function_name must select a type=job entry in functions."
    }
  }
}

resource "catalyst_appsail_cron" "daily" {
  count       = var.appsail_schedule == null ? 0 : 1
  project_id  = var.project_id
  job_pool_id = var.appsail_schedule.job_pool_id
  appsail_id  = try(catalyst_appsail.website[0].id, null)
  name        = "tf_example_appsail"
  job_name    = "tf_appsail"
  expression  = "0 9 * * *"
  timezone    = "Asia/Kolkata"
  end_time    = var.appsail_schedule.end_time
  enabled     = false
  path        = var.appsail_schedule.path
  method      = "get"
  params      = {}
  lifecycle {
    precondition {
      condition     = var.appsail != null
      error_message = "appsail_schedule requires an appsail configuration in this example."
    }
  }
}

# Experimental and deprecated features: opt in separately after reviewing docs.
resource "catalyst_auth_user_role" "experimental" {
  count           = var.authentication == null ? 0 : (var.authentication.assignment == null ? 0 : 1)
  project_id      = var.project_id
  user_id         = var.authentication.user_id
  role_id         = var.authentication.assignment.role_id
  destroy_role_id = var.authentication.assignment.destroy_role_id
}

resource "catalyst_filestore_file" "legacy" {
  count         = var.legacy_filestore == null ? 0 : 1
  project_id    = var.project_id
  folder_id     = var.legacy_filestore.folder_id
  name          = var.legacy_filestore.name
  source        = var.legacy_filestore.source
  source_sha256 = filesha256(var.legacy_filestore.source)
}

resource "catalyst_project" "experimental" {
  count               = var.experimental_project == null ? 0 : 1
  name                = var.experimental_project.name
  deletion_protection = true
}

output "website_url" { value = try(catalyst_appsail.website[0].url, null) }
output "function_ids" { value = { for name, fn in catalyst_function.application : name => fn.id } }
output "datastore_configuration_row_id" { value = try(catalyst_datastore_row.configuration[0].row_id, null) }
output "experimental_project_id" { value = try(catalyst_project.experimental[0].id, null) }

output "inventory_counts" {
  description = "Discovery only; these counts are not newly provisioned resources or a service coverage score."
  value = {
    project_name     = data.catalyst_project.current.name
    account_projects = length(data.catalyst_projects.account.projects)
    job_pools        = length(data.catalyst_job_pools.current.pools)
    datastore_tables = length(data.catalyst_datastore_tables.current.tables)
    nosql_tables     = length(data.catalyst_nosql_tables.current.tables)
    stratus_buckets  = length(data.catalyst_stratus_buckets.current.buckets)
    cache_segments   = length(data.catalyst_cache_segments.current.segments)
    functions        = length(data.catalyst_functions.current.functions)
  }
}
