## 0.1.0 (Unreleased)

BREAKING CHANGES:

- All managed resources now require an explicit organization. Environment-scoped resources persist `routing_scope` and reject changed region, environment or organization before resource requests, including saved-plan applies. Refresh legacy state under its original routing before mutation. Projects retain their separate cross-environment scope.
- Managed projects now require an explicit provider organization. State binds the organization and data center; changed routing fails before requests. Refresh legacy project state with its original routing before updates or deletion.
- Invisible projects are retained with an error instead of automatically removed and recreated. After independently confirming external deletion, remove only the exact Terraform binding to authorize a replacement. Delete preflight no longer treats a missing project as success.

FEATURES:

- Add regional project/Stratus checks across both environments and authentication modes, SHA-256 validation of signed downloads, and disposable-object lifecycle checks with exact-key cleanup. Add a protected-environment CI matrix and offline verification of fixture isolation and cleanup failures. Authenticated live coverage remains pending regional fixtures.

- Add `allowed_data_centers` and `allowed_environments` configuration policies with environment fallbacks. Nonempty policies require explicit destinations and fail before authentication or access preflight when a selection is not approved.

- Add optional `allowed_organization_ids` and `forbidden_organization_ids` routing guardrails and a read-only `validation_project_id` preflight. Guardrails require an explicit organization; they do not attest caller identity.

- Configure Catalyst data center, environment and organization independently for each provider instance.
- Authenticate using an access token or OAuth refresh credentials, with shared in-memory token caching.
- Read project metadata with `catalyst_project`, discover projects with `catalyst_projects`, and list existing job pools with `catalyst_job_pools`.
- Manage and import pre-defined CronExpression schedules with Webhook targets using `catalyst_webhook_cron`. Crons default to disabled.

- Discover Data Store tables, File Store folders, Cache segments and Stratus buckets with four new data sources.
- Manage existing File Store workloads with `catalyst_filestore_file`: binary upload, SHA-256 verification, import, replacement, drift detection and confirmed deletion without storing contents in state.

FIXES AND RELIABILITY:

- Add configurable project operation timeouts, exact-ID readiness checks that recover incomplete create metadata, and deletion polling that tolerates temporary read/list disagreement. Retain state on permission failures and timeouts without replaying ambiguous writes. Expand offline Terraform guard/import/recovery coverage and add `make testacc-project` for the full disposable-project lifecycle; live account eligibility remains unverified.

- Clone standard HTTP transports, require certificate verification and TLS 1.2 or newer, preserve stronger TLS minimums and reject incompatible TLS limits without mutating shared clients.
- Test forged Stratus origins and redirect credential isolation across every supported data center and both environments, including HTTP downgrade, cross-region/environment responses, lookalike hosts, userinfo, ports, paths and query strings.

- Preserve per-client OAuth cooldown deadlines across failed, cancelled and expired refresh sequences, including long `Retry-After` responses. Concurrent new callers fail fast during cooldown; successful refresh clears it and stale responses cannot affect a newer refresh.
- Separate OAuth validation, token refresh and typed attempt results into `oauth.go`; consolidate API, OAuth and Stratus routing metadata in `regions.go`.
- Add a strict all-region, both-authentication-mode live read gate. Missing fixtures fail before any API request; mock coverage remains separate from live evidence.

- Require an explicit data center for Production. Existing Production configurations relying on the implicit US default must set `data_center = "us"` or `CATALYST_DATA_CENTER=us`.
- Retry explicit transient OAuth HTTP responses with bounded exponential backoff, jitter, `Retry-After` handling and a shared deadline. Ambiguous transport failures and credential rejection are not replayed.
- Verify OAuth/API routing for all seven data centers in both environments, concurrent provider-instance isolation, organization guardrails and access-preflight errors offline. Add opt-in read-only regional smoke tests; cross-region live evidence remains pending.

- Make shared OAuth refresh cancellation independent for each caller; cancel abandoned refreshes and prevent stale results from overwriting newer tokens.

- Preserve a trusted newly created cron ID when other response metadata cannot be decoded, allowing Terraform to retain recovery state.
- Retry temporary GET failures up to three times with exponential backoff, jitter and bounded `Retry-After` handling. Ambiguous writes are not automatically retried.
- Wait up to two minutes for cron deletion, respect cancellation, and preserve state if absence or parent access cannot be confirmed.
- Provide controlled HTTP error guidance and allowlisted API error codes without exposing arbitrary response content.

DEVELOPMENT AND RELEASES:

- Verify downloaded draft artifacts against the trusted signing-key fingerprint before local-mirror installation.
- Add an isolated post-publication Registry installation checker and read-only release-preflight tooling.
- Add API, Terraform lifecycle/protocol and opt-in live tests for storage and discovery.

- Add offline lifecycle, import, drift, replacement, unknown-value, recovery and retry tests; live tests remain opt-in.
- Require reusable checks before release packaging, pin release tools, and verify dependency files without modifying them.
- Build draft releases with signed checksums. Offline packaging checks validate archives, the Registry manifest a consistent release version, installation through a local filesystem mirror and the native Terraform protocol handshake.
- Add native Windows and macOS lifecycle CI jobs; hosted execution remains pending.
- Use lowercase snapshot versions compatible with Terraform mirror discovery.

