# Catalyst API coverage and implementation plan

This community provider is under development. Public availability and broad service coverage remain goals; implemented capabilities are listed below. API availability must be verified separately from the Terraform schema and mock tests.

## Implemented and verified

| Terraform type | Capability | Verification |
| --- | --- | --- |
| `data.catalyst_project` | Read a project by ID | Offline protocol/client tests; live India access-token and refresh-token reads |
| `data.catalyst_projects` | List projects visible in the selected organization | Offline protocol/client tests; live India listing and stable Terraform plan |
| `data.catalyst_job_pools` | List a project's existing scheduling pools | Offline populated/empty/error cases; live India empty listing and stable Terraform plan |

All data sources are read-only. There are no managed resources yet. Live tests are opt-in; credentials and account IDs are never included in repository fixtures.

## API findings

Reviewed on 2026-10-23 against the [official Catalyst REST reference](https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/create-new-project/), the installed Catalyst CLI implementation, and the live India API.

- **Projects:** the reference documents create, read, list and delete. A request to create a uniquely named temporary project returned HTTP 400 with `PROJECT_CREATION_NOT_ALLOWED` in the test account. No project was created. This establishes a restriction for that account/authentication context, not a universal conclusion for all accounts. A supported project rename endpoint was not verified. Keep project creation out of the provider until supported OAuth access and full cleanup can be demonstrated.
- **Job pools:** the published reference documents list and get endpoints, with `ZohoCatalyst.jobpool.READ`. Create/update/delete pool endpoints were not found in the reviewed reference. Pool management is therefore not implemented. The test project currently has no pools.
- **Crons:** the reference documents create, read, update and delete under `/baas/v1/project/{project_id}/job_scheduling/cron`, plus enabled/disabled status. This is the next managed-resource candidate. It needs an existing compatible job pool and an appropriate target. No cron lifecycle has been tested or implemented yet.
- **Other services:** File Store folders, Data Store table schemas and other infrastructure require a separate management API review. A data-operation API (for example inserting rows or executing a function) does not by itself establish a supported infrastructure lifecycle.

## Next implementation gates

1. Identify or create an isolated test setup using a supported method: a development project, a compatible job pool, and OAuth access to cron create/read/update/delete.
2. Verify a **disabled** temporary cron's create/read/update/delete responses, disappearance behavior, and field normalization. Do not execute jobs during lifecycle testing. Delete only resources created by that test.
3. Implement a typed cron schema, import, drift handling and credential-safe error diagnostics. Test authorization failures separately from absence so Terraform does not discard state on permission errors.
4. Run offline tests and opt-in live lifecycle tests, including import, an update, a stable plan, and verified cleanup, before describing the resource as live-tested.
5. Expand coverage service by service with the same API and lifecycle checks.

## Public release gates

The repository must be public and the release process must produce versioned provider binaries with signed checksums. Configure the release signing secrets, verify the release workflow, register the provider with the Terraform Registry, and document tested service coverage before announcing public installation. Repository creation and local tests alone do not complete publication.
