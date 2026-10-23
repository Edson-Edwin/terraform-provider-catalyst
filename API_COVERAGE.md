# Catalyst API coverage and implementation plan

This community provider is under development. Public availability and broad service coverage remain goals; implemented capabilities are listed below. API availability must be verified separately from the Terraform schema and mock tests.

## Implemented and verified

| Terraform type | Capability | Verification |
| --- | --- | --- |
| `data.catalyst_project` | Read a project by ID | Offline protocol/client tests; live India access-token and refresh-token reads |
| `data.catalyst_projects` | List projects visible in the selected organization | Offline protocol/client tests; live India listing and stable Terraform plan |
| `data.catalyst_job_pools` | List a project's existing scheduling pools | Offline populated/empty/error cases; live India empty and populated listings with stable plans |
| `catalyst_webhook_cron` | Manage pre-defined CronExpression schedules with Webhook targets | Offline lifecycle/import/drift/error tests; live India create, import, update, clear, stable plan and verified destroy |
| `catalyst_api_gateway` | Enable/disable project Gateway routing | Offline Terraform lifecycle and recovery tests; live India Development enable/import/destroy |
| `catalyst_api_gateway_ruleset` | Complete Basic I/O ruleset replacement, OAuth/no-auth, import and drift | Offline lifecycle/policy preservation; live India Development create/import/replace/clear/stable plan/destroy |
| `catalyst_appsail` | Manage Node.js 22 ZIP deployment, memory, command, port, authentication and environment maps; import and artifact drift | Offline lifecycle, safety and recovery tests; live results recorded below |
| `catalyst_function` | Manage Node.js 22/24 Basic I/O and Advanced I/O code, memory and environment variables; import and drift | Offline lifecycle/safety regressions; live India Development create/import/update/clear/stable plan/destroy with baseline cleanup; never invoked |
| `data.catalyst_functions` | List serverless Function IDs and names | Offline contracts and populated live Terraform discovery in replacement India Development project; never invokes Functions |
| `data.catalyst_datastore_tables` | List existing Data Store table IDs and names | Offline API/protocol/real Terraform tests; live India empty listing and stable plan |
| `data.catalyst_filestore_folders` | List existing File Store folder IDs and names | Offline API/protocol/real Terraform tests; live India populated listing and stable plan |
| `data.catalyst_cache_segments` | List existing Cache segments | Official CLI read endpoint; offline tests and live India default segment with stable plan |
| `data.catalyst_stratus_buckets` | List existing Stratus bucket names and endpoints | Offline tests, live India empty listing and stable plan; populated live buckets untested |
| `catalyst_stratus_object` | Manage unversioned object bytes, content type, import and SHA-256 drift | Offline safety/protocol tests; live India Development full Terraform lifecycle with exact-key cleanup |
| `catalyst_filestore_file` | Upload, read, import and delete files; replace on changes | Offline binary upload, content drift, external deletion and recovery tests; live India tiny binary upload/import/rename replacement/stable plan/destroy with baseline cleanup |

All data sources are read-only. Webhook crons, File Store files, Functions, AppSail services API Gateway and unversioned Stratus objects are the managed resource areas. Live tests are opt-in; credentials and account IDs are never included in repository fixtures.

## API findings

Reviewed on 2026-10-23 against the [official Catalyst REST reference](https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/create-new-project/), the installed Catalyst CLI implementation, and the live India API.

- **Projects:** the reference documents create, read, list and delete. A request to create a uniquely named temporary project returned HTTP 400 with `PROJECT_CREATION_NOT_ALLOWED` in the test account. That API request created no project. This establishes a restriction for that account/authentication context, not a universal conclusion for all accounts. The console explicitly states that project names cannot be edited. A dedicated development project was successfully created through the signed-in console for lifecycle tests. Keep project creation out of the provider until supported OAuth access and full cleanup can be demonstrated.
- **Job pools:** the published reference documents list and get endpoints, with `ZohoCatalyst.jobpool.READ`. Create/update/delete pool endpoints were not found in the reviewed reference. Pool management is therefore not implemented. A reusable Webhook pool was successfully created through the console in the isolated development test project. No jobs were submitted.
- **Crons:** the reference documents create, read, update and delete under `/baas/v1/project/{project_id}/job_scheduling/cron`, plus enabled/disabled status. The `catalyst_webhook_cron` resource now implements pre-defined CronExpression schedules targeting Webhook. Create, import, rename/update, clearing optional fields, stable plans and deletion were verified live. Test crons stayed disabled and were removed. The dedicated development project and empty pool remain as reusable test prerequisites. Live validation also established a 20-character job-name limit and a 60–86400 second retry interval when retries are enabled. The provider distinguishes `404 INVALID_ID` from permission errors by checking parent-project access before removing state.
- **Other services:** File Store folders, Data Store table schemas and other infrastructure require a separate management API review. A data-operation API (for example inserting rows or executing a function) does not by itself establish a supported infrastructure lifecycle.

