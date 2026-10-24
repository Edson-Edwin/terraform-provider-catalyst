---
page_title: "Project ownership and recovery"
subcategory: "Projects"
description: "Safe project ownership, visibility recovery, supported settings and live verification prerequisites."
---

# Project ownership and recovery

`catalyst_project` manages creation, reads, import and deletion through the
documented project API. Project deletion affects all contents, including Production
applications. The resource remains experimental until the live create-to-delete
gate passes in an eligible account.

## One state owns each project

Give each project exactly one owning resource address in one shared state. All
operators and CI jobs managing that project must use that same backend and state
key/workspace, with backend locking enabled. Keep credentials outside configuration
and restrict access to state and write-capable credentials. Do not use `-lock=false`.

Other stacks should receive the project ID as an input and use
`data.catalyst_project` when they need metadata. They must not import the same ID
into another `catalyst_project` resource. Before migrating ownership, stop writers,
verify the ID and scope, and remove the old binding without destroying the remote
project before importing it into the destination state. Do not resume writers
until the destination plan has been reviewed.

Terraform [expects one resource binding per remote object](https://developer.hashicorp.com/terraform/language/state/purpose).
[Backend locking](https://developer.hashicorp.com/terraform/language/state/locking)
coordinates operations on that state. Different state keys have different locks;
locking does not prevent duplicate imports into independent states. The reviewed
Catalyst REST/CLI contracts do not establish an atomic ownership claim or a
conditional project-create primitive. A local mutex or a client-side name check
cannot provide a distributed ownership guarantee.

## Bind routing before managing a project

Configure the project's explicit organization and data center. Credentials can
come from the documented environment variables.

```terraform
provider "catalyst" {
  data_center     = "in"
  organization_id = var.organization_id
  environment     = "Development"
}
```

The resource stores the selected organization and data center in state. A later
provider change to either value fails before API requests. This prevents accidental
redirection; it does not verify caller identity or project transfer authorization.
Development and Production belong to the same project, so changing the environment
does not limit the effect of whole-project deletion.

For legacy state, configure the original routing, run `terraform plan -refresh-only`,
inspect the proposed scope fields and apply the reviewed refresh-only plan. Update
and deletion reject state that has not yet acquired a scope binding. Import creates
a binding after a successful read using the selected routing.

## Recover lost visibility

Missing access must not authorize recreation. The provider keeps state when a
project returns `404 INVALID_ID` and is absent from repeated successful listings.
It also fails deletion preflight instead of treating that observation as a successful
destroy. A valid delete acknowledgement for the exact ID is required before the
current operation can accept disappearance as completion.

1. Restore the original data center, organization and credentials. Check project
   membership and permissions with an administrator, then refresh again.
2. If the administrator confirms actual deletion, inspect the exact Terraform
   address and saved project ID. Back up state securely and remove only that binding
   with `terraform state rm '<exact-resource-address>'`. This command does not
   delete a remote project. The next plan can propose creation; review it explicitly.
3. If deletion may have succeeded after a lost response, follow the same independent
   verification process. Do not bypass the visibility check or guess ownership from
   a matching name.

See HashiCorp's [state removal documentation](https://developer.hashicorp.com/terraform/cli/commands/state/rm).

## Project name and timezone

Name changes require replacement. Apply `deletion_protection = false` separately
before an intentional replacement, and review the impact on all child resources.
An in-place project rename API has not been verified in the reviewed REST reference
or installed CLI.

Catalyst documents changing the project timezone through
[General Settings](https://docs.catalyst.zoho.com/en/getting-started/set-up-a-catalyst-project/general-settings/).
The reviewed contracts do not establish a supported REST timezone-update operation.
The provider therefore reads `timezone` and does not send speculative PUT/PATCH
requests. A Terraform postcondition can enforce your required value after an
administrator configures it:

```terraform
data "catalyst_project" "existing" {
  id = var.project_id

  lifecycle {
    postcondition {
      condition     = self.timezone == var.required_timezone
      error_message = "Set the required project timezone in Catalyst General Settings."
    }
  }
}
```

This detects a mismatch; it does not modify the timezone. Cron resources have their
own supported timezone settings, which are separate from the project timezone.

## Unblock live project creation

On 2026-10-24 a fresh access token from the saved India CLI login authenticated
successfully and the live Terraform creation request in its sole visible
organization again returned HTTP 400 `PROJECT_CREATION_NOT_ALLOWED`. The project
ID inventory was unchanged after the attempt. Confirm REST
creation eligibility, selected organization and project quota with the Catalyst
administrator or support. The [getting-started guide](https://docs.catalyst.zoho.com/en/getting-started/catalyst-projects/)
requires the first project to be created through the console before CLI creation;
that documented CLI prerequisite is not a proven explanation for the recorded
REST failure in an account that already has projects.

Use credentials with `ZOHOCATALYST.projects.CREATE`, `.Read` and `.Delete`, securely
injected into the local environment. Run `make testacc-project` from the provider
source checkout with explicit region, organization, Development routing and
`CATALYST_TEST_ALLOW_PROJECT_CREATE=1`. The gate uses a newly generated empty project,
checks Terraform import/protection/refresh/stable plans, deletes its exact created
ID and compares the project ID inventory with its starting baseline. Repeat with
each authentication mode separately before claiming both modes are verified.

Offline tests, a console-created fixture and a configured CI workflow do not replace
a successful authenticated lifecycle run. Hosted CI is separately blocked by billing.

When asking your administrator or Catalyst support for help, provide the region,
organization ID and this non-secret diagnostic: authenticated project listing
succeeds, but `POST /baas/v1/project` for a unique empty project returns HTTP 400
`PROJECT_CREATION_NOT_ALLOWED`. Ask which account/client prerequisites permit
creation and whether a supported project rename/timezone mutation API or atomic
ownership mechanism exists. Never include access tokens, refresh tokens or client
secrets in the request.