LIMITATIONS:

- No Terraform Registry release is available yet.
- Recorded live validation covers India. Actual webhook delivery and other regions have not been tested.
- Project creation is experimental and the current live account is ineligible; job-pool creation, other cron types/targets and many Catalyst service lifecycles remain unsupported. See `API_COVERAGE.md`.

PLAN EXECUTION:

- Add a draft capability inventory and schema/documentation/example consistency test.
- Add internal Function metadata read/list client and offline API contract tests; add the live-verified `catalyst_functions` discovery data source and the managed Function capability described below.

FUNCTION MANAGEMENT:

- Add `catalyst_function` for Node.js 22 Basic I/O code deployment, in-place code/memory/environment updates, import, hash-based drift detection and confirmed deletion.
- Reject existing names before creation and retain created IDs after incomplete metadata or verification failures.
- Handle success/data:null absence, transient metadata/listing gaps and 404 artifact propagation with bounded reads and verified parent access.
- Validate ZIP configuration, paths, integrity and source hash before uploading; protect environment variables as sensitive state.

APPSAIL DELETION RELIABILITY:

- Reconcile a definitive AppSail deletion 404 against verified project access and complete listings. Retry the same immutable ID only while it remains listed, with a one-minute bound; never replay an ambiguous transport/server failure.
- Add a gated recovery helper that deletes only explicitly selected recent acceptance-test fixture IDs. Confirmed cleanup in the isolated India Development project.

SCHEDULING UTC DEFAULT:

- Encode the public UTC default as the console-supported GMT wall-clock timezone. The Catalyst API rejects the literal UTC identifier. Preserve UTC in state and preserve an explicitly configured GMT alias.
- Pass 60 live Terraform create/update/import/stable-plan steps across all Function and Webhook schedule forms using the default timezone, followed by confirmed fixture deletion. No scheduled jobs were executed.

APPSAIL SCHEDULING:

- Add `catalyst_appsail_cron` for expression, calendar, one-time and periodic schedules. Verify AppSail/pool membership and the authenticated HTTPS target origin. Manage HTTP fields and retries, with disabled-by-default scheduling and sensitive request values.
- Pass live Terraform create/import/update/clear/drift repair/external deletion and recreation/stable plan/destroy and all seven schedule forms using the UTC default. Confirm temporary AppSail cleanup without executing jobs or endpoints.
- Record vendor-documented Data Store schema automation limits, Circuit regional restrictions and the legacy CloudScale Cron sunset separately from provider implementation gaps. Coverage remains incomplete.

APPSAIL NODE.JS 24:

- Extend `catalyst_appsail` to Node.js 24 while retaining the Node.js 22 default. Runtime changes still require replacement.
- Pass local Terraform artifact drift/external deletion regressions for both runtimes and live Node.js 24 create/import/code and configuration updates/clear/stable plan/destroy. Restore the isolated test baseline without invoking the app.

FILE STORE RETIREMENT:

- Mark `catalyst_filestore_file` and `data.catalyst_filestore_folders` deprecated with Terraform configuration warnings. Zoho announces File Store sunset on 31 October 2026 and excludes new projects/accounts.
- Keep existing schemas/state compatible; no automatic file or permission migration occurs. Direct new storage to Stratus and link the vendor notice in public documentation.

EXPERIMENTAL PROJECT MANAGEMENT:

- Add documented project create/read/delete, immutable-name replacement, import and default-on local deletion protection, with offline Terraform lifecycle and recovery tests.
- Recheck live creation: the current account returns PROJECT_CREATION_NOT_ALLOWED. Keep this limitation explicit; no successful live project lifecycle is claimed.

APPSAIL MANAGED LANGUAGES:

- Add managed Node16/18/20, Java SE8/11/17/21/25 and Python3.9–3.13 ZIP deployments alongside Node22/24. Expose deployment URL and login redirect/root reset. Keep runtime/platform replacement and explicit Java/Python startup requirements.
- Reconcile stale refreshes and retain known URL state after failed updates. Verify all 15 deployment lifecycles and HTTP responses for generated Node24/Python3.13/Java25 apps before and after updates.

DATABASE CONFIGURATION AND DISCOVERY:

- Add `catalyst_nosql_item` for persistent typed configuration items, with exact numeric/binary normalization, atomic duplicate protection, consistent reads, per-item status checks, import, attribute removal, drift repair and confirmed deletion. Keep transactional application data outside Terraform.
- Add `catalyst_nosql_tables`, `catalyst_nosql_table` and `catalyst_datastore_table` data sources with allowlisted metadata. Table/index/schema provisioning remains unimplemented; read support does not imply management support.

STRATUS METADATA AND WEBSITE EXAMPLE:

- Add complete custom metadata-map management through the documented replacement API, including clearing, import, live drift repair and state recovery. Bucket provisioning, policies, versioning, TTL and multipart remain unsupported.
- Add a combined website configuration with explicit project/table/schema/bucket prerequisites and application responsibilities.

- Pass a live generated full-stack application smoke test across frontend HTML, AppSail backend, NoSQL configuration reads, private Stratus download and Data Store row CRUD, with stable plan and independent fixture cleanup. Table/schema/bucket provisioning remains manual.
