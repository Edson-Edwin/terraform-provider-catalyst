# Terraform Provider for Zoho Catalyst

A community Terraform provider intended for public use, built with the Terraform Plugin Framework. Each user supplies their own Zoho credentials and selects their account's data center. No personal account credentials or organization settings are built into the provider.

**Current status:** OAuth authentication and the read-only `catalyst_project`, `catalyst_projects`, and `catalyst_job_pools` data sources are implemented. The `catalyst_webhook_cron` managed resource supports create/read/update/delete and import. Registry releases are not available yet. This is not an official Zoho provider. The long-term goal is broad Catalyst service coverage wherever supported management APIs exist.

## Requirements

- Go 1.25.8 or newer (see `go.mod`).
- Terraform 1.13 or newer for local development.
- Make and a POSIX shell.
- For live project reads: a Catalyst account, an existing project, and OAuth credentials with the `ZOHOCATALYST.projects.Read` scope.

## Build and test without credentials

From the repository root:

```sh
go mod download
make test
make verify
```

`make test` runs the client and provider protocol tests against mock API responses, with Go's race detector. It does not contact Zoho. The cron lifecycle test runs a real Terraform binary against a local fake API, so Terraform must be on PATH. Tests cover token refresh and caching, concurrent reads, authorization retries, error handling, numeric ID precision, configuration precedence, Terraform state mapping, cron lifecycle/import, clearing optional fields, enable/disable changes, and recreation after external deletion.

`make verify` builds `bin/terraform-provider-catalyst`, creates an ignored `.terraformrc.local`, reads the provider schema into `bin/provider-schema.json`, validates the provider, data source, and resource examples, and runs a no-op plan. This verifies local plugin loading without credentials or cloud resources. Expect a development override warning and **No changes**. It does not verify live authentication.

The override only applies to commands that select it; your global Terraform configuration is unchanged. Skip `terraform init` for these standalone local examples: the provider is not published, and `init` still attempts Registry version selection despite the override.

## Authentication

Choose one of the following modes. Explicit provider attributes take precedence over their environment variables. A null or omitted attribute falls back to the environment; an explicit empty string suppresses that fallback. Unknown provider values are rejected at configuration time.

### OAuth refresh credentials (recommended)

