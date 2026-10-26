# Contributing

This is a community provider for Zoho Catalyst, built with the Terraform Plugin Framework. Use the framework for new resources. Keep changes focused on a supported Catalyst management API and a complete Terraform lifecycle.

## Local checks

Install Go 1.25.8 or newer, Terraform, Make, golangci-lint v2.15.0, and GoReleaser v2.18.3. Default tests need no Catalyst account or credentials.

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

Environment-scoped resource factories must use `withEnvironmentScope` and include
the `routing_scope` object in their Terraform model. The shared guard covers plans,
saved-plan applies, reads, mutations and imports. Project resources use their own
cross-environment guard. Extend `TestEnvironmentScopeGuardsAllResources` when adding
a resource; do not bypass its registration-count assertion. All managed-resource
fixtures must configure an explicit organization. Legacy state must be refreshed
with its original routing before mutation; see the
[environment migration guide](docs/guides/environments.md).

Test the lifecycle, stable plans, import, updates, clearing optional values, replacement, unknown values, external deletion, permission failures and partial failures. Preserve state on access errors. Retain a trusted ID when creation succeeds but response metadata is invalid. Do not repeat ambiguous write requests without an API-supported idempotency guarantee.

Keep transport and authentication in `internal/catalyst`. The current `internal/provider` layout is appropriate for the small supported surface. Introduce service packages or shared helpers when several implementations actually need them.

## Live tests

Use a dedicated Development project and disabled test crons. The person running the test supplies credentials and checks cleanup. Never commit credentials, account-specific fixtures, saved plans or state. Do not enable live mutation tests in pull-request jobs. Default `make test` sets `TF_ACC=0` and remains offline.

### Credentials and read-only tests

