---
page_title: "Function deployment and verification"
subcategory: "Functions"
description: "Function updates, operation budgets, ownership, recovery and runtime verification."
---

# Function deployment and verification

`catalyst_function` manages Node.js 22/24 Basic I/O, Advanced I/O, Event, Cron and
Job Functions. Use prebuilt ZIPs with matching `catalyst-config.json`, `package.json`
and entry-point files. The provider checks ZIP paths, sizes, corruption and the
planned SHA-256 before uploading code. It does not build dependencies or invoke
Functions during Terraform operations.

## Update behavior

| Change | Behavior |
| --- | --- |
| ZIP contents/hash | Upload code and configuration to the existing Function |
| Reapply a previous ZIP/configuration | Redeploy that artifact in place; this is not an atomic traffic switch |
| Node runtime | Update in place with a matching ZIP; verify runtime and artifact afterward |
| Memory or environment variables only | Send configuration without a ZIP; first require the existing artifact to match the planned hash |
| Remove environment variables | Send the complete desired map, including an explicit empty map |
| Source path with unchanged contents, or timeouts only | Update local state without deploying |
| Project, name or Function type | Replace the Function; review dependencies and deletion before applying |

The code-free upsert follows the official CLI's `functions:config` implementation,
including its separate Advanced I/O endpoint. A configuration update fails if its
saved identity no longer matches or the artifact changed since planning. Refresh
and review the new plan before retrying. Runtime changes still require a ZIP whose
embedded runtime matches the Terraform configuration. Catalyst documents runtime
version updates in its [Function implementation guide](https://docs.catalyst.zoho.com/en/serverless/help/functions/implementation/).

Deployment verification requires three consecutive matching configuration and
artifact observations. While configuration is stale, the waiter avoids downloading
the ZIP. Refresh downloads and hashes the archive to detect external code drift. An
observation matching saved state returns immediately; a changed observation must
repeat three times before replacing state. This reconciles stale replicas without
filtering out stable external changes. No authoritative remote checksum or
conditional-download contract has been established that would safely eliminate
this read.

## Operation budgets and recovery

```terraform
resource "catalyst_function" "example" {
  project_id    = var.project_id
  name          = "example_function"
  stack         = "node24"
  source        = var.function_zip
  source_sha256 = var.function_zip_sha256

  timeouts = {
    create = "10m"
    read   = "3m"
    update = "10m"
    delete = "3m"
  }
}
```

Defaults are five minutes for create/update and two minutes for read/delete.
Timeouts cover requests and nested polling; caller cancellation and shorter caller
deadlines take precedence. Positive Go durations are required. A timeout does not
cancel a server-side operation or automatically replay an ambiguous write.

A trusted created ID and routing scope are saved before deployment verification.
If verification fails, inspect that exact ID and refresh before retrying. If the
creation response was lost without an ID, inspect the generated name and explicitly
import the exact object after establishing ownership. Do not blindly repeat an
upsert. Permission errors and failed refreshes retain existing state.

## Ownership and security

Use one owning state and backend locking for each project/Function name. Other
states should consume its ID. Stop external deployment tools before Terraform
writes. Catalyst's name-keyed upsert has no established atomic conditional-create
or compare-and-swap contract. The name check and per-client lock cannot prevent a
concurrent independent writer. The artifact precheck reduces stale-write risk but
does not provide a distributed lock.

An explicit organization and the saved `routing_scope` bind resources to their
configured destination. See [environment isolation](environments.md) before
upgrading state or setting up Production. Environment-variable values remain in
Terraform state and saved plans despite being marked sensitive; protect both.
Function execution permissions, runtime profiles, logs and release promotion need
separate configuration. This resource does not implement versions, weighted aliases,
atomic release rollback, other language runtimes, or Production promotion.

## Repeatable verification

Run the offline test suite without credentials. For dedicated Development fixtures,
`make testacc-function-variants` and `make testacc-function-background` exercise
Terraform creation, import, configuration-only updates/clearing, code updates,
stable plans and deletion. The Node 22 cases additionally check a same-ID Node 24
upgrade. Each matrix checks its original Function ID inventory after cleanup.
If Terraform cleanup fails after a created ID has been recorded, a bounded fallback
uses a fresh client and deletes only that exact generated identity after confirming
it was absent from the starting inventory. The original test still fails. A lost
creation response or process termination can require manual recovery.

`make testacc-function-execution` is a separate, explicitly opted-in client/API gate.
It creates generated constant-output Basic I/O fixtures on Node 22 and Node 24,
checks runtime output before and after configuration/code changes and redeploying
the original artifact, and deletes only returned fixture IDs absent from the
starting inventory. Three consecutive
expected outputs are required. Only a recognized previous fixture output is retried;
arbitrary errors and ambiguous execution failures stop the check. Response bodies
are withheld from failure diagnostics. The gate never invokes existing application
Functions and does not run in Production.

Both `CATALYST_TEST_ALLOW_FUNCTION_CREATE=1` and
`CATALYST_TEST_ALLOW_FUNCTION_INVOKE=1` are required, along with explicit Development
routing, organization, project and one authentication mode. Missing prerequisites
fail the explicit live gate. See the
[contributor instructions](https://github.com/Edson-Edwin/terraform-provider-catalyst/blob/main/CONTRIBUTING.md)
for secure fixture injection and scopes.

Basic I/O execution evidence does not establish Advanced I/O request handling,
Event/Cron/Job trigger delivery, other regions or Production readiness. The initial
live runtime mismatch and subsequent results are recorded in
[verification evidence](https://github.com/Edson-Edwin/terraform-provider-catalyst/blob/main/API_COVERAGE.md).
Hosted CI remains blocked by billing; local test success does not imply hosted CI
success or feature parity with AWS Lambda.
