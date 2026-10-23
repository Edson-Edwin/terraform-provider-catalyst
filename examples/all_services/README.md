# All currently registered Catalyst resources and data sources

`main.tf` contains all **13 resource types and 12 data source types** currently
registered by this provider. It uses one provider, configured for India
Development. It demonstrates supported subsets, not complete Catalyst service
coverage. AppSail and Function applications must be built separately.

All optional resource inputs default to null or an empty map. Supplying only
`project_id` reads inventory and creates nothing. The companion
`terraform.tfvars.example` enables the core managed subsets once its placeholders
are replaced. Delete any unwanted input section before applying. Experimental
and deprecated sections are commented out independently.

## What the file contains

| Service | Resource blocks | Data source blocks |
| --- | --- | --- |
| Projects | `catalyst_project` (experimental, separate additional project) | `catalyst_projects`, `catalyst_project` |
| AppSail | `catalyst_appsail` | None |
| Functions | `catalyst_function` (map supports multiple Functions/types) | `catalyst_functions` |
| Stratus | `catalyst_stratus_object`, including optional multipart transfers | `catalyst_stratus_buckets` |
| NoSQL | `catalyst_nosql_item` | `catalyst_nosql_tables`, `catalyst_nosql_table` |
| Data Store | `catalyst_datastore_row` | `catalyst_datastore_tables`, `catalyst_datastore_table` |
| API Gateway | `catalyst_api_gateway`, `catalyst_api_gateway_ruleset` | None |
| Job Scheduling | `catalyst_webhook_cron`, `catalyst_function_cron`, `catalyst_appsail_cron` | `catalyst_job_pools` |
| Authentication | `catalyst_auth_user_role` (experimental) | `catalyst_auth_user` |
| Cache | None; discovery only | `catalyst_cache_segments` |
| Legacy File Store | `catalyst_filestore_file` (deprecated) | `catalyst_filestore_folders` (deprecated) |

## Prerequisites

Use an existing isolated project and supply credentials with the read scopes for
the inventory data sources plus the scopes required for whichever resource
sections you enable. The provider does not consume the Catalyst CLI login. Set
`CATALYST_CLIENT_ID`, `CATALYST_CLIENT_SECRET` and `CATALYST_REFRESH_TOKEN`, or use
`CATALYST_ACCESS_TOKEN` instead of refresh credentials. Keep credentials outside
Terraform inputs and application archives. Resource values marked sensitive
still remain in Terraform state.

For each enabled input:

- `appsail`: a prebuilt ZIP with installed dependencies. Bind the application to
  `0.0.0.0` and `X_ZOHO_CATALYST_LISTEN_PORT` (9000 by default). Java SE and Python
  require a matching stack and explicit startup command. Containers and WAR are
  not supported. Authentication defaults to enabled.
- `functions`: Node22/24 ZIPs with matching `catalyst-config.json`, `package.json`,
  names/types and entry points. Event/cron triggers are separate prerequisites;
  scheduling here uses a **Job** Function.
- `datastore`: an existing table with a nullable text column named `message`.
- `nosql_table_id`: an existing ONLINE table with string keys `pk` and `sk`. The
  example exclusively owns the item `terraform` / `all_services_example`.
- `stratus`: an existing private authenticated unversioned bucket and a source
  file no larger than 100 MiB. Use a new exclusively owned object key.
- `gateway`: a Basic/Advanced I/O key from `functions`. This example owns the
  **complete custom ruleset**; use a project without existing rules. Basic I/O
  uses its `/execute` endpoint; Advanced I/O code must implement `/execute`.
  Routes require OAuth. This does not configure user login or roles.
- Schedules: existing correctly typed Webhook, Function and AppSail job pools,
  fixed future Unix-seconds end timestamps and appropriate target application
  code/URLs. All schedules remain disabled in this file.
- `authentication`: an existing application user for discovery. Only opt into
  assignment with a disposable end-user and two suitable existing roles. The
  assignment resource has offline verification only; destroy assigns the
  fallback role and never deletes the user.
- `legacy_filestore`: an existing legacy folder and local file. Deprecation
  warnings are expected even when these blocks are disabled.
- `experimental_project`: API creation eligibility. This creates a separate
  empty project and never substitutes for `project_id`. Live creation was
  rejected for the current account; deletion protection is enabled.

Cache is discovery only. The file does not create tables, columns, indexes,
buckets, job pools, roles or permissions. It does not configure Connections,
containers, scaling controls, custom domains or Production promotion.

## Run locally

From the repository root:

```sh
make dev
export TF_CLI_CONFIG_FILE="$PWD/.terraformrc.local"
cd examples/all_services
cp terraform.tfvars.example terraform.tfvars
```

Replace every active placeholder, point source paths to your own built artifacts,
remove unwanted sections, and set provider credentials in your environment.
Then run:

```sh
terraform validate
terraform plan -out=terraform.tfplan
terraform apply terraform.tfplan
```

Skip Registry initialization while using this local development override. The
provider has not been established as installable from the public Registry.
`website_url` reports the deployed endpoint; apply alone does not prove the
application runs correctly. The application must explicitly read the passed
database/storage environment values and implement its own business logic.

`terraform destroy` deletes the managed application, Functions, configuration
row/item, objects/files, schedules and Gateway rules/configuration according to
the enabled resources. It leaves manually provisioned prerequisites in place.
If enabled, role assignment resets the existing user to the configured fallback.
The optional additional project's deletion guard must be deliberately disabled
and applied before that project can be destroyed. Removing an input after
deployment can also schedule destruction; inspect every plan.

## Verification scope

This combined file is checked with local `terraform fmt` and `terraform validate`;
it has not been applied as one complete live deployment. Individual resource
lifecycles and a smaller full-stack integration have live India Development
evidence, as detailed in the [service expansion guide](../../docs/guides/service_expansion.md).
Experimental project/role assignments retain their stated limitations. No cloud
changes or hosted CI checks are needed to validate the example.
