# Terraform Provider for Zoho Catalyst

The Catalyst provider lets Terraform manage supported [Zoho Catalyst](https://catalyst.zoho.com/) application infrastructure. Each user supplies their own OAuth credentials and selects a data center and environment.

This is a community provider, not an official Zoho or HashiCorp product. It is under development and is **not yet available from the public Terraform Registry**. The current implementation registers **13 resource types and 12 data source types**; some capabilities are experimental or deprecated. These counts do not represent complete Catalyst service coverage.

- [Provider documentation and configuration](docs/index.md)
- [Resources and data sources by service](docs/index.md#supported-services)
- [All-services example](examples/all_services/) and [full-stack website example](examples/full_stack/)
- [Implementation status and verification evidence](API_COVERAGE.md)
- [Contributing](CONTRIBUTING.md), [release process](RELEASING.md) and [changelog](CHANGELOG.md)
- [Support](.github/SUPPORT.md) and [security reporting](SECURITY.md)

## Getting started

For local development, install Go 1.25.8 or newer, Terraform 1.13 or newer, Make and a POSIX shell. Application runtimes and build tools are needed only for the applications you deploy; the provider itself is written in Go.

Build the provider from this repository:

```sh
go mod download
make dev
export TF_CLI_CONFIG_FILE="$PWD/.terraformrc.local"
```

The local override points Terraform to `bin/terraform-provider-catalyst`. Skip `terraform init` for these standalone development examples: the provider is unpublished and initialization still attempts Registry version selection. This override applies only while `TF_CLI_CONFIG_FILE` selects it; unset that variable when finished.

Configure your own credentials, using placeholders below:

```sh
export CATALYST_DATA_CENTER="in"
export CATALYST_ENVIRONMENT="Development"
export CATALYST_CLIENT_ID="your-client-id"
export CATALYST_CLIENT_SECRET="your-client-secret"
export CATALYST_REFRESH_TOKEN="your-refresh-token"
unset CATALYST_ACCESS_TOKEN
```

Start with a read-only project lookup:

```sh
export TF_VAR_project_id="your-existing-numeric-project-id"
terraform -chdir=examples/data-sources/catalyst_project validate
terraform -chdir=examples/data-sources/catalyst_project plan
```

This requires an existing project and the `ZOHOCATALYST.projects.Read` scope. A Catalyst CLI login alone does not configure provider credentials. See [authentication and configuration](docs/index.md#authentication-and-configuration) for access-token mode, OAuth setup, environment-variable precedence and aliases.

Production now requires an explicit `data_center` or `CATALYST_DATA_CENTER`; set
`us` explicitly if you previously relied on the default. Optional organization
allow/deny lists and `validation_project_id` provide routing guardrails and a
read-only access preflight. OAuth refresh uses bounded backoff for transient HTTP
responses. See [organization guardrails and access preflight](docs/index.md#organization-guardrails-and-access-preflight).

Optional `allowed_data_centers` and `allowed_environments` policies restrict the
selected destination before network access. The guarded Production example uses
nonempty approved-region and organization inputs. Standard transports enforce
verified TLS 1.2 or newer and block redirects; see [regional security policies](docs/index.md#regional-security-policies).

## Supported services

| Service | Current Terraform support |
| --- | --- |
| AppSail | Managed Node, Java SE and Python ZIP deployments, code/configuration updates, import and artifact drift |
| Functions | Node22/24 Basic I/O, Advanced I/O, Event, Cron and Job Function deployments |
| Stratus | Objects/metadata in existing unversioned buckets, optional multipart transfers and bucket discovery |
| NoSQL | Configuration items and existing table/index metadata |
| Data Store | Configuration rows and existing table/column metadata |
| API Gateway | Enable/disable and complete ruleset ownership; supported Function/client targets and authentication modes |
| Job Scheduling | Webhook, Job Function and AppSail schedules; existing pool discovery |
| Projects | Discovery; experimental project creation/deletion with deletion protection |
| Authentication | User access metadata; experimental existing-user role assignment |
| Cache | Existing segment discovery only |
| File Store | Deprecated file management and folder discovery for legacy projects |

Existing tables, schemas, buckets, job pools, roles and permissions are prerequisites. Containers, custom domains, Production promotion, Authentication setup and Connections provisioning are not implemented. Project creation is account-dependent and has no successful live lifecycle verification; user-role assignment and Gateway client routes also retain verification limits. See the [service expansion guide](docs/guides/service_expansion.md) for the precise boundaries.

File Store is deprecated. New storage examples use Stratus; legacy migrations and permission changes must be planned separately. See the [File Store resource](docs/resources/filestore_file.md) for the recorded vendor notice and limitations.

## Examples

The [all-services example](examples/all_services/main.tf) includes every registered resource and data source with optional inputs. Start with its [instructions](examples/all_services/README.md) and replace the active placeholders in `terraform.tfvars.example`. Schedules start disabled; experimental and deprecated features require separate opt-ins.

The [full-stack example](examples/full_stack/README.md) connects existing database/storage prerequisites to your own AppSail application. Terraform deploys built artifacts; application code must implement database/storage access and its own business logic. A successful apply alone does not establish application health.

## Contributing and development

Run offline checks without Catalyst credentials:

```sh
make check
make lint
make generate
```

The [contributor guide](CONTRIBUTING.md) explains documentation generation, resource lifecycle expectations and opt-in live tests. Local checks do not establish public release availability or feature parity with other providers. Recorded live evidence is limited to the regions and behaviors listed in [API_COVERAGE.md](API_COVERAGE.md). Hosted CI is currently blocked by account billing, as reported by the maintainer; local verification and pending live regional checks are recorded separately.

## License

[MPL-2.0](LICENSE).
