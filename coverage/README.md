# Capability inventory and operation audit

The [capability inventory](capabilities.json) maps implemented Terraform types to
known persistent Catalyst capabilities. The [operation audit](audit.json) adds
priorities, create/read/update/delete/import status, API evidence, blockers,
remaining field gaps and a next action for every inventory entry.

Run from the repository root, without Catalyst credentials:

```sh
make coverage-status
make coverage-audit
go run ./internal/cmd/provider-tools coverage-audit -json
```

The JSON command writes the validated report to standard output. Redirect it to a
local file if needed. These commands read repository files only: they do not run
acceptance tests, use account credentials or contact source URLs.

## Interpret the classifications

- `implemented` describes provider code, not proof that the API accepts the
  operation in every account/region. Project creation is implemented but rejected
  in the tested account; its eligibility and verification blockers remain open.
- `replacement` describes project name changes. Local deletion-protection updates
  do not rename the remote project.
- `reset_on_destroy` means restoring settings rather than deleting the project,
  Gateway service or application user. The resource documentation defines the
  actual reset/fallback behavior.
- `not_implemented` describes a missing Terraform operation. It does not assert
  that Catalyst lacks the feature or that a usable management API exists.
- `api_contract` describes the available reference and may cover read operations
  only. SDK table/bucket helpers do not by themselves establish provisioning APIs.
- `source_reviewed` entries have targeted source checks; `inventory_only` entries
  retain historical notes and still need a fresh API/field review. Neither status
  establishes a complete service audit.
- Blockers distinguish `api_contract`, `account_eligibility`, `implementation`,
  `live_fixture`, `live_verification` and `deprecation`. Several may apply together.
  Historical live results remain in `evidence`; a missing fixture means that a new
  run needs fresh setup, not that the earlier result has been invalidated.

`inventory_complete` stays false until the service and field audit is complete.
The 16 managed entries are subsets, and several entries share one Terraform
type. Use the [catalog review](catalog_review.md) for the upstream service map and
the first operation and field crosswalks.

## Maintain the audit

When adding or reclassifying a capability, update both JSON files, cite real local
source files or public HTTPS references, and record the remaining work explicitly.
Keep operation implementation separate from API availability and live evidence.
Use `P0` for the project-creation blocker, `P1` for existing-resource validation and
foundational provisioning contracts, `P2` for application/integration expansion,
`P3` for later service audits, and `P4` for legacy compatibility work.

The audit command validates membership, lifecycle classifications, metadata and
local source targets before reporting. It does not fetch public sources to prove
their contents. The existing provider coverage test checks the operation audit
and registered Terraform schema together; it rejects uncatalogued resources/data
sources or a stale audit after inventory changes. For reviewed response fields,
add `read_fields` with the API field, Terraform data source and dot-separated
computed attribute path. The schema test checks that mapped attributes exist,
including nested fields.
The existing CI validation job also runs these checks for configuration-only
changes, so editing the JSON files cannot silently skip the audit/schema guard.

See the [implementation roadmap](../docs/guides/provider_roadmap.md) for delivery
order and the [support request draft](../docs/guides/project_creation_support_request.md)
for the project-creation dependency.
The [live acceptance matrix](live_testing.md) maps registered Terraform types
to test commands and fixture prerequisites.
