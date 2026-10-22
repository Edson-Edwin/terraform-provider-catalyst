# Catalyst API coverage and implementation plan

This community provider is under development. Public availability and broad service coverage remain goals; implemented capabilities are listed below. API availability must be verified separately from the Terraform schema and mock tests.

## Implemented and verified

| Terraform type | Capability | Verification |
| --- | --- | --- |
| `data.catalyst_project` | Read a project by ID | Offline protocol/client tests; live India access-token and refresh-token reads |
| `data.catalyst_projects` | List projects visible in the selected organization | Offline protocol/client tests; live India listing and stable Terraform plan |
| `data.catalyst_job_pools` | List a project's existing scheduling pools | Offline populated/empty/error cases; live India empty and populated listings with stable plans |
| `catalyst_webhook_cron` | Manage pre-defined CronExpression schedules with Webhook targets | Offline lifecycle/import/drift/error tests; live India create, import, update, clear, stable plan and verified destroy |

All data sources are read-only. The webhook cron is the first managed resource. Live tests are opt-in; credentials and account IDs are never included in repository fixtures.

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