## Next implementation gates

1. Extend cron targets and schedule types only after verifying their request/response normalization and lifecycle in an isolated test setup.
2. Review supported management APIs for other services before promising Terraform resources. Do not infer infrastructure creation APIs from data-operation endpoints.
3. For each resource, verify import, drift, permission failures, updates, stable plans and cleanup. Default tests remain offline; live mutation tests require explicit opt-in.
4. Add cross-region live validation. Current lifecycle evidence covers India only; webhook delivery has not been exercised because all test crons stayed disabled.

## Public release gates

The repository must be public and the release process must produce versioned provider binaries with signed checksums. Configure the release signing secrets, verify the release workflow, register the provider with the Terraform Registry, and document tested service coverage before announcing public installation. Repository creation and local tests alone do not complete publication.

## Reliability and release checks

Offline tests cover partial-create ID recovery, safe read retries and rate limits, cancellation, delayed deletion, loss of parent access, project-change replacement, and unknown Terraform values. These checks do not extend the live regional or delivery claims above.

The release workflow requires shared checks for the tagged commit and creates a draft release. Unsigned package verification checks all configured platform archives, checksums, the Registry manifest and the native Terraform handshake. Signing secrets, a working private security-reporting route, successful hosted CI and clean-directory Registry installation remain public-release gates. Follow [RELEASING.md](RELEASING.md).

The reliability changes were checked locally on 2026-10-23: race-enabled offline tests, static analysis, all example validations, stable document generation, and the cron lifecycle on Terraform 1.13.5, 1.14.9, 1.15.9 and 1.16.7 passed. All 14 configured platform archives built and passed checksum/layout checks; only the native Linux executable was run through Terraform. These were unsigned snapshots, not published releases. That first hardening pass used offline tests; the follow-up validation below supersedes its live-test limitation.

### Follow-up reliability validation (2026-10-23)

- Shared OAuth refresh now supports independent caller cancellation, stops when all callers leave, shares failures without a concurrent refresh burst, and rejects abandoned results. Repeated race-detector tests cover those behaviors.
- Read-only discovery using refresh credentials and the disabled cron lifecycle using an access token passed again against the isolated India Development project. The post-test cron ID set matched the pre-test baseline, confirming cleanup. No webhook was invoked.
- Package verification now installs from a filesystem mirror through `terraform init`, without a development override or remote fallback, then reads the provider schema. This is still not evidence of public Registry installation or production signing.
- Lowercase snapshot versions work with Terraform mirror discovery. Archive checks enforce one version across platforms and support an explicit expected version. Seven focused packaging tests reject corrupt/mixed packages and unsafe inputs before executable launch.
- Native Windows/macOS test jobs are configured but have not run on hosted CI. Other Catalyst regions and actual webhook delivery remain unverified.

- GoReleaser signing was exercised locally with a disposable Ed25519 test key. The checksum signature, archive checks, mirror installation and schema handshake passed. The temporary key and test signature were removed. Production signing credentials and hosted release execution remain unconfigured/unverified.

## Service-level coverage audit (2026-10-23)

This is a capability inventory, not a claim of 100% coverage. A read-only data source counts as discovery only. No percentage is assigned without a complete, agreed service/operation inventory.

| Area | Implemented | Remaining or blocked |
| --- | --- | --- |
| Projects | Single and list discovery | Create/delete API exists, but create was rejected in this account; no managed project resource |
| Job Scheduling | Pool discovery; pre-defined Webhook CronExpression lifecycle | Function/Circuit/AppSail targets, other schedule types; pool creation API not established |
| Data Store | Table discovery | Table/column provisioning API not established; rows are data operations, not table provisioning |
| File Store | Folder discovery; file upload/read/import/delete and replacement | Folder provisioning API not established; metadata updates have no documented endpoint; legacy service |
| Cache | Segment discovery | Segment creation is explicitly console-only; cache entries expire within 48 hours and are not durable infrastructure |
| Stratus | Bucket discovery; unversioned object upload/read/import/update/drift repair/delete | Bucket provisioning API not established; bucket-policy/versioning, multipart, TTL and custom metadata remain unimplemented |
| NoSQL | None | Public reference documents item operations; table/index provisioning must be verified separately |
| Functions / AppSail / Circuits | Node.js 22/24 Basic I/O and Advanced I/O Function management; Node.js 22 AppSail management; Function discovery | Other Function runtimes/types and Circuit deployment/configuration remain unimplemented; AppSail currently supports Node.js 22 only |
| API Gateway / Authentication / Connections | Gateway enabled setting and complete Basic I/O rulesets; OAuth/default and explicit no-auth | Other Gateway targets/authentication/throttling, Authentication and Connections management remain unimplemented |
| Web Client / Domain Mappings / Slate / Pipelines | None | Deployment and domain/configuration lifecycles remain unimplemented |
| Mail / Notifications / Search / ZCQL / Zia / SmartBrowz / ConvoKraft / QuickML / Signals / Data Streams | None | Separate persistent configuration from send/execute/query operations; verify supported management APIs before defining Terraform resources |

