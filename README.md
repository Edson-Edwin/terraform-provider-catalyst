# Terraform Provider for Zoho Catalyst

A community Terraform provider intended for public use, built with the Terraform Plugin Framework. Each user supplies their own Zoho credentials and selects their account's data center. No personal account credentials or organization settings are built into the provider.

**Current status:** OAuth authentication and the read-only `catalyst_project` data source are implemented. Managed resources and Registry releases are not available yet. This is not an official Zoho provider. The long-term goal is broad Catalyst service coverage wherever supported management APIs exist.

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

`make test` runs the client and provider protocol tests against mock API responses, with Go's race detector. It does not contact Zoho. Tests cover token refresh and caching, concurrent reads, authorization retries, error handling, numeric ID precision, configuration precedence, and Terraform state mapping.

`make verify` builds `bin/terraform-provider-catalyst`, creates an ignored `.terraformrc.local`, reads the provider schema into `bin/provider-schema.json`, validates the provider-only example, and runs a no-op plan. This verifies local plugin loading without credentials or cloud resources. Expect a development override warning and **No changes**. It does not verify live authentication.

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

The provider exchanges refresh credentials only when an API read is needed, caches access tokens in memory for that provider instance, refreshes before expiry, and retries one HTTP 401 after refreshing. Independent Terraform processes have separate caches. It sends credentials in the token request body and does not follow redirects. HTTP 429 responses are reported so the user can wait before retrying.

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

## Opt-in live acceptance test

After configuring your own credentials and data center, run:

```sh
export CATALYST_TEST_PROJECT_ID="your-existing-numeric-project-id"
make testacc
```

This test starts the provider through Terraform's protocol, reads the project into temporary local state, checks the exact ID and a non-empty name, then checks a second plan for stability. Its configuration contains only a data source: it does not create, update, or delete remote Catalyst resources. The test harness may run multiple reads and OAuth exchanges. Supply only one authentication mode and allow for Zoho's token/rate limits.

`make test` explicitly disables live acceptance testing. GitHub's default checks also remain offline. Live tests require credentials supplied by the person running them; no account IDs or credentials are included in the tests. A signed-in Catalyst CLI can supply credentials for local development, but automatic CLI credential discovery is not part of the provider's public authentication interface.

## Development and publication

- `internal/catalyst/`: regional API client, token handling and HTTP tests.
- `internal/provider/`: Terraform schema, configuration, project data source and protocol tests.
- `examples/`: provider-only smoke test and live project lookup example.
- `make generate`: regenerates provider and data source documentation.
- `make test`, `make verify`, `go vet ./...`: local checks; GitHub Actions runs tests and verification.

Next milestones are a first managed resource with lifecycle/import support, and broader service coverage. Each service needs an API capability review before promising Terraform lifecycle support.

Public distribution requires a public source repository, a versioned release, signed provider checksums, and registration with the Terraform Registry. The release workflow requires `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets before tagging a release. Repository setup alone does not publish the provider.

## License

MPL-2.0. Based on HashiCorp's Terraform Provider Scaffolding Framework; original copyright notices are retained.
