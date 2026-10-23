# Contributing

This is a community provider for Zoho Catalyst, built with the Terraform Plugin Framework. Use the framework for new resources. Keep changes focused on a supported Catalyst management API and a complete Terraform lifecycle.

## Local checks

Install Go 1.25.8 or newer, Terraform, Make, Python 3, golangci-lint v2.15.0, and GoReleaser v2.18.3. Default tests need no Catalyst account or credentials.

```sh
make check
make lint
make generate
make release-check
```

`make check` verifies dependency files, formatting, static checks, offline tests, and example schemas. `make generate` updates generated references; include those changes with the implementation. `make release-check` builds unsigned snapshots for all configured platforms, checks checksums and the Registry manifest, installs the native package using a local filesystem mirror, and starts it through Terraform. It rejects mixed release versions and checks the requested version when supplied. It does not publish or contact Catalyst.

Native Windows and macOS CI jobs run offline tests and builds; Linux runs the race detector and the Terraform version matrix. These workflow definitions still need successful hosted runs.

CI also checks that generation produces no tracked or untracked documentation changes. It covers the latest patches in the Terraform 1.13–1.16 series. Add newer series deliberately; do not assume compatibility without checking them.

## Resource changes

For a new resource, first establish supported create/read/update/delete APIs, scopes, regional behavior and import identity. A console feature or data-operation API alone is not enough. Use exact strings for IDs. Avoid API calls during provider configuration.

Test the lifecycle, stable plans, import, updates, clearing optional values, replacement, unknown values, external deletion, permission failures and partial failures. Preserve state on access errors. Retain a trusted ID when creation succeeds but response metadata is invalid. Do not repeat ambiguous write requests without an API-supported idempotency guarantee.

Keep transport and authentication in `internal/catalyst`. The current `internal/provider` layout is appropriate for the small supported surface. Introduce service packages or shared helpers when several implementations actually need them.

## Live tests

Follow the opt-in instructions in [README.md](README.md). Use a dedicated Development project and disabled test crons. The person running the test supplies credentials and checks cleanup. Never commit credentials, account-specific fixtures, saved plans or state. Do not enable live mutation tests in pull-request jobs.

Update [API_COVERAGE.md](API_COVERAGE.md) with what was actually verified, including region and limitations. A mocked API test is not evidence that a live endpoint supports an operation.

## Compatibility and review

Describe the user-visible problem, resulting behavior and validation in each pull request. Update [CHANGELOG.md](CHANGELOG.md). Keep state and schema changes backward-compatible where possible. When changing the stored schema of a released resource, add a schema version, migration and upgrade test. Do not add migrations for formats that have never been released.

Follow existing MPL-2.0 notices. Report sensitive vulnerabilities using [SECURITY.md](SECURITY.md).
