# Terraform Provider for Zoho Catalyst

A community provider built with the Terraform Plugin Framework. The long-term goal is to manage Catalyst services through Terraform wherever supported management APIs exist.

**Current status:** a working provider skeleton. Terraform can load its schema and accept an empty provider configuration. Authentication, API calls, resources, and data sources are not implemented yet. This provider is not published on the Terraform Registry and is not an official Zoho product.

## Requirements

- Go 1.25.8 or newer (see `go.mod`).
- Terraform 1.13 or newer for the local development workflow.
- Make and a POSIX shell.

## Build and verify locally

From the repository root:

```sh
go mod download
make test
make verify
```

`make verify` builds `bin/terraform-provider-catalyst`, creates an ignored `.terraformrc.local` pointing to that binary, retrieves the provider schema, validates the example, and runs a plan. Expect a development override warning and a successful **No changes** plan: the skeleton has nothing to manage yet. The retrieved schema is saved to `bin/provider-schema.json`.

The override applies only to the commands that use it; your global Terraform CLI configuration is not changed. No Zoho credentials are needed and no cloud resources are created.

To run Terraform manually after `make dev`:

```sh
export TF_CLI_CONFIG_FILE="$PWD/.terraformrc.local"
terraform -chdir=examples/provider providers schema -json
terraform -chdir=examples/provider validate
terraform -chdir=examples/provider plan
```

Skip `terraform init` for this standalone development example. The provider is not published, and `init` still attempts Registry version selection despite a development override. Run `unset TF_CLI_CONFIG_FILE` when finished using this override in your shell.

## Example configuration

```hcl
terraform {
  required_providers {
    catalyst = {
      source = "Edson-Edwin/catalyst"
    }
  }
}

provider "catalyst" {}
```

Terraform normalizes the provider address to `registry.terraform.io/edson-edwin/catalyst`, matching the server and local development override. This is an intended publishing address, not an existing Registry release.

## Project structure

- `main.go`: plugin entry point and provider address.
- `internal/provider/provider.go`: metadata, empty schema, and resource/data source registration.
- `internal/provider/provider_test.go`: protocol-level schema, validation, and configuration test.
- `examples/provider/`: runnable Terraform example.
- `tools/`: documentation generator; run `make generate` to refresh `docs/`.
- `scripts/setup-dev.sh`: writes the local development override.

## Next milestones

1. OAuth authentication, token refresh, and data-center configuration.
2. A project lookup data source backed by the Catalyst API.
3. The first supported managed resource with lifecycle and import support.
4. Broader service coverage, API tests, and acceptance tests against a dedicated Catalyst test account.
5. Signed releases and Terraform Registry publication. The inherited release workflow requires `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets before tagging a release.

Each service needs an API capability review before promising Terraform lifecycle support.

## License

MPL-2.0. Based on HashiCorp's Terraform Provider Scaffolding Framework; original copyright notices are retained.