Read the [provider authentication reference](docs/index.md#authentication-and-configuration) before configuring credentials. A project lookup requires `ZOHOCATALYST.projects.Read`; each additional service needs its own scopes from its reference page. Most deployment fixtures use an access token directly, so use access-token mode for the mutation matrix below. Independent Terraform test processes otherwise refresh separately and can encounter OAuth rate limits.

```sh
export CATALYST_DATA_CENTER="in"
export CATALYST_ENVIRONMENT="Development"
export CATALYST_ACCESS_TOKEN="your-access-token"
unset CATALYST_CLIENT_ID CATALYST_CLIENT_SECRET CATALYST_REFRESH_TOKEN CATALYST_REDIRECT_URI
export CATALYST_TEST_PROJECT_ID="your-isolated-project-id"
make testacc
make testacc-discovery
```

These two targets read project/job-pool metadata and create no cloud objects. Pool discovery additionally needs `ZohoCatalyst.jobpool.READ`. `make testacc-inventory` reads all implemented inventories, including legacy File Store, and requires their respective scopes/service access. `make testacc-database-schema` additionally needs `CATALYST_TEST_DATASTORE_TABLE_ID` and `CATALYST_TEST_NOSQL_TABLE_ID` for existing schemas.

### Mutation test opt-ins

All managed-resource tests require `CATALYST_ORGANIZATION_ID` for the dedicated
fixture organization, in addition to explicit region and environment. Verify the
fixture belongs to that destination before enabling writes.

Each `make testacc-*` target sets `TF_ACC=1`; mutation tests also require the explicit opt-ins below. Set those flags to `1` only for the target you intend to run. Unless noted, tests generate their own payloads and verify cleanup. They do not prove other data centers or Production compatibility. The matrix describes test prerequisites, not an instruction to execute every live test.

| Make target | Additional opt-ins and prerequisites |
| --- | --- |
| `testacc-cron`, `testacc-webhook-cron-schedules` | `CATALYST_TEST_ALLOW_CRON_CREATE`, `CATALYST_TEST_JOB_POOL_ID` for an existing Webhook pool |
| `testacc-function`, `testacc-function-variants`, `testacc-function-background` | `CATALYST_TEST_ALLOW_FUNCTION_CREATE`; includes configuration-only updates and Node 22-to-24 upgrades |
| `testacc-function-execution` | Both `CATALYST_TEST_ALLOW_FUNCTION_CREATE` and `CATALYST_TEST_ALLOW_FUNCTION_INVOKE`; generated constant-output Basic I/O fixtures only; `ZohoCatalyst.functions.EXECUTE` plus deployment/read/delete scopes |
| `testacc-appsail`, `testacc-appsail-languages` | `CATALYST_TEST_ALLOW_APPSAIL_CREATE`; language fixtures also require a JDK with `javac --release 8` |
| `testacc-appsail-health` | Both `CATALYST_TEST_ALLOW_APPSAIL_CREATE` and `CATALYST_TEST_ALLOW_APPSAIL_INVOKE`; explicitly invokes generated constant-response applications |
| `testacc-function-cron`, `testacc-function-cron-schedules` | Both `CATALYST_TEST_ALLOW_FUNCTION_CREATE` and `CATALYST_TEST_ALLOW_FUNCTION_CRON_CREATE`; `CATALYST_TEST_FUNCTION_POOL_ID` for an existing Function pool |
| `testacc-appsail-cron` | Both `CATALYST_TEST_ALLOW_APPSAIL_CREATE` and `CATALYST_TEST_ALLOW_APPSAIL_CRON_CREATE`; `CATALYST_TEST_APPSAIL_POOL_ID` for an existing AppSail pool |
| `testacc-gateway` | `CATALYST_TEST_ALLOW_GATEWAY_CREATE`; an isolated project with disabled Gateway and no custom rules; creates generated Functions without invoking them |
| `testacc-stratus-object`, `testacc-stratus-multipart` | `CATALYST_TEST_ALLOW_STRATUS_OBJECT_CREATE`; `CATALYST_TEST_BUCKET` for an existing private authenticated unversioned bucket |
| `testacc-nosql-item` | `CATALYST_TEST_ALLOW_NOSQL_ITEM_CREATE`; `CATALYST_TEST_NOSQL_TABLE_ID` for the dedicated ONLINE `TerraformProviderNoSQLTests` table with string keys `pk` and `sk` |
| `testacc-datastore-row` | `CATALYST_TEST_ALLOW_DATASTORE_ROW_CREATE`; `CATALYST_TEST_DATASTORE_TABLE_ID` for `TerraformProviderDataStoreTests` with a nullable text column `message` |
| `testacc-full-stack` | `CATALYST_TEST_ALLOW_FULLSTACK_CREATE`, both database table IDs, `CATALYST_TEST_BUCKET`, and pinned application SDK fixture dependencies; see the [full-stack instructions](examples/full_stack/README.md#repeat-the-isolated-application-smoke-test) |
| `testacc-auth-user-role` | `CATALYST_TEST_ALLOW_AUTH_USER_ROLE_UPDATE`; a disposable existing user and two existing roles, detailed below |
| `testacc-file` | Deprecated: `CATALYST_TEST_ALLOW_FILE_CREATE`, `CATALYST_TEST_FOLDER_ID` for an existing legacy folder |

For example, to run only the Data Store configuration-row lifecycle after preparing its dedicated table:

```sh
export CATALYST_ORGANIZATION_ID="your-fixture-organization-id"
export CATALYST_TEST_ALLOW_DATASTORE_ROW_CREATE=1
export CATALYST_TEST_DATASTORE_TABLE_ID="your-dedicated-table-id"
make testacc-datastore-row
```

The row test needs `ZohoCatalyst.tables.READ`, `ZohoCatalyst.tables.columns.READ`, and each of `ZohoCatalyst.tables.rows.CREATE`, `.READ`, `.UPDATE` and `.DELETE` as individual scopes. It creates configuration rows, not schema. Stratus tests create objects, not buckets. Scheduling tests create disabled schedules, not job pools. These tests do not constitute complete service provisioning.

### Function runtime verification

The normal Function Terraform lifecycle never invokes handlers. To check actual
execution, use the separate client/API gate in a dedicated Development project:

```sh
# Inject either an access token OR complete OAuth refresh credentials securely.
export CATALYST_DATA_CENTER="in"
export CATALYST_ENVIRONMENT="Development"
export CATALYST_ORGANIZATION_ID="your-fixture-organization-id"
export CATALYST_TEST_PROJECT_ID="your-dedicated-development-project-id"
export CATALYST_TEST_ALLOW_FUNCTION_CREATE=1
export CATALYST_TEST_ALLOW_FUNCTION_INVOKE=1
make testacc-function-execution
```

Use `ZohoCatalyst.functions.ALL` and project read access, or separately provisioned
permissions for creation, configuration, artifact reads, execution and deletion.
The gate fails when opt-ins or explicit routing are missing. It supports either
access-token or OAuth-refresh mode; a successful run verifies only the selected
mode and region. Never put credentials in command arguments, logs or Git.

The gate creates two isolated Basic I/O fixtures, checks three matching outputs
per phase around configuration/code updates and redeployment of the original artifact,
and restores the original Function inventory. Cleanup uses bounded independent
context after failure or cancellation;
only a returned new ID with matching name/runtime/type may be deleted. A process
killed before cleanup or an ambiguous creation without an ID requires manual
inspection using the logged generated name. Do not clean up unrelated objects.

The Terraform Function matrices also attempt bounded fallback cleanup with a fresh
client after failure, but only for a recorded new ID whose generated name/type still
match and which was absent from the starting inventory. A successful recovery does
not turn the failed test into a pass. If the first apply fails before its identity
check records the ID, inspect Terraform's retained state and establish ownership
before using the explicit fixture recovery gate.

Development execution does not establish Production readiness or background
trigger delivery. Dedicated Production fixtures and a verified promotion contract
are still required. See [Function management](docs/guides/function_management.md)
and [recorded evidence](API_COVERAGE.md).

### Experimental project management

`make testacc-project` exercises real Terraform creation, default deletion
protection, a rejected protected destroy, import that re-enables protection,
another rejected destroy, refresh, an explicit guard update, a stable plan and
final deletion. It creates a new empty project with a random name and never
uses an existing project as a disposable fixture. The same steps run against the
offline test server on every normal test run. Use `make test-project` for the
focused offline project suite.

Prerequisites:

1. An account and organization eligible to create projects through the REST API.
   The tested India OAuth context returned `PROJECT_CREATION_NOT_ALLOWED` with
   a CLI-version message; the official CLI now uses console-assisted creation.
   Confirm a supported non-interactive route with Catalyst support before retrying;
   see [the creation diagnosis](docs/guides/project_management.md#unblock-live-project-creation).
   A console-created project does not establish API creation eligibility.
2. OAuth credentials with `ZOHOCATALYST.projects.CREATE`,
   `ZOHOCATALYST.projects.Read` and `ZOHOCATALYST.projects.Delete`. Follow the
   [official OAuth setup](https://docs.catalyst.zoho.com/en/api/oauth2/register-new-client/)
   and the [project creation contract](https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/create-new-project/).
   Scopes alone do not prove account eligibility. Inject either `CATALYST_ACCESS_TOKEN`
   or all three of `CATALYST_CLIENT_ID`, `CATALYST_CLIENT_SECRET` and
   `CATALYST_REFRESH_TOKEN` through your local secret manager or protected environment.
   Never put credentials in Terraform examples, shell command arguments, chat or Git.
3. Explicit routing and a disposable-write opt-in:

```sh
export CATALYST_DATA_CENTER=in # use the eligible account's region
export CATALYST_ENVIRONMENT=Development
export CATALYST_ORGANIZATION_ID="your-organization-id"
export CATALYST_TEST_ALLOW_PROJECT_CREATE=1
make testacc-project
```

The gate fails on missing configuration instead of counting a skipped live test
as a pass. It accepts any explicitly selected supported region, and only
Development routing. Each run verifies the selected authentication mode; rerun
with the other mode independently to establish both. Credentials must not be
written to debug logs; leave Terraform debug logging disabled.

The harness records only generated names and IDs. It captures partial-create
Terraform state and attempts bounded cleanup even after a test failure, checking
the exact ID and name before deletion. It never cleans up by name alone or removes
a pre-existing ID from the initial inventory. A killed process or creation response
without an ID needs manual inspection using the generated name; review the
project before importing or deleting it. Do not rerun ambiguous creation blindly.

Successful offline checks are separate from a successful live gate. Until an
eligible account passes the gate and cleanup, keep the resource experimental.
Hosted CI is currently blocked by billing; this local target does not depend on it.

The saved India CLI login was subsequently used in memory on 2026-10-24 to mint a
fresh access token and select its sole visible organization. The full Terraform
gate reached creation and received HTTP 400 `PROJECT_CREATION_NOT_ALLOWED` again.
An independent before/after project ID inventory comparison was unchanged. No
credential values were printed or written to the repository. Authentication works
in that context. A subsequent unique-name direct request received a message that
creation was unsupported in that CLI version; the actual refreshed grant included
`ZohoCatalyst.projects.ALL`. CLI 1.20.0 introduced console-assisted creation, and
the inspected CLI 1.28.0 follows that flow. A supported non-interactive route for
this OAuth context needs vendor clarification; a CLI upgrade alone does not change
Terraform requests. See [the recorded diagnosis](API_COVERAGE.md).

Project safety now deliberately differs from ordinary automatic disappearance
handling: an invisible project retains state and stops the run, even after three
empty listings. An explicit organization is required; saved organization/data-center
bindings reject changed provider routing. Legacy state must be refreshed in its
original scope before mutation. Tests must assert no recreation on visibility loss.
See [Project ownership and recovery](docs/guides/project_management.md) for shared
state locking, migration and independently confirmed external deletion.

### Experimental user-role assignment

No disposable application user was available for the recorded milestone, so this resource remains live-unverified. To verify it later, prepare an existing disposable end-user and two different existing roles in the isolated project. The user must initially belong to the fallback role. Add `ZohoCatalyst.projects.users.READ` and `.UPDATE` and set:

```sh
export CATALYST_TEST_ALLOW_AUTH_USER_ROLE_UPDATE=1
export CATALYST_TEST_AUTH_USER_ID="existing-disposable-app-user-id"
export CATALYST_TEST_AUTH_ROLE_ID="existing-test-role-id"
export CATALYST_TEST_AUTH_FALLBACK_ROLE_ID="initial-and-destroy-fallback-role-id"
make testacc-auth-user-role
```

The test changes the role, imports, updates, checks a stable plan and verifies the fallback after destroy. It does not invite users or send password-reset messages. If interrupted, inspect the supplied user and restore its fallback role before reusing it. Login setup, role provisioning, permissions and Connections provisioning remain outside this resource.

### Cleanup and evidence

Confirm generated resource deletion and restoration of the initial project inventory. Review failed or interrupted tests before rerunning: never blindly replay ambiguous creates. Keep fixture identity and recovery evidence locally without disclosing credentials or user profiles. The provider's opt-in test gates do not automatically authorize tests against customer data or Production.

Update [API_COVERAGE.md](API_COVERAGE.md) with what was actually verified, including region and limitations. A mocked API test is not evidence that a live endpoint supports an operation.

## Public regional TLS checks

`make testacc-regional-tls` checks certificate chains, hostnames and TLS protocol
negotiation for every configured API and OAuth host. It requires no account
credentials and sends no HTTP requests. The target enables both `TF_ACC=1` and
`CATALYST_TEST_REGIONAL_TLS=1`; ordinary offline tests skip it.

The probe uses direct TCP/TLS connections, the system trust store and the
provider's TLS minimum, with a ten-second timeout per host. It does not exercise
HTTP proxy routing, authenticated API access, Stratus transfers or service
permissions. Record those verification boundaries with any results.

## Regional authentication smoke tests

`make testacc-regional-auth` only reads existing projects. Select regions explicitly
with `CATALYST_TEST_REGIONS` (comma-separated `us,eu,in,au,ca,jp,sa`), and supply
independent credentials and an existing project for every selected region. Global
provider credentials are deliberately not inherited by this test.

For each selected region, replace `IN` in these variable names with its uppercase
code:

| Variable | Purpose |
| --- | --- |
| `CATALYST_TEST_IN_PROJECT_ID` | Required existing project readable with `ZOHOCATALYST.projects.Read` |
| `CATALYST_TEST_IN_ACCESS_TOKEN` | Optional access-token test mode |
| `CATALYST_TEST_IN_CLIENT_ID` | Refresh mode OAuth client ID |
| `CATALYST_TEST_IN_CLIENT_SECRET` | Refresh mode client secret |
| `CATALYST_TEST_IN_REFRESH_TOKEN` | Refresh mode token |
| `CATALYST_TEST_IN_REDIRECT_URI` | Optional refresh-mode registered redirect URI |
| `CATALYST_TEST_IN_ORGANIZATION_ID` | Optional explicit organization routing header |
| `CATALYST_TEST_IN_ENVIRONMENT` | `Development` (default) or `Production`; tests only read |

At least one authentication mode is required per selected region. Supplying an
access token and complete refresh credentials runs both modes separately. Partial
refresh credentials fail before any network request. Each mode reads the project
twice to exercise initial authentication and cached-token reuse. Refresh mode
issues an access token, but the test never creates, updates or deletes application
resources. Keep secrets in environment injection, outside source control.

```sh
# Inject the matching CATALYST_TEST_IN_* values before running this command.
CATALYST_TEST_REGIONS=in make testacc-regional-auth
```

The normal `make test` suite remains offline and skips this test. Its mocked
regional tests exercise API/OAuth requests, organization/environment headers,
401 renewal and cached reads in all seven regions. Record live results separately
in `API_COVERAGE.md`; the test harness itself is not cross-region live evidence.

Use `make testacc-regional-auth-all` for the complete regional authentication gate.
It derives every supported region from the same metadata used by API, OAuth and
Stratus routing, and requires both access-token and complete refresh credentials
for each one. All fixtures are checked before the first network request. A missing
region or authentication mode fails the gate rather than being silently skipped.
The existing subset target remains available for incremental verification.

Record the run date, region, actual selected environment, authentication modes and
outcome in `API_COVERAGE.md` only after each live run succeeds. Existing-project
reads prove authentication and routing access to that project. They do not prove
caller identity, every service permission, Production deployment readiness or
resource lifecycle compatibility across those regions.

### Both environments and Stratus

Set `CATALYST_TEST_ENVIRONMENTS=all` for independent Development and Production
fixtures. In this mode, insert the environment into every fixture name, for
example `CATALYST_TEST_IN_DEVELOPMENT_PROJECT_ID` and
`CATALYST_TEST_IN_PRODUCTION_PROJECT_ID`. Credentials and fixtures never fall back
to another environment or the legacy names above. Comma-separated explicit
environment selections are also supported.

Stratus checks need these additional values under the same regional/environment
prefix:

| Suffix | Purpose |
| --- | --- |
| `STRATUS_BUCKET` | Existing, dedicated, unversioned test bucket |
| `STRATUS_OBJECT_KEY` | Existing small non-sensitive binary fixture; retained by every test |
| `STRATUS_SHA256` | Independently computed lowercase SHA-256 of that fixture |
| `ALLOW_STRATUS_WRITES` | Exactly `1` to authorize disposable keys in this fixture bucket |

Use credentials with project-read and the documented Stratus bucket/object read
and temporary-signature permissions. Write runs additionally need object-create
and exact-key deletion permissions. Verify the needed scopes for your account;
project access alone does not establish Stratus permissions. No bucket is created,
reconfigured or deleted by these tests. Do not use customer data as a fixture.

```sh
# Reads only; all 7 DCs x 2 environments x 2 authentication modes are required.
make testacc-regional-all

# Optional subset, using environment-specific fixture names and both modes.
CATALYST_TEST_REGIONS=in CATALYST_TEST_ENVIRONMENTS=Development \
  CATALYST_TEST_REQUIRE_BOTH_AUTH_MODES=1 make testacc-regional-stratus

# Dedicated test buckets only. Each fixture also requires ALLOW_STRATUS_WRITES=1.
CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES=1 \
  make testacc-regional-stratus-lifecycle-all
```

`testacc-regional-all` checks project identity twice per mode, then bucket metadata
and signed downloads against the known fixture SHA-256. It includes all 28
region/environment/authentication combinations. Each check validates its complete
selected fixture matrix before making requests; missing fixtures fail the check.
The original `testacc-regional-auth-all` remains the seven-region authentication
gate using one explicitly selected or default Development environment per region.

The lifecycle gate first verifies the existing marker fixture. It generates a
128-bit random key under `tfacc-regional/`, proves absence, creates a small binary
payload bound to that key using `overwrite=false`, and requires three consistent
content/size/type observations. Cleanup verifies the unique payload before deleting
that key and confirms absence; unverifiable ownership retains the object with an
error. Use dedicated buckets without concurrent writers. Cleanup has an
independent bounded context, runs after ambiguous uploads and cancellation, and
reports failures. Uploads are never blindly replayed. Keep the generated key from
test output for exact-key recovery if the process is killed; never delete the
existing marker or sweep arbitrary objects. Interrupted runs require inspection
before rerunning. These checks do not cover multipart transfer or metadata updates.

### Protected regional CI

The manually dispatched [Regional verification workflow](.github/workflows/regional.yml)
runs only default-branch code, never pull-request code. Its 14 jobs cover every
region/environment pair; each job requires both authentication modes. Read-only
is the default mode. Mutation mode also requires the fixture's write permission.
Jobs have read-only repository permissions, pinned actions, no persisted checkout
credentials, no uploaded fixtures/log artifacts and no credential cache. Failed
jobs do not cancel other jobs or interrupt their cleanup; separate runs serialize.

Before dispatching, create all 14 GitHub environments named
`catalyst-regional-<dc>-<environment>`, for example
`catalyst-regional-in-Development` and `catalyst-regional-in-Production`. Set
required reviewers, prevent self-review, and restrict deployment branches to the
default branch. Store the following as **environment secrets**, independently for
each environment; do not put shared test credentials in repository-wide secrets:

`CATALYST_PROJECT_ID`, `CATALYST_ACCESS_TOKEN`, `CATALYST_CLIENT_ID`,
`CATALYST_CLIENT_SECRET`, `CATALYST_REFRESH_TOKEN`, `CATALYST_STRATUS_BUCKET`,
`CATALYST_STRATUS_OBJECT_KEY`, `CATALYST_STRATUS_SHA256`; optionally
`CATALYST_ORGANIZATION_ID` and `CATALYST_REDIRECT_URI`. Add
`CATALYST_ALLOW_STRATUS_WRITES=1` only for dedicated buckets approved for mutation
tests. Access tokens must be fresh at dispatch; the access-token mode deliberately
does not borrow refreshed credentials from the other mode.

The CI launcher maps only that environment's named secrets into its selected
fixture prefix. It strips inherited Catalyst fixtures and Terraform debug-log
settings, rejects newline/control-character injection, and propagates failures.
It never prints credentials or signed URLs. Missing secrets fail the tests.
Normal PR CI runs the launcher's offline tests without account secrets.

The workflow file does not provision credentials, project/bucket fixtures, GitHub
environment protections or a hosted run. A complete passing matrix is required
before claiming verified all-region support. Record region, environment, auth
mode, run date/commit and lifecycle cleanup outcome in `API_COVERAGE.md` after
execution; do not treat an unrun harness as evidence.

## Documentation

Follow the documentation pattern used by established Terraform providers: a short repository README, a provider configuration reference, service-grouped resource/data source pages, runnable examples, argument/attribute references and exact import commands. Keep user instructions separate from contributor tests and historical investigation notes.

This repository uses the Plugin Framework documentation layout. Do not duplicate the AWS provider's legacy `website/docs` tree or add a separate website build system.

| Content | Maintained source | Generated output |
| --- | --- | --- |
| Repository introduction | `README.md` | None |
| Provider configuration | `templates/index.md.tmpl` and provider schema descriptions | `docs/index.md` |
| Resource pages | `templates/resources.md.tmpl`, resource schema descriptions, `examples/resources/*` | `docs/resources/*.md` |
| Data source pages | `templates/data-sources.md.tmpl`, data source schema descriptions, `examples/data-sources/*` | `docs/data-sources/*.md` |
| Long-form service guides | `templates/guides/*.md.tmpl` | `docs/guides/*.md` |
| Combined runnable configurations | `examples/*` | None |
| Verification history | `API_COVERAGE.md` | None |

Edit templates, schema descriptions or example sources rather than generated pages. The shared templates assign service subcategories and keep arguments and exported attributes derived from the schema. Add new type names to their category mappings when introducing a service. Every resource/data source needs an example; importable resources also need a matching import example.

Use `terraform` fences for Terraform configuration and `shell` fences for commands. Show required prerequisites, defaults, allowed values, replacement behavior, exclusive ownership and sensitive-state implications. Mark experimental, deprecated and offline-only features explicitly. Keep simple examples on reference pages and combined configurations under `examples/`. Document only supported arguments; a platform feature is not automatically a provider feature.

Run from the repository root:

```sh
make generate
make verify
```

Then validate generated documentation against a freshly built local schema:

```sh
cd tools
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs validate \
  --provider-dir .. --provider-name catalyst
```

Review generated changes, relative links, service categories and import IDs. Re-running generation must produce identical output. Documentation generation uses the existing Go tooling; no additional documentation runtime is required. Hosted CI execution is currently deferred by the maintainer.

## Compatibility and review

Describe the user-visible problem, resulting behavior and validation in each pull request. Update [CHANGELOG.md](CHANGELOG.md). Keep state and schema changes backward-compatible where possible. When changing the stored schema of a released resource, add a schema version, migration and upgrade test. Do not add migrations for formats that have never been released.

Follow existing MPL-2.0 notices. Report sensitive vulnerabilities using [SECURITY.md](SECURITY.md).