Sources: [Catalyst REST reference](https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/create-new-project/), [Cache implementation](https://docs.catalyst.zoho.com/en/cloud-scale/help/cache/implementation/), [File Store documentation](https://docs.catalyst.zoho.com/en/cloud-scale/help/file-store/implementation/), and the installed official Catalyst CLI's File Store, Cache and Stratus endpoint implementations. Absence in the reviewed reference is not proof that no supported API exists; those areas need further vendor/API verification.

### New storage/discovery evidence

- Local Terraform lifecycle tests exercise populated and empty discovery, sorting, large-ID precision, permission failures and stable plans.
- File tests exercise binary multipart upload, source-hash validation, import, rename replacement, same-size content drift, external deletion/recreation, partial-create recovery, and retained state after permission errors or unconfirmed deletion.
- Live India tests use the isolated Development project and reusable `TerraformProviderFileTests` folder. Only generated five-byte fixtures were uploaded, and baseline file IDs matched after cleanup. The empty folder remains a test prerequisite. No production resources were modified.
- The initial discovery acceptance run used refresh credentials in many short-lived Terraform processes and hit OAuth HTTP 400 throttling. The successful rerun used one in-memory access token. Provider instances still refresh independently across processes; do not mistake that run for a cross-process token-cache test.
- Hosted CI is outside the current implementation task at the maintainer's request because billing is already known to block it. No further CI checks or billing changes are part of this pass.

### What a full-coverage claim would require

For each supported Catalyst infrastructure API: a maintained typed Terraform schema, create/read/update-or-replace/delete, import, drift handling, error/state recovery, examples, documentation and live cleanup evidence. Services without supported management APIs must be explicitly excluded from any **API-addressable** coverage percentage. This provider is not at that threshold yet, and its broader service coverage is not 10/10.

## Plan execution: capability inventory and compute investigation

The machine-readable draft at `coverage/capabilities.json` tracks 43 capability areas, including objects that still lack management support. It is deliberately marked incomplete until the service/field/API audit finishes. Run `make coverage-status` to see counts; it does not assign a coverage rating. An offline contract test checks that all registered provider types have matching inventory entries, generated docs and examples.

The Function metadata client implements typed read/list operations based on the installed official CLI. Contract tests cover deterministic ordering, large identifiers, incomplete/duplicate metadata, routing and errors. It excludes environment variables. Function discovery is registered as `catalyst_functions`; the subsequent managed Function milestone below adds `catalyst_function`.

On 2026-10-23, the maintainer confirmed deleting the old isolated test project. Its earlier successful live tests remain historical evidence, not evidence that the old test prerequisites still exist. Refresh-based probing also encountered a network routing error. A replacement isolated Development project is needed before the next live lifecycle milestone. Existing personal projects must not be used as a substitute.

### Compute investigation follow-up

A replacement isolated Development project was created and verified in the console and API. Direct Function API upload/read/code-update/delete checks passed with a generated Node 22 Basic I/O fixture. The update retained its ID, and the downloaded ZIP exactly matched the submitted bytes. Populated Function discovery passed real Terraform apply/plan checks. No Function was invoked.

The final immediate baseline comparison differed; follow-up confirmed zero Functions remained in the isolated project. Keep delete-confirmation polling as a requirement. The `is_deployed` field remained false after polling and `function_output` was empty. Its meaning (including Development/Production distinctions) is not established; do not treat it as a proven readiness signal or assume the Function failed. Name-keyed upsert collision semantics also remain an explicit resource-design gate.

## Managed Function milestone (2026-10-23)

The Node.js 22 Basic I/O `catalyst_function` resource supersedes the earlier deployment gate for this specific runtime/type. It manages prebuilt code ZIPs, memory and complete environment-variable maps. Code/configuration updates retain the ID. Project/name/runtime/type changes require replacement. Function metadata and exact downloaded artifact hashes verify desired-state persistence; no claim of runtime invocation, Production promotion or `is_deployed` readiness semantics is made.

A real Terraform lifecycle passed in the replacement India Development project: create, import verification, code/memory/environment updates, clearing variables, stable plan and destroy. Baseline listing confirmed cleanup. No Function was invoked. Local tests cover external code drift and deletion/recreation, refusing an existing name, partial-create ID recovery, permission-safe absence, cancellation and import validation.

Live testing exposed success/data:null for absent Functions and temporary null metadata/missing listings and 404 artifact replicas around upsert. Reads now use bounded reconciliation, and absence requires parent access and three consecutive absent listing observations. Artifact 404s are retried only while metadata remains accessible. Failed verification retains state. These fixes have focused regression tests.

The name-keyed upsert API still has no established atomic conditional-create primitive. Existing names are rejected before creation; writes are serialized within one provider client. Concurrent external deployment tools/independent Terraform states must not own the same project/name. This is an explicit API/ownership limitation, not a claim of distributed locking. Other Function types/runtimes remain unimplemented.

## Managed AppSail milestone (2026-10-23)

`catalyst_appsail` manages Node.js 22 ZIP deployment with memory (128/256/512/1024/2048 MB), startup command, port, Catalyst authentication and complete environment-variable maps. Project/name/runtime changes require replacement. Creation refuses an existing name; independent states and external tools must keep name ownership exclusive. The same per-client gate serializes Function and AppSail upserts. No distributed locking is claimed.

The installed official CLI establishes PUT `/appsail/upsert`, multipart code and top-level memory with configuration fields for command/port/authentication/environment. Live probes in the isolated India Development project verified deployment, typed configuration reads, exact downloaded ZIP bytes and deletion, with baseline cleanup. The app endpoint was never invoked. [AppSail configuration documentation](https://docs.catalyst.zoho.com/en/serverless/help/appsail/appsail-configurations/) describes command, port and allocation behavior.

Offline real-Terraform tests cover create/import/code and configuration updates/environment clearing, artifact drift repair, external deletion/recreation, stable plan and destroy. Client tests cover refusing name collisions, failed listing access, partial-create identity recovery, non-retried ambiguous writes, verified absence, propagation gaps and cancellation. Shared ZIP validation rejects unsafe paths, symlinks, duplicate entries, corrupt files and expanded archives over 100 MB. Other runtimes, container images, Java platforms, login redirects, disk configuration and application health checks remain outside the implemented schema.

The full AppSail Terraform lifecycle passed live in India Development: create, import, code/memory/environment update, variable clearing, stable plan and destroy, with baseline cleanup. An initial immediate-read implementation failed the live test because fresh and stale configuration observations alternated after upsert. Deployment verification now requires three consecutive matching configuration and artifact observations within two minutes; a focused regression exercises a match followed by stale data. No mutation is replayed by this verification loop.

A follow-up live run also verified startup-command and application-port updates and their restoration. The full offline checks, race tests, static analysis, generated documentation and example validation passed locally. Hosted CI remains outside scope.

## API Gateway milestone (2026-10-23)

The provider now registers six resources and eight data sources. `catalyst_api_gateway` controls enabled status. `catalyst_api_gateway_ruleset` owns the complete user ruleset for an enabled Gateway, currently limited to Basic I/O Function targets with OAuth or explicitly unauthenticated routes and unconfigured throttling. Other target/authentication modes and configured throttling are still gaps. This is partial service coverage, not a 10/10 claim.

Live API probes verified that disabled Gateways reject rule reads with 403 INVALID_OPERATION, deployments are asynchronous and can block further changes with MIGRATION_SCHEDULED, and a terminal state of completed can still include per-rule failures. A first fixture targeting a nonexistent client was rejected asynchronously. The subsequent Go probes used an existing generated Basic I/O Function and passed create/read/replace/delete for OAuth and no-auth. Complete ruleset replacement deleted omitted rules. No route or Function was invoked. The disabled/empty Gateway baseline and generated Function cleanup were confirmed.

The full live Terraform lifecycle passed in isolated India Development: Gateway creation, ruleset creation, both imports, replacement of two rules with one, clearing to an empty map, a stable second plan and destroy. Gateway disabled status and the Function baseline matched after cleanup. Offline tests cover drift repair, partial-write recovery identity, refusal to adopt existing rules, policy preservation, paginated listings, large IDs, permission failures, asynchronous per-rule failures and cancellation. Unknown policy fields fail safely. Writes are serialized per client; independent states/external tools must not share project ruleset ownership.

Reference: [official Gateway concepts](https://docs.catalyst.zoho.com/en/cloud-scale/help/api-gateway/key-concepts/) and [official CLI JSON example](https://docs.catalyst.zoho.com/en/catalyst-extensions/vs-code-extension/catalyst-tools-explorer/). The ruleset resource must be removed before disabling Gateway because its read API is unavailable while disabled. Dependency ordering through the Gateway resource ID handles destruction.

### Go-only repository utilities

The Python release, Registry, readiness and coverage helpers have been replaced with Go under `internal/releasetools` and `internal/cmd/provider-tools`. Local checks no longer need Python. Existing rejection, signature-identity, environment-isolation, exact-version and stale-evidence tests were ported; archive validation additionally rejects unsafe/duplicate entries and ambiguous provider executables. Hosted readiness reads remain optional and were not run.

The Go-only local checks, race tests, static analysis, generated references and all examples passed. All 14 unsigned platform snapshots built locally, and the Go verifier checked hashes/layout/manifest/version before a native filesystem-mirror install and schema handshake. Foreign binaries were inspected, not executed. No hosted CI, public Registry installation or publication was performed.

## Function runtime and type expansion (2026-10-23)

`catalyst_function` now supports Node.js 22 and 24 with both Basic I/O and Advanced I/O types. Existing defaults stay Node.js 22 Basic I/O. Each of the four combinations passed live Terraform create, import, code/memory/environment updates, variable clearing, stable plan and destroy in isolated India Development. Generated fixture IDs were compared with the baseline, and no Function was invoked. Other languages, runtime versions and Function types remain unimplemented.

Advanced I/O uses the official CLI’s distinct `/server/upsert` and `/server/{id}` deletion endpoints, with metadata/archive reads through the Function API. Legacy `applogic` metadata is normalized to `advancedio`. A regression checks routing and normalization. [Supported stacks](https://docs.catalyst.zoho.com/en/serverless/help/functions/stack/) and the installed official CLI were reviewed before extending the schema.

The first matrix exposed stale configuration/artifact observations and transient deletion 404s. Verification now requires three consecutive matching observations within two minutes. A definitive deletion 404 is reconciled against verified project access and complete listings; a still-listed immutable ID may be retried idempotently. Ambiguous server/transport failures are not replayed. Errors retain managed state. A separate issue in the new acceptance checks used a cancelled testing context during cleanup; cleanup now uses its own bounded context and checks for stable baseline restoration. One leftover generated fixture from the failed run was identified and its deletion confirmed before rerunning. Focused regressions cover these cases.

## Managed Stratus object milestone (2026-10-23)

The official `zcatalyst-sdk-node` 3.4.0 Stratus implementation establishes the bucket metadata, object metadata/listing, temporary bucket-signature and exact-key deletion contracts. Signed PUT/GET transfers use the expected regional `zohostratus` origin, with `compress:false` and an atomic `overwrite:false` create guard. OAuth/organization headers go only to the Catalyst API; signed URLs and response bodies never enter diagnostics or Terraform state. The SDK is reference material, not a provider dependency.

A private authenticated, unversioned test bucket was created in the isolated India Development project. A Go binary probe verified upload/download, duplicate-key HTTP 409, overwrite and deletion. The real Terraform resource then passed create, import, content/type updates, same-size drift repair, external deletion/recreation, stable plan and destroy. Exact-key absence was confirmed; the empty private bucket is retained as a reusable test prerequisite. No application was invoked.

`catalyst_stratus_object` requires existing bucket ownership and supports canonical relative UTF-8 keys, including escaped spaces and URL delimiters. It downloads bytes during refresh to compute SHA-256 independently of ETag. Metadata/listing permission failures and malformed pagination retain state with errors. Three absence observations plus verified bucket access distinguish deletion from inconsistent replicas; bounded waits reconcile propagation. Confirmed upload identities are persisted before verification. Ambiguous writes are never replayed, and creation never adopts an existing key.

The supported scope is existing **unversioned** buckets, no version history on the exclusively owned key, canonical content type and files up to the provider's 100 MB limit. Versioned buckets/keys and unrepresented metadata are rejected. Bucket provisioning, policies, versioning, multipart uploads, TTL and custom metadata are still pending. Other data centers and Production have not been live-tested. This adds one partially managed capability area; it does not complete the 43-area draft inventory.
