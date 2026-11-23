---
page_title: "Catalyst provider gap audit and implementation roadmap"
subcategory: "Getting Started"
description: "Prioritized capability audit, project creation dependency and delivery gates."
---

# Catalyst provider gap audit and implementation roadmap

Start with the [full coverage implementation checklist](#full-coverage-implementation-checklist).
It gives a solo maintainer a first pull request, a delivery order, and a way to
decide when a support claim is justified.

Reviewed on 2026-11-24 against provider commit `b5512eb40a85e022ea462419116961bdfb3d7ab0`
and official JavaScript SDK commit `9e870262459c4ed13e99e7db6fb68148199a56e7`.

The immediate goal is a reliable public preview on existing Development projects,
while resolving unattended project creation with Zoho. Keep the provider in Go
and use the official SDK as an API-contract reference. Adding a JavaScript runtime
dependency does not supply a missing provisioning API.

## Offline implementation progress

The first delivery adds [coverage/audit.json](../../coverage/audit.json) and the
validated `make coverage-audit` / `coverage-audit -json` commands. The 59 tracked
entries record lifecycle operations, sources, blockers and next actions. The
[catalog review](../../coverage/catalog_review.md) maps the service index and
records the first application-user, environment and Stratus field crosswalks.
The provider coverage test now checks the audit and registered schema together.
The provider also reads specific job pool capacity, Stratus bucket versioning
and regional URLs, bucket CORS rules, object versions and Web Client Hosting
history. The latest iteration adds organization and environment discovery
alongside Slate app, pipeline, SmartTrain and Data Streams metadata reads. Live verification is pending.
Provisioning and settings changes still require verified management contracts.

Current-status notes distinguish historical billing and live-fixture evidence from
today's state. A [project-creation support request](project_creation_support_request.md)
is ready for review. This stage is offline by the maintainer's choice; no support
message, cloud mutation or release publication is part of this delivery.

## Project creation decision

The current modular SDK has no project-provisioning package or creation method
in the inspected source. Its `ZCAuth.init` creates a local application object;
the normal service transport addresses an existing `/project/{projectId}`.
The older Node SDK integration guide likewise starts with console project
creation and now recommends migration to the modular JavaScript SDK.

The REST reference still describes `POST /baas/v1/project` with `project_name`,
OAuth scope `ZOHOCATALYST.projects.CREATE`, the regional API domain and optional
organization/environment headers. This provider already implements that request in
`internal/catalyst/project_management.go` and exposes `catalyst_project`.
The 2026-11-01 live creation recheck returned HTTP 400
`PROJECT_CREATION_NOT_ALLOWED` in the tested India OAuth context. This does not
establish that every account or region rejects it.

Separately, three user-approved existing projects were successfully deleted through
the API on 2026-11-08. That verifies those deletion operations, not project creation
or the complete Terraform-managed lifecycle. `Project-Rainfall` was retained.

1. Ask Zoho for the currently supported non-interactive creation endpoint,
   required OAuth client type/scopes, organization/owner role, and any account,
   region or plan eligibility requirement. The existing CLI issue remains open
   without comments as of this review; no new message has been posted.
2. Implement changes only if Zoho supplies a different supported contract. Do not
   infer a workaround from console cookies, private endpoints or extra headers.
3. With eligible credentials and explicit live-test authorization, run the existing
   disposable-project acceptance gate. Require creation, exact-ID reads, import,
   deletion protection, stable plans and verified cleanup before removing the
   experimental label.
4. Until then, use an existing project through `data.catalyst_project`, or explicit
   import only when Terraform is intended to own the entire project. The blocker
   must not prevent releasing the other supported resources.

Sources: [SDK initialization](https://github.com/catalystbyzoho/zcatalyst-sdk-js/blob/9e870262459c4ed13e99e7db6fb68148199a56e7/packages/auth-admin/src/index.ts),
[SDK transport](https://github.com/catalystbyzoho/zcatalyst-sdk-js/blob/9e870262459c4ed13e99e7db6fb68148199a56e7/packages/transport/src/http-handler.ts),
[legacy SDK integration](https://docs.catalyst.zoho.com/en/sdk/nodejs/v2/integrate-sdk-in-third-party-apps/),
[create-project REST reference](https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/create-new-project/),
and [vendor issue](https://github.com/catalystbyzoho/zcatalyst-cli/issues/1).

## Current baseline

The [capability manifest](../../coverage/capabilities.json) tracks **59 draft
capability areas**: **16 managed subsets, 19 discovery-only areas and 24
unimplemented areas**, represented by **13 resources and 24 data sources**.
One resource may implement several capability areas. These counts are not a
percentage of all Catalyst features; `inventory_complete` is explicitly false.

Main CI is operational. The [latest main run checked](https://github.com/Edson-Edwin/terraform-provider-catalyst/actions/runs/43237007497)
passed, but routine CI selects jobs by changed files. Full compatibility checks
remain a separate manual/release gate. Historical billing-blocked statements in
the coverage notes now have a dated current-status clarification.
No GitHub release or configured `GPG_PRIVATE_KEY`/`PASSPHRASE` secret name was
returned by the read-only publication checks. Registry signing, registration and
fresh public installation verification remain release work.

## How to discover the remaining gaps

For each persistent Catalyst object, compare the current official REST catalog,
modular SDK exports, official CLI contracts, existing capability manifest,
Terraform schema and acceptance tests. Record the exact source/version and classify
each operation separately: create, read/list, update/reset, delete and import.
An SDK helper that only selects an existing table/bucket is not table/bucket creation.

Track field-level gaps as well as missing resource types: accepted request fields,
defaults, clearing/removal behavior, immutable fields, ID precision, pagination,
async readiness, timeouts, drift and partial-failure recovery. Record data-center,
environment, OAuth scope and role restrictions independently from implementation.

Use separate statuses for missing code, unsupported or unverified management API,
partial implementation, missing fixture, failed live behavior and deprecated
compatibility. Do not turn an unknown contract into a promised feature. If a
service exposes only transient runtime actions, classify it outside Terraform
resource scope instead of adding an artificial resource.

The following backlog covers every currently tracked discovery-only or
unimplemented entry. The first audit iteration must extend this draft inventory
with any additional persistent objects found in the current catalogs.

## Discovery-only areas needing a management contract

| Capability ID | Next action and dependency |
| --- | --- |
| `scheduling.pool` | Establish pool create/read/update/delete, capacity defaults and deletion behavior before adding a resource. |
| `filestore.folder` | Recheck post-sunset service availability; prioritize migration/state compatibility rather than new File Store provisioning. |
| `datastore.table` | Confirm a supported schema provisioning lifecycle; existing metadata reads do not provide table ownership. |
| `datastore.column` | Establish type/default/nullable changes and destructive-update rules; depends on table ownership. |
| `cache.segment` | Verify provisioning beyond the documented console workflow and define deletion ownership. |
| `stratus.versioning` | Read the versioning flag. Verify the management contract and safe object ownership before adding writes. |
| `stratus.bucket_cors` | Read existing rules with the new data source. Verify the read in a disposable bucket and establish a supported write/reset API. |
| `stratus.bucket` | Establish bucket create/read/update/delete, naming and environment behavior; prerequisite for storage settings. |
| `stratus.object_versions` | Read version IDs, latest flags and metadata. Verify the read with a versioned fixture. |
| `authentication.application_user` | Read user access metadata by ID or page. Audit signup, identity membership, consent and delete ownership. |
| `hosting.client_deployment` | Read client version history. Verify live output, artifact drift and activation/delete ownership before adding writes. |
| `organizations.organization` | Read organization identities and the default flag. Audit minimum scope and organization lifecycle. |
| `environments.inventory` | List IDs, names and status. Confirm live read and supported scope. |
| `slate.application_configuration` | List app IDs and names. Audit app configuration readback and safe lifecycle operations. |
| `pipelines.pipeline_configuration` | Read pipeline identity and status. Audit definition fields and management separately from runtime execution. |
| `convokraft.smarttrain_material` | Read material identity and status. Verify the route and exact ownership before managing training material. |
| `datastreams.channel` | List channels and look up a channel by ID. Verify live access settings and a supported management API for channel writes. |
| `nosql.table` | Establish supported table provisioning, key/TTL changes and deletion semantics. |
| `nosql.index` | Establish local/global index creation, async readiness and deletion; depends on table management. |

## Unimplemented areas

| Capability ID | Next action and dependency |
| --- | --- |
| `scheduling.circuit_cron` | Verify the target lifecycle in an eligible region with a disposable Circuit fixture. |
| `datastore.table_permission` | Establish permission read/write/reset semantics and role/table ownership. |
| `datastore.index` | Identify a supported index management contract before implementation. |
| `stratus.bucket_policy` | Audit policies and public access with complete reset/readback behavior. |
| `authentication.configuration` | Find supported login/signup/social-provider/domain configuration APIs; console UI support alone is insufficient. |
| `authentication.role_and_permission` | Establish role CRUD/default-role/permission behavior; distinguish this from existing-user role assignment. |
| `connections.connection` | Find definition CRUD; runtime credential retrieval is not connection provisioning. |
| `circuits.definition` | Audit definition versioning, readback, updates and deletion in an eligible region. |
| `signals.event_bus` | Verify the current Signals inventory and management contract separately from legacy Event Listeners. |
| `signals.rule` | Audit persistent trigger/rule CRUD and initially disabled fixtures to prevent unintended execution. |
| `hosting.domain_mapping` | Verify mapping/DNS/TLS readiness and unmapping using a disposable domain. |
| `mail.domain_and_sender` | Verify domain/sender ownership and DNS verification without sending mail during lifecycle tests. |
| `mail.template` | Establish template definition CRUD and sensitive-value handling. |
| `notifications.configuration` | Identify persistent app/platform configuration APIs; exclude sending notifications. |
| `search.configuration` | Identify persistent index/search configuration APIs; exclude executing searches. |
| `zia.persistent_configuration_audit` | Inventory any durable configurable objects; exclude inference calls. |
| `smartbrowz.persistent_configuration_audit` | Inventory persistent settings if available; exclude screenshots/PDF jobs. |
| `convokraft.bot_configuration` | Audit bot/training/configuration ownership, sensitive data and async behavior. |
| `quickml.deployment_configuration` | Audit persistent deployment configuration separately from predictions. |
| `devops.alert_and_event_configuration` | Audit alert/subscription configuration and test without triggering notifications. |
| `environments.production_lifecycle` | Zoho documents console-based enable/disable. Confirm supported lifecycle and readiness APIs. |
| `environments.deployment` | Zoho documents console-only promotion and status. Track reverse migration and rollback separately. |
| `mdm.application_version` | Check management API availability in an eligible data center. Zoho documents console app hosting and version history. |
| `projects.iac_bootstrap` | The official CLI imports a project ZIP with browser confirmation. Verify a supported unattended contract before implementation. |

## Gaps within implemented subsets

| Area | Remaining implementation or verification |
| --- | --- |
| Projects | Resolve creation rejection and prove the full lifecycle. Renaming/timezone management need supported contracts. |
| AppSail | Containers need reliable configuration/artifact readback; the previous upload/readback probe failed. Custom domains and explicit scaling controls need API/fixture audits. |
| Functions | Audit additional languages/function types and persistent triggers. Current support is the documented Node 22/24 subset; handler deployment does not create triggers. |
| Stratus objects | Versioned ownership, TTL, larger files and multipart abort/resume need contract/safety work. Existing multipart support does not provision buckets. |
| Database configuration data | Existing rows/items require existing schemas. Broader field types and reset semantics need separate audits. |
| Authentication | Existing-user role assignment needs a disposable user and live verification; it does not provision authentication, users or roles. |
| Gateway | Live-test client routes with a hosted client. Throttling/system-route ownership remains unimplemented. |
| Environments | Environment selection is implemented; environment creation, promotion and rollback are separate unsupported management lifecycles. |

## Full coverage implementation checklist

**Release target.** A user can declare every persistent Catalyst capability with a
supported management API in the published scope, adopt existing infrastructure,
review an accurate plan and safely change or remove what Terraform owns.
Discovery has its own target for capabilities with a supported read API.
Publish the catalog date, supported regions and environments, and remaining
platform gaps alongside either claim.

### 1. Establish the coverage baseline offline

- [x] Map the local provider's 13 resources and 24 data sources to the draft manifest. The schema test also checks example and documentation presence.
- [x] Record operation-level implementation, sources, blockers and next actions for the 59 reviewed capability areas. The audit counts 16 managed subsets, 19 discovery-only areas and 24 unimplemented areas.
- [ ] **First pull request.** Reconcile the official product/service index against the REST catalog, SDK packages, and CLI provisioning/deployment commands. Pin the review date and source versions. Give every catalog entry a destination in the manifest or a documented reason for exclusion. Include services appearing only in help or release notes. [Catalyst service index](https://docs.catalyst.zoho.com/en/)
- [x] Record the documented application-user lifecycle and environment operations alongside existing provider fields and open ownership questions. [Catalog review](../../coverage/catalog_review.md)
- [x] Trace Web Client Hosting version upload, readback, activation and delete. Record active-version protection and the remaining API questions. [Catalog review](../../coverage/catalog_review.md#web-client-hosting)
- [ ] Extend the existing audit with an object/field crosswalk. Give each entry a service, stable object ID, method and source, API field, Terraform type/attribute or gap, availability by region/environment, fixture and last verification. Track list and exact-ID read separately. Track update, reset, replacement and destroy semantics where they differ.
- [ ] Tag durable configuration, read-only discovery, runtime actions, retired features and unresolved contracts. Keep durable gaps visible even when an account or API blocks implementation. Record the source, an owner, the question and what would reopen each blocker.
- [ ] Extend the existing schema/manifest check to reject an unmapped catalog entry or field in the reviewed snapshot, duplicate ownership and a verified claim without evidence. Keep the upstream comparison reviewable when Zoho changes its catalogs.

**Done when** another contributor can start at any service in the dated index and follow it to an implemented attribute, an evidence-backed exclusion or an open gap. Track catalog entries reviewed/total, eligible operations and fields implemented/total, and live checks passed/claimed. Report blocked, unknown and skipped counts alongside those figures. Set `inventory_complete` only after the catalog reconciliation and field review finish.

### 2. Establish a disposable test environment and resolve project eligibility

- [ ] **Maintainer, offline now.** Use the existing project support draft to assemble the documented request, observed error, date, region, OAuth client type, role and organization context. Redact account identifiers and credentials before sharing. Record any Zoho answer in the audit.
- [ ] **Zoho dependency.** Obtain the supported unattended creation route and eligibility rules. If Zoho confirms the current contract, exercise the existing resource. If it supplies a different contract, update the Go client and its protocol tests first. Keep creation experimental while the decision or live test is outstanding.
- [x] Create and verify an isolated India Development project for acceptance tests through the Catalyst console.
- [ ] Start with the read-only project lookup after supplying project read credentials. Keep the retained `Project-Rainfall` outside all destructive fixtures.
- [ ] Record prerequisites by test, including a job pool, a versioned and an unversioned bucket, Data Store and NoSQL schemas, a hosted client and a disposable user with two roles as needed. Record which IDs the test creates, which it borrows and how to verify cleanup after interruption. Supply OAuth material from the local environment or secret store.
- [ ] In an eligible isolated organization, run `make testacc-project`. Verify exact-ID create/read/import, a clean plan after refresh, replacement behavior, default deletion protection, explicit destroy and remote absence. Record rejected runs with their actual scope and error.

**Done when** existing-project work has a usable fixture and a tested cleanup procedure. Closing the independent project-creation item also requires a successful unattended lifecycle in the advertised account context.

### 3. Prove the provider users can try today

- [x] Map the **13 registered resources and 24 data sources** to tests and fixtures. [Live acceptance matrix](../../coverage/live_testing.md)
- [ ] Record results and any missing variant cases on the candidate revision. Distinguish passed, failed, skipped and fixture-blocked runs.
- [ ] Start with project lookup, table metadata, pool capacity, bucket versioning and CORS. Run the existing discovery gates, including `make testacc-infrastructure-details`. For every lookup, check correct identity, empty results where valid, missing objects, permission failures, ordering and pagination where applicable.
- [ ] Exercise Function and AppSail deployment, the three managed cron target types, Gateway configuration/rulesets, Stratus objects and multipart transfers, configuration rows/items and role assignment. Record File Store compatibility separately. Apply the resource checklist below to each advertised variant.
- [ ] Run a representative deployment from `examples/full_stack` in the disposable project. Create, run a second plan, change a field, introduce one out-of-band change, recover, then destroy. Read each owned ID back and verify that borrowed tables, buckets, pools and identities survive.
- [ ] Classify each failure as a provider bug, contract mismatch, eligibility issue or missing fixture. Fix reproducible provider bugs with a regression test. Narrow the documented support scope for failures that remain open.

**Done when** every advertised type and variant has a result tied to the candidate revision and scope. A skipped acceptance test still needs evidence before the feature earns a live-verified label.

### 4. Remove prerequisites that users currently create by hand

Use the capability IDs in the tables above to deliver one independently owned object or setting per pull request. Start with supported operations that unlock the most dependent resources. If a write contract is unknown, file the precise question in the audit and take the next ready item.

- [ ] **Stratus.** Audit bucket creation and deletion, then versioning, CORS and policy read/write/reset. Determine ownership of historical versions and behavior when deleting a nonempty bucket. Preserve unrelated objects, versions and policies in acceptance tests. Track object TTL and multipart recovery separately.
- [ ] **Data Store.** Confirm table identity and deletion semantics, then columns, defaults, type changes, indexes/searchable flags and permissions. Test with a populated disposable table so a schema change that loses data is visible in the plan.
- [ ] **NoSQL.** Confirm table provisioning, partition/sort keys and TTL before local/global indexes. Test index readiness, immutable key changes and exact-item ownership. Use existing tables as fixtures while schema provisioning is unresolved.
- [ ] **Scheduling.** Confirm pool name/ID, capacity defaults, updates and deletion with attached schedules. Verify all supported schedule forms against an existing pool. Add a Circuit-target cron when the target and schedule have independently verified lifecycles.
- [ ] **Identity and shared settings.** Review user role definitions, table permission dependencies and inherited defaults before writing grants. For a connection or identity provider, establish credential rotation, readable metadata and revocation first.
- [ ] **Cache and legacy File Store.** Check for supported segment provisioning. Recheck File Store availability and document state migration or removal for existing users before proposing new legacy writes.

**Done when** each foundation has either a tested management lifecycle or a recorded dependency and a working existing-object path wherever reads are supported. Only proven writes count toward provisioning coverage.

### 5. Finish service coverage in dependency order

- [ ] **Compute and ingress.** Extend Functions by runtime/type and persistent triggers. Resolve AppSail container image/configuration readback, scaling and domains. Define Gateway ownership of rules, client routes, system routes and throttling. Test one redeploy, import and drift case per newly advertised variant.
- [ ] **Hosting and environments.** Establish artifact identity/version for client hosting, then domain mapping, DNS/TLS readiness and unmapping. For environments, Slate and Pipelines, identify persistent configuration, version promotion and rollback behavior. Classify one-time execution separately.
- [ ] **Orchestration and events.** Audit Circuit definitions and versions before Circuit schedules. Audit Signals rules, Data Streams channels/access and DevOps alert receivers. Test disabled definitions or isolated destinations and verify cleanup.
- [ ] **Accounts and integrations.** Audit authentication/login providers, application users, role permissions, Connections, Mail domains/senders/templates, notification settings and search settings. Document ownership boundaries where a project, table or external account also controls a setting.
- [ ] **AI services.** Check Zia, SmartBrowz, ConvoKraft and QuickML for durable templates, bots, training configuration and deployed models. For each candidate establish an ID, readable configuration, update/replacement behavior and cleanup. Record inference, rendering or one-time training jobs as runtime actions when applicable.
- [ ] Add items uncovered in step 1 to the matching service queue. Reconcile these group checkboxes against individual capability IDs before closing a group.

**Done when** each eligible operation and field in the dated service inventory passes the applicable checklist below. Publish the open platform gaps with their evidence and affected user workflows.

### Pull request checklist for each resource or data source

- [ ] **Contract and ownership.** Link the official method or confirmed vendor contract. Specify the remote ID and import format, org/project/DC/environment, minimum scopes, defaults and which object or fields Terraform owns. Choose a resource, data source or documented prerequisite according to that contract.
- [ ] **Go client.** Reuse routing, OAuth, retry and error handling in `internal/catalyst`. Test response identity, ID precision, pagination, validation failures, access denial, rate limiting, timeouts and asynchronous readiness as applicable. Recover a timed-out create by confirmed identity before retrying a write that could make a duplicate.
- [ ] **Terraform schema and state.** Implement in `internal/provider`. Specify required/optional/computed attributes, omission, clearing, replacement and sensitive inputs. Refresh from the remote API. Preserve prior state on denied/transient reads and remove an absent managed object from state only after a definitive missing-object response. Add state upgrade tests when persisted shapes change.
- [ ] **Resource lifecycle.** Test create → refresh → empty plan → import into fresh state → update → remove an optional field → empty plan. Exercise external edit/deletion, replacement, partial failure and interrupted cleanup. Verify destroy deletes the exact owned ID or restores the documented setting. Verify that a neighboring object survives.
- [ ] **Data source behavior.** Test lookup by the declared identity, repeated reads, legitimate empty results, pagination limits and stable ordering for collections. Distinguish missing, denied and malformed responses. Check the account, environment and regional routing on every request.
- [ ] **Credentials and state.** Check redacted diagnostics, logs, examples and test fixtures. Identify values Terraform persists even when marked sensitive. Prefer credential references or supported write-only handling for secrets and verify the actual saved state before advertising that behavior.
- [ ] **User experience and evidence.** Add an executable HCL example, import example for managed objects, minimum scopes, prerequisites, known limitations and recovery instructions. Update the manifest and operation audit. Generate docs, run targeted offline tests, then record the live result, revision, date, region, environment and cleanup for the scope being claimed. [Terraform acceptance testing](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests)

### 6. Qualify the release and keep coverage current

- [ ] Run the existing offline checks and packaging commands below. Verify documentation generation is repeatable and that prior example configurations and state still work. Run the manual full compatibility workflow on the candidate commit on `main`.
- [ ] Run regional read checks across the seven configured data centers, both environments and both supported OAuth modes where fixtures exist. Record each missing combination. Gate writes separately and publish the exact region/environment coverage for each service. A successful login alone verifies authentication.
- [ ] Prepare a versioned changelog, limitations, upgrade notes and support table. Review the generated docs, signed checksums and release archives. After the maintainer configures signing and Registry publication, verify a clean public `terraform init` at the pinned version and a read-only plan from a second user's setup. [Release procedure](../../RELEASING.md)
- [ ] On each release, diff the saved catalog against Zoho's current services, docs, CLI and SDK. Reopen affected rows when a new field, region, restriction or deprecation appears. Re-run the smallest affected lifecycle and the provider-wide compatibility checks.

**Decision to declare full coverage.** The inventory is reconciled, every eligible management operation and field has implementation and evidence for its advertised scope, every discovery claim is verified, and a clean public install succeeds. Publish any unresolved durability or eligibility questions explicitly and use a narrower release label while they affect the claim.

### First three pull requests for one maintainer

| Order | Deliverable | Review it by checking |
| --- | --- | --- |
| 1 | Extend the existing catalog and field audit, beginning with application-user lifecycle and environment operations. | Trace one documented operation from Zoho to the Terraform schema or a named gap. Run the schema/manifest check and show the remaining unaudited count. |
| 2 | Build one read-only feature end to end. A candidate is version discovery for one Stratus object in an existing versioned bucket. | Specify the object key and result bound, cover empty/multiple pages and repeated continuation tokens offline, and add an example that returns version IDs. Use a versioned fixture for the later read-only live check. [Zoho object-version listing](https://docs.catalyst.zoho.com/en/sdk/nodejs/v2/cloud-scale/stratus/list-object-versions/) |
| 3 | Qualify one existing write path before adding another, starting with `catalyst_stratus_object` in an unversioned bucket. | Cover create/import, content and metadata drift, clearing metadata, repeat apply and exact-key cleanup. Reuse existing tests, add only missing cases and collect live evidence when the disposable fixture is available. |

These three changes can start offline. The project eligibility question proceeds independently. Select the next write implementation from step 4 once a management contract and test fixture are available.

## Testing and release gates

Existing offline audit commands, run from the repository root:

```sh
go run ./internal/cmd/provider-tools coverage-status
go run ./internal/cmd/provider-tools coverage-audit -json
TF_ACC=0 go test -race -count=1 -run '^TestCapabilityInventoryMatchesProvider$' ./internal/provider
```

Each new resource needs API-contract tests, Terraform create/read/import/update/
stable-plan/drift/delete tests, permission and ambiguous-response recovery cases,
explicit routing, bounded waits and privacy checks. Test field removal/default
restoration as well as setting a field. Generated docs and an executable example
must match the provider schema and capability manifest.

Before a release, run `make check`, `make lint`, `make generate` and
`make release-check`, then the full hosted workflow and signed-draft verification.
Publishing requires successful clean public installation, not just a local build.
See [release instructions](../../RELEASING.md),
[service boundaries](service_expansion.md),
[project ownership](project_management.md) and
[regional fixture instructions](../../CONTRIBUTING.md).

## Inputs needed before live implementation

- Table, bucket, job-pool and user fixtures in the isolated Development project
  as needed by the selected tests.
- OAuth credentials supplied through a protected local environment or secret store,
  with only the selected service scopes. No private key or OAuth token is needed
  in chat to prepare this plan.
- Zoho's clarification for API-blocked features, starting with project creation.
- Optional isolated Production/other-region fixtures and a disposable DNS domain
  only when testing those specific capabilities.

Keep destructive test operations within the isolated Development fixture.
