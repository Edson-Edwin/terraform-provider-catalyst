# Live acceptance matrix

A disposable India Development project was created through the Catalyst console
on November 24, 2026. Set its project ID, organization ID, data center and
environment locally. The first test needs project read access. Individual tests
require additional scopes and fixtures, and mutation tests use separate opt-ins.
See the [provider credentials guide](../docs/index.md) and
[contributor fixture instructions](../CONTRIBUTING.md).

As of this milestone, the new project has only console verification. The tests
below are ready for scheduling once their credentials and fixtures are
available. Record the exact revision, date, region, environment, authentication
mode, result and cleanup for each run. Use separate statuses for failed,
skipped and fixture-blocked runs.

## Read-only gates

All Terraform type names below use the `catalyst_` prefix.

| Data sources | Existing test target | Additional prerequisites |
| --- | --- | --- |
| `project` | `make testacc` | Project read scope and exact project ID. Start here. |
| `projects`, `job_pools` | `make testacc-discovery` | Accessible organization and project plus job pool read scope. |
| `datastore_tables`, `nosql_tables`, `functions`, `stratus_buckets`, `cache_segments`, `filestore_folders` | `make testacc-inventory` | Service-specific access. Record the File Store result separately as legacy compatibility. |
| `datastore_table`, `nosql_table` | `make testacc-database-schema` | Existing isolated tables with the fixture names in the test. Supply both IDs to exercise both branches. |
| `job_pool`, `stratus_bucket`, `stratus_bucket_cors` | `make testacc-infrastructure-details` | Existing pool and bucket. Use a bucket with known CORS rules to verify the reported values. |
| `stratus_object_versions` | `make testacc-stratus-versions` | Existing versioned bucket, object key and expected latest version ID. A multi-version fixture checks the complete collection. |
| `auth_user` | `make testacc-auth-user` | Existing disposable user, user read scope and the user's expected role ID as `CATALYST_TEST_AUTH_FALLBACK_ROLE_ID`. |
| `web_client_versions` | `make testacc-web-client-versions` | Existing hosted client, `ZohoCatalyst.webapp.ALL` and the expected live `CATALYST_TEST_WEB_CLIENT_LIVE_HISTORY_ID`. |
| `project_environments`, `slate_apps`, `pipeline`, `smarttrain_materials` | `make testacc-extended-discovery DISCOVERY_TEST=TYPE` | Substitute one type name from this row. Pipeline and SmartTrain need `CATALYST_TEST_PIPELINE_ID` and `CATALYST_TEST_SMARTTRAIN_BOT_ID` respectively. SmartTrain also requires Development and an explicit organization. Use the scope from the relevant data-source docs. |
| `organizations` | `make testacc-extended-discovery DISCOVERY_TEST=organizations` | OAuth access to organization discovery in the configured data center. Unset `CATALYST_ORGANIZATION_ID` for account-wide testing. |
| `datastream_channels` | `make testacc-extended-discovery DISCOVERY_TEST=datastream_channels` | An existing channel ID as `CATALYST_TEST_DATASTREAM_CHANNEL_ID`, plus `ZohoCatalyst.datastreams.channel.READ`. |

## Managed lifecycle gates

Run one fixture group at a time. After an interrupted write, reconcile only IDs
created by the current run. Keep borrowed fixtures and unrelated projects.

| Resources | Existing test targets | Additional prerequisites or coverage gaps |
| --- | --- | --- |
| `project` | `make testacc-project` | Separate project-creation eligibility and explicit create/delete opt-in. Creation in the console establishes the fixture, while this test measures whether the API supports unattended creation. |
| `function` | `make testacc-function`, `make testacc-function-variants`, `make testacc-function-background` | Generated packages for the advertised Node/runtime types. Use the Function execution test separately for runtime evidence. |
| `appsail` | `make testacc-appsail`, `make testacc-appsail-languages`, `make testacc-appsail-health` | Generated deployment packages and supported runtimes. Node 24 has an additional `TestAccAppSailNode24` case. |
| `webhook_cron`, `function_cron`, `appsail_cron` | `make testacc-cron`, `make testacc-webhook-cron-schedules`, `make testacc-function-cron`, `make testacc-function-cron-schedules`, `make testacc-appsail-cron` | Existing job pools for the target type and generated Functions or AppSail services. Recheck disabled, timezone, calendar and periodic variants as advertised. |
| `api_gateway`, `api_gateway_ruleset` | `make testacc-gateway` | Dedicated project for full ruleset ownership. Separately verify client routes with a hosted client. |
| `stratus_object` | `make testacc-stratus-object`, `make testacc-stratus-multipart` | Private unversioned bucket for generated keys. Check bytes and metadata after repair, then verify exact-key cleanup. |
| `datastore_row`, `nosql_item` | `make testacc-datastore-row`, `make testacc-nosql-item` | Isolated existing schemas and test rows/items. |
| `auth_user_role` | `make testacc-auth-user-role` | Existing disposable user and appropriate primary/fallback roles. The test does not invite or delete a user. |
| `filestore_file` | `make testacc-file` | Existing legacy service and folder. Record availability separately. |

Run `make testacc-full-stack` after the supporting service gates. It uses an
isolated AppSail application and existing table and bucket fixtures.