Each user registers an OAuth client in the [Zoho API Console](https://api-console.zoho.com/) for their account's data center. A Self Client is suitable for personal/local use. Obtain a grant code with `ZOHOCATALYST.projects.Read`, exchange it for access and refresh tokens, and save the refresh token securely. Interactive/server applications must complete Zoho's consent flow with offline access. The provider consumes existing credentials; it does not run the browser consent or grant-code exchange flow.

Follow Zoho's [client registration](https://docs.catalyst.zoho.com/en/api/oauth2/register-new-client/), [grant token](https://docs.catalyst.zoho.com/en/api/oauth2/generate-grant-token/) and [token exchange](https://docs.catalyst.zoho.com/en/api/oauth2/generate-access-and-refresh-token/) instructions.

Set these values locally (the values below are placeholders):

```sh
export CATALYST_DATA_CENTER="in" # example; choose your account's region
export CATALYST_CLIENT_ID="your-client-id"
export CATALYST_CLIENT_SECRET="your-client-secret"
export CATALYST_REFRESH_TOKEN="your-refresh-token"
unset CATALYST_ACCESS_TOKEN
```

For an OAuth client that uses a registered redirect URI, also set `CATALYST_REDIRECT_URI` to that URI. Omit it for a Self Client. Zoho's generic [refresh-token API](https://www.zoho.com/developer/oauth/web-server-apps/refresh-access-token.html) does not require it; the provider sends it when configured.

The provider exchanges refresh credentials only when an API request is needed, caches access tokens in memory for that provider instance, refreshes before expiry, and retries one HTTP 401 after refreshing. Independent Terraform processes have separate caches. It sends credentials in the token request body and does not follow redirects. HTTP 429 responses are reported so the user can wait before retrying.

### Existing access token

For a short-lived session, supply an access token instead:

```sh
export CATALYST_DATA_CENTER="in" # example; choose your account's region
export CATALYST_ACCESS_TOKEN="your-access-token"
unset CATALYST_CLIENT_ID CATALYST_CLIENT_SECRET CATALYST_REFRESH_TOKEN CATALYST_REDIRECT_URI
```

Access-token mode does not refresh automatically. Replace the token when it expires. Mixed authentication modes or incomplete refresh credentials produce a configuration error. Keep credentials out of version control; `.env` and `.tfvars` files are ignored, but the provider does not automatically load `.env` files.

## Regions, environments and organizations

`CATALYST_DATA_CENTER` accepts `us`, `eu`, `in`, `au`, `ca`, `jp`, or `sa`, defaulting to `us`. Select the region where your Zoho account is hosted, not your current physical location. The provider uses the [regional Catalyst API endpoints](https://docs.catalyst.zoho.com/en/api/introduction/multi-dc-support/) and [canonical OAuth accounts hosts](https://accounts.zoho.com/oauth/serverinfo). In particular, the Australian OAuth host is `accounts.zoho.com.au`.

`CATALYST_ENVIRONMENT` accepts `Development` (default) or `Production`. `CATALYST_ORGANIZATION_ID` optionally selects an organization through the `CATALYST-ORG` request header.

Provider aliases allow multiple independent configurations:

```hcl
provider "catalyst" {
  alias           = "development"
  data_center     = "in"
  environment     = "Development"
  organization_id = "123456789"
}
```

Credentials still come from the environment unless explicitly configured. Use sensitive Terraform input variables for aliases needing different credentials; the environment is shared by the Terraform process.

## Read an existing project

After setting credentials, run from the repository root:

```sh
make dev
export TF_CLI_CONFIG_FILE="$PWD/.terraformrc.local"
export TF_VAR_project_id="your-existing-numeric-project-id"
terraform -chdir=examples/data-sources/catalyst_project validate
terraform -chdir=examples/data-sources/catalyst_project plan
```

The example performs a read-only request to the [Catalyst project API](https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/get-specific-project/). It creates or changes no remote resources. A successful plan shows the project name and domain outputs. Live access-token and refresh-token project lookups have been verified in the India data center. Other accounts and regions should be verified using the opt-in acceptance test below. Default automated tests use mock responses.

```hcl
terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

provider "catalyst" {}

data "catalyst_project" "example" {
  id = "12345678901234567"
}

output "project_name" {
  value = data.catalyst_project.example.name
}
```

The data source also exposes `project_type`, `domain`, `domain_name`, and `timezone`. IDs remain strings to preserve precision. Optional metadata absent from the API is null. A missing or inaccessible project reports an error.

Run `unset TF_CLI_CONFIG_FILE` when finished using the development override. The intended Registry address is `Edson-Edwin/catalyst`; it is not currently installable from the public Registry.

## Discover projects and job pools

`catalyst_projects` lists projects visible to your credentials and selected organization. It requires `ZOHOCATALYST.projects.Read`. `catalyst_job_pools` lists an existing project's scheduling pools and additionally requires `ZohoCatalyst.jobpool.READ`.

```hcl
data "catalyst_projects" "available" {}

data "catalyst_job_pools" "available" {
  project_id = "12345678901234567"
}

output "projects" {
  value = data.catalyst_projects.available.projects
}

output "job_pools" {
  value = data.catalyst_job_pools.available.pools
}
```

Both lists use a stable lexicographic ID order, preserve numeric IDs as strings, and return `[]` when no items exist. An API error or malformed response produces an error, not an empty list. Project results contain the same metadata as `catalyst_project`; pool results contain `id`, `name`, and `type`. No creator profiles, emails, or credentials are stored in these results.

Standalone examples are in `examples/data-sources/catalyst_projects` and `examples/data-sources/catalyst_job_pools`. With the development override enabled, use `terraform -chdir=examples/data-sources/catalyst_projects plan` to discover IDs. The job-pool example takes `TF_VAR_project_id`.

Project listing and both empty and populated pool listings have been verified live in India. See [API coverage and implementation plan](API_COVERAGE.md) for the management API restrictions found during validation.

## Manage a webhook cron

The first managed resource, `catalyst_webhook_cron`, manages **pre-defined CronExpression schedules with Webhook targets**. Calendar schedules, dynamic crons, Function/Circuit/AppSail targets, and job-pool creation are not part of this resource. It requires an existing project and a Webhook job pool in the selected environment. Use the console to create these prerequisites, then use the discovery data sources to find their IDs.

Obtain OAuth credentials with `ZohoCatalyst.cron.CREATE`, `ZohoCatalyst.cron.READ`, `ZohoCatalyst.cron.UPDATE`, and `ZohoCatalyst.cron.DELETE`. Also include `ZOHOCATALYST.projects.Read`: the provider checks parent access before treating a missing cron as deleted. Add `ZohoCatalyst.jobpool.READ` when using pool discovery.

```hcl
resource "catalyst_webhook_cron" "example" {
  project_id  = var.project_id
  job_pool_id = var.job_pool_id
  name        = "terraform_example"
  job_name    = "terraform_example"
  expression  = "0 9 * * 1-5"
  timezone    = "Asia/Kolkata"
  end_time    = var.end_time # future Unix timestamp in seconds
  enabled     = false
  url         = "https://example.com/your-webhook"
}
```

Crons default to **disabled**. Set `enabled = true` deliberately after configuring your real endpoint and schedule; enabled crons submit real HTTP jobs and consume Catalyst usage. The provider does not invoke a webhook itself. `job_name` is limited to 20 characters. With retries enabled, `retry_interval` must be 60–86400 seconds; with retries disabled it is omitted from requests and reported as zero in state.

The [resource reference](docs/resources/webhook_cron.md) covers headers, parameters, body, retries, and import. These fields and the URL are sensitive in Terraform output, but their values remain in state. Secure your state backend and do not commit state files. Changing `project_id` replaces the cron; other supported fields update in place.

```sh
terraform import catalyst_webhook_cron.example 'PROJECT_ID/CRON_ID'
```

Import requires the matching data center, organization, and environment and a resource configuration that describes the existing cron. Import does not change the cron. An API access error preserves state; a confirmed missing cron is removed from state so Terraform can recreate it. If a create request succeeds remotely but its response is lost, inspect the console and import the cron before retrying. The client does not retry ambiguous transport or server failures for writes.

A complete disabled example is in `examples/resources/catalyst_webhook_cron`. Its `project_id`, `job_pool_id`, and `end_time` variables must be set before planning. `make verify` validates its syntax without applying it.

Create, import, rename/update, clearing optional fields, a stable plan, and destroy with confirmed cleanup have been tested against the India API. Live tests keep all crons disabled; actual webhook delivery and other regions have not been tested.

## Opt-in live acceptance test

After configuring your own credentials and data center, run:

```sh
export CATALYST_TEST_PROJECT_ID="your-existing-numeric-project-id"
make testacc
```

This test starts the provider through Terraform's protocol, reads the project into temporary local state, checks the exact ID and a non-empty name, then checks a second plan for stability. Its configuration contains only a data source: it does not create, update, or delete remote Catalyst resources. The test harness may run multiple reads and OAuth exchanges. Supply only one authentication mode and allow for Zoho's token/rate limits.

For project and job-pool discovery, add the `ZohoCatalyst.jobpool.READ` scope to your credentials and run `make testacc-discovery` with the same `CATALYST_TEST_PROJECT_ID`. This read-only test checks that the selected project appears in the project list, checks that the pool list is known (empty is valid), and verifies a stable second plan.

`make test` explicitly disables live acceptance testing. GitHub's default checks also remain offline. Live tests require credentials supplied by the person running them; no account IDs or credentials are included in the tests. A signed-in Catalyst CLI can supply credentials for local development, but automatic CLI credential discovery is not part of the provider's public authentication interface.

## Opt-in live cron lifecycle test

Use a dedicated Development project and an existing Webhook pool. With the OAuth scopes above and credentials configured locally:

```sh
export CATALYST_ENVIRONMENT="Development"
export CATALYST_TEST_PROJECT_ID="your-test-project-id"
export CATALYST_TEST_JOB_POOL_ID="your-test-webhook-pool-id"
export CATALYST_TEST_ALLOW_CRON_CREATE="1"
make testacc-cron
```

This test creates a uniquely named disabled cron, imports it, updates and clears fields, checks a stable plan, and destroys it. It verifies deletion through the API and never submits jobs. The explicit creation flag is required; `make test` disables live tests. Interrupted runs may leave a disabled cron with a `tfacc` prefix in the test project; inspect it before cleanup. The project and pool are reusable prerequisites and are not destroyed by this test.

## Development and publication

- `internal/catalyst/`: regional API client, token handling and HTTP tests.
- `internal/provider/`: Terraform schema, configuration, discovery data sources, webhook cron lifecycle and protocol tests.
- `examples/`: provider-only smoke test, discovery examples, and a disabled webhook cron example.
- `make generate`: regenerates provider and data source documentation.
- `make test`, `make verify`, `go vet ./...`: local checks; GitHub Actions runs tests and verification.

Next milestones are broader cron target support, additional service coverage, and a public release. The [API coverage plan](API_COVERAGE.md) records verified lifecycle behavior and API restrictions. Each service needs an API capability review before promising Terraform lifecycle support.

Public distribution requires a public source repository, a versioned release, signed provider checksums, and registration with the Terraform Registry. The release workflow requires `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets before tagging a release. Repository setup alone does not publish the provider.

## License

MPL-2.0. Based on HashiCorp's Terraform Provider Scaffolding Framework; original copyright notices are retained.
