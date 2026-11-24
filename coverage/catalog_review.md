# Catalyst catalog review

The November 24, 2026 service index maps to the capability groups below. Each
group still needs a component-by-component REST, SDK, CLI and field review.
The operation-level starting point is [audit.json](audit.json). The audit remains
open until each group has a source-backed disposition for its persistent
operations. [Catalyst service index](https://docs.catalyst.zoho.com/en/)

| Official service or platform area | Capability groups | Work to reconcile |
| --- | --- | --- |
| Slate | `slate.*` | App linking, repository, branch, build settings, environment variables, domains and deployment versions. |
| Serverless | `functions.*`, `appsail.*`, `circuits.*` | Deployment runtimes, triggers, scaling, container and artifact readback, and configuration ownership. |
| CloudScale | `authentication.*`, `connections.*`, `datastore.*`, `nosql.*`, `stratus.*`, `filestore.*`, `cache.*`, `gateway.*`, `hosting.*`, `datastreams.*`, `mail.*`, `notifications.*`, `search.*` | Resource lifecycle and settings review. Classify ZCQL/search queries, mail delivery and live subscriptions as runtime. Review service settings separately. |
| Zia Services | `zia.*` | Separate persistent models and app settings from per-request inference. |
| DevOps | `devops.*` | Map monitoring, health, alert and receiver settings. Classify logs and diagnostics according to retention and ownership. |
| SmartBrowz | `smartbrowz.*` | Enumerate durable templates and grid configuration separately from browser sessions and rendering requests. |
| Job Scheduling | `scheduling.*` | Pool ownership, schedule fields and target combinations. One-off job submission has a runtime lifecycle. |
| Pipelines | `pipelines.*` | Definitions, source integrations, credentials, schedules and build settings. |
| QuickML | `quickml.*` | Inventory connectors, datasets, pipeline definitions, models and serving endpoints. |
| Signals | `signals.*` | Inspect bus, source, rule, filter and target configuration. |
| ConvoKraft | `convokraft.*` | Inventory bots, channels, training material, access and persistent settings. |
| Project settings and Deployment & Billing | `projects.*`, `environments.*` | Audit organization/project ownership, deployment workflows and account controls. Record which account-level billing actions require the owner. |
| CodeLib and developer tools | Platform tooling | Audit any resulting durable objects under their owning service. Classify local scaffolding, code generation and packaging separately. |

## Application users

API paths below are relative to `/baas/v1/project/{project_id}`. The app-user
ZAAID in these APIs represents application tenancy. The provider's
`organization_id` supplies the separate Catalyst organization routing header.
[Catalyst authentication API](https://docs.catalyst.zoho.com/en/api/code-reference/cloud-scale/authentication/add-new-user/)

| Operation or field | Provider support or open question |
| --- | --- |
| `GET /project-user/{user_id}` with users READ | `data.catalyst_auth_user` reads by exact ID. `user_id` and `role_details.role_id` retain precision. Role name, user type and status are projected. The provider omits email, name, profile and membership from state. |
| `GET /project-user?start=…&end=…` with users READ | The paged data source maps the documented start/count request and exposes only IDs and access metadata. Verify tenant visibility and pagination live. [User list reference](https://docs.catalyst.zoho.com/en/api/code-reference/cloud-scale/authentication/get-all-users/) |
| `POST /project-user/signup` with users CREATE | Documented platform, user fields and optional redirect. Establish when it creates a new identity or sends an invitation, whether confirmation is needed before the ID can be read, and how to recover an ambiguous create. |
| `POST /project-user` with users CREATE | Documented add into existing app organization. The ZAAID and membership are immutable according to the docs. Confirm whether a replacement would revoke access to a shared identity. |
| Role assignment | Existing `catalyst_auth_user_role` owns the role assignment of a pre-existing user. Profile/account mutations need independent review. |
| `DELETE /project-user/{user_id}` with users DELETE | Removes application access. Confirm if it removes the identity or membership across multiple app organizations. Managed adoption and destroy require exact-ID and isolated live verification. |
| Password reset and sign-out | Runtime operations. |

## Environments and project bootstrap

| Operation | Provider support or open question |
| --- | --- |
| Select project, data center, organization and environment | Existing provider routing, with persisted routing guards for managed resources. [Environment guide](../docs/guides/environments.md) |
| Enable, disable or re-enable Production | Console workflow. Production readiness and billing eligibility still need a management contract. [Zoho Production environment](https://docs.catalyst.zoho.com/en/deployment-and-billing/environments/production-environment/) |
| Preview and promote selected components to Production, then read the rollout status | Documented console workflow. Zoho explicitly states that the CLI and public API do not expose production promotion or deployment status. [Zoho deployment FAQ](https://docs.catalyst.zoho.com/en/faq/general/deploy-to-production-from-cli-or-ci/) |
| Production-to-Development migration and rollback | Zoho documents a reverse deployment. Verify rollback separately since a reverse deployment is a distinct operation. [Zoho deployment types](https://docs.catalyst.zoho.com/en/deployment-and-billing/environments/deployment-types/) |
| Bootstrap from a project template ZIP | `catalyst iac:import` creates a new project with some child configuration. Zoho documents browser confirmation. Audit an unattended contract, job failure/recovery and ownership before wiring it to Terraform. [Zoho CLI import](https://docs.catalyst.zoho.com/en/cli/v1/export-and-import-projects/import-project/) |

## Stratus object-version fields

The version listing maps `project_id`, `bucket_name` and the exact
`object_key` to the request. The client handles continuation internally and
checks the returned key on every page. The data source exposes
`versions[*].version_id`, `is_latest`, `size_bytes` (API `size`), `etag`, and
`last_modified`. The timestamp retains Zoho's original format. The version
list has Terraform protocol tests, and an opt-in acceptance test requires an
existing versioned object. Owned version writes and delete-marker handling
remain separate capability work. [Zoho version-listing reference](https://docs.catalyst.zoho.com/en/sdk/nodejs/v2/cloud-scale/stratus/list-object-versions/)

## Web Client Hosting

| Operation | Provider support or open question |
| --- | --- |
| Deploy or update a ZIP | The CLI and console support deployment. The archive's `client-package.json` defines its version, and Zoho documents restrictions on reusing or decrementing versions. Verify what immutable ID and artifact digest can be read back before designing a resource. [Web Client Hosting concepts](https://docs.catalyst.zoho.com/en/cloud-scale/help/web-client-hosting/key-concepts/) |
| Read version history or active version | The official CLI reads `GET /baas/v1/project/{project_id}/webapp/history` as one array. `data.catalyst_web_client_versions` exposes `history_id`, `version` (API `app_version`) and `live` (API `status`). Offline protocol tests cover empty, duplicate and denied responses. Verify a known live version in a disposable fixture. [Pinned CLI contract](https://github.com/catalystbyzoho/zcatalyst-cli/blob/ed1eef8f32f870201c2a3c72b607318ca11a0861/src/endpoints/lib/client.ts) · [CLI history consumer](https://github.com/catalystbyzoho/zcatalyst-cli/blob/ed1eef8f32f870201c2a3c72b607318ca11a0861/src/pull/features/client.ts) |
| Activate a previous version | The console supports selecting a prior version. Audit whether a supported call can change only the active pointer and recover after a partial failure. |
| Delete a version | CLI and console document exact-version deletion and protect the active version. Confirm that a destroy can safely detach the active version and verify that neighboring versions remain intact. [CLI client deletion](https://docs.catalyst.zoho.com/en/cli/v1/working-with-the-client/delete-client/) |

## Foundational writes

| Area | Evidence and remaining work |
| --- | --- |
| Stratus buckets and CORS | The bucket creation workflow selects a globally unique name, access template and versioning. CORS edit/delete is documented through the console. Audit supported write endpoints and a precise ownership boundary for nonempty buckets, permissions and individual CORS rules. [Create a bucket](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/buckets/create-bucket/) · [CORS](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/stratus-config/bucket-cors/) |
| Data Store and NoSQL | Data Store's console establishes default columns when it creates a table. NoSQL console schema creation and deletion are separate from documented item APIs. Audit a supported management contract and test schema changes against populated disposable tables. [Data Store tables](https://docs.catalyst.zoho.com/en/cloud-scale/help/data-store/tables/) · [NoSQL tables](https://docs.catalyst.zoho.com/en/cloud-scale/help/nosql/create-manage-tables/) |
| Job pools | The console creates pools by target type and configures per-function memory or maximum target count. Confirm management methods and what happens when schedules reference a pool. [Job pool setup](https://docs.catalyst.zoho.com/en/job-scheduling/help/implementation/create-jobpool/) |
| Circuit cron | The cron update reference recognizes a Circuit target. Zoho currently lists EU, AU, IN and CA as unavailable for Circuit scheduling, so the India fixture cannot verify it. An eligible project needs an existing Circuit, its job pool and a schedule created disabled. [Cron API](https://docs.catalyst.zoho.com/en/api/code-reference/job-scheduling/cron/update-cron/update-cron-cron-expressions/) · [Cron availability](https://docs.catalyst.zoho.com/en/job-scheduling/help/cron/introduction/) |

## Organizations, environments, Signals, Slate, Pipelines and SmartTrain

| Area | Implemented fields and next management dependency |
| --- | --- |
| Organizations | The CLI lists visible organization IDs, names and the default flag. The provider reads that account inventory. Membership and organization management need separate semantics. [CLI organization source](https://github.com/catalystbyzoho/zcatalyst-cli/blob/ed1eef8f32f870201c2a3c72b607318ca11a0861/src/endpoints/lib/org.ts) |
| Project environments | The pinned CLI reads `env_zgid`, `env_name`, `env_type` and status using the project environment endpoint. The provider projects them and the optional default flag. Activation and promotion remain separate operations. [CLI environment source](https://github.com/catalystbyzoho/zcatalyst-cli/blob/ed1eef8f32f870201c2a3c72b607318ca11a0861/src/endpoints/lib/env.ts) |
| Signals | The CLI sample generator reads legacy Cloud Scale Event Listeners. Zoho's announcement treats Signals as a separate service. Current Signals publishers, rules, filters and targets need their own read and write contracts. [CLI event bus source](https://github.com/catalystbyzoho/zcatalyst-cli/blob/ed1eef8f32f870201c2a3c72b607318ca11a0861/src/endpoints/lib/event-bus.ts) · [Zoho deprecation announcement](https://docs.catalyst.zoho.com/en/announcements/2025/fs-el-cron-deprecation-announcement/) |
| Slate | The data source lists app IDs and names. The CLI also uploads archives and reads build status, but the provider still needs artifact identity, configuration readback, rollback and deletion behavior before it can manage a deployment. [CLI Slate source](https://github.com/catalystbyzoho/zcatalyst-cli/blob/ed1eef8f32f870201c2a3c72b607318ca11a0861/src/endpoints/lib/slate.ts) |
| Pipelines | The documented exact-ID lookup returns a pipeline and its project. The provider verifies both IDs and returns name/status. Pipeline execution is a separate runtime action. Definition ownership still needs a management contract. [Pipeline read API](https://docs.catalyst.zoho.com/en/api/code-reference/pipelines/get-pipeline-details/) |
| SmartTrain | The data source reads material ID, title, source type and status in Development. The documentation's endpoint field omits the project prefix present in its full cURL example, which the provider follows. The experimental resource implements URL material creation, replacement, import and exact-ID deletion against the documented sample envelopes. Verify routing, URL normalization, asynchronous status and deletion in an isolated bot. [SmartTrain read API](https://docs.catalyst.zoho.com/en/api/code-reference/convokraft/get-training-materials/) |

Zoho documents hosting, version history and deletion for Mobile Device Management
through the console. The India acceptance fixture cannot verify that service.
[Zoho MDM availability](https://docs.catalyst.zoho.com/en/cloud-scale/help/mobile-device-management/introduction/)
