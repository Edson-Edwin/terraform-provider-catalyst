## 0.1.0 (Unreleased)

FEATURES:

- Configure Catalyst data center, environment and organization independently for each provider instance.
- Authenticate using an access token or OAuth refresh credentials, with shared in-memory token caching.
- Read project metadata with `catalyst_project`, discover projects with `catalyst_projects`, and list existing job pools with `catalyst_job_pools`.
- Manage and import pre-defined CronExpression schedules with Webhook targets using `catalyst_webhook_cron`. Crons default to disabled.

- Discover Data Store tables, File Store folders, Cache segments and Stratus buckets with four new data sources.
- Manage existing File Store workloads with `catalyst_filestore_file`: binary upload, SHA-256 verification, import, replacement, drift detection and confirmed deletion without storing contents in state.

FIXES AND RELIABILITY:

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
- Project creation, job-pool creation, other cron types/targets and many other Catalyst services remain unimplemented. See `API_COVERAGE.md`.

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
