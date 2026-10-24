---
page_title: "Environment isolation and migration"
subcategory: "Configuration"
description: "Bind managed resources to their region, environment and organization, and migrate existing state safely."
---

# Environment isolation and migration

The provider selects Development or Production for service requests. It does not
create environments, promote releases, or roll back deployments. Changing the
provider's environment is not a deployment workflow.

## Saved routing

Each environment-scoped resource records a computed `routing_scope` containing
`data_center`, `environment` and `organization_id`. Reads, plans, updates and
deletes reject a different provider destination before resource API requests.
Creation plans also record routing, so changing environment variables between
plan and apply cannot silently redirect a new resource. A mismatch requires
restoring the original routing and making a new reviewed plan.

Every managed resource requires an explicit organization through `organization_id`
or `CATALYST_ORGANIZATION_ID`. Account defaults cannot safely identify a stable
destination. The binding records configured routing; it does not attest caller
identity, provide permissions, or prevent another state owning the same object.
Provider access preflight, when enabled, may run before resource scope checks.

The project resource is different: a Catalyst project includes Development and
Production. Its existing organization/data-center binding applies to the whole
project, and deleting it affects both environments.

## Upgrade existing state

Older state has no environment binding, so the provider cannot infer its original
environment. Before upgrading, stop other writers and securely back up state.
Restore each provider alias's original data center, environment, organization and
credentials. Confirm these against deployment records; do not combine this upgrade
with a routing change.

1. Set the explicit original organization, including for previously default-routed resources.
2. Run `terraform plan -refresh-only -out=scope.tfplan` under the original routing.
3. Review every new `routing_scope`, then apply the reviewed refresh-only plan.
4. Generate a normal plan before resuming changes. Updates and deletion refuse
   unbound state, including when refresh was skipped.

Only a successful read of a surviving resource binds legacy state. Failed reads
preserve the old state without claiming a verified binding. This initial adoption
still relies on the operator supplying the original destination. Imported resources
bind to the explicitly selected destination; normal import refresh validates access.
Partial creation keeps the routing binding with any returned recovery identity.

## Separate Development and Production

Use separate state keys/workspaces, access-controlled backends with locking, and
separately provisioned credentials for each environment. Give every remote object
one owning state. Do not change an existing state's provider environment to reuse it
for Production. Separate aliases can express different destinations within a stack,
but an alias alone is not a security boundary or separate state.

```terraform
provider "catalyst" {
  data_center              = var.data_center
  environment              = "Production"
  organization_id          = var.organization_id
  allowed_data_centers     = var.approved_data_centers
  allowed_environments     = ["Production"]
  allowed_organization_ids = var.approved_organization_ids
  validation_project_id   = var.project_id
}
```

Supply nonempty independently maintained allowlists and inject credentials securely.
Do not share credential environment variables between concurrently running jobs.
Preflight proves project access, not caller identity or every service permission.
Keep application secrets out of checked-in configuration; sensitive Terraform
attributes can still be stored in state and saved plans.

## Verification boundaries

Offline tests exercise routing guards and region/environment request isolation.
They do not prove that every service supports Production mutations or promotion.
Authenticated checks need dedicated per-region/per-environment fixtures described
in CONTRIBUTING.md. Run read-only authentication and Stratus checks first; enable
disposable-object writes only for explicitly designated fixtures. The write gate
verifies content integrity and cleanup but does not certify all service lifecycles.

Production promotion/rollback and authenticated account-identity attestation require
verified supported Catalyst contracts. They are not implemented by a routing header
or by changing a rating. See API_COVERAGE.md for recorded live results and limits.
