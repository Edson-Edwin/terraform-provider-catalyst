---
page_title: "Authentication, roles, permissions and Connections"
subcategory: ""
description: "Supported user-role assignment, integration prerequisites and remaining API requirements."
---

# Authentication, roles, permissions and Connections

The provider reads a specific application's user access metadata and has an
experimental resource for assigning an existing end-user to an existing role.
It does not configure authentication methods, create custom roles, edit table
permissions or provision Connections. These service areas are not 10/10.

## Available Terraform features

| Type | Purpose | Verification |
| --- | --- | --- |
| `data.catalyst_auth_user` | Read assigned role, account type and status | Offline API/Terraform tests; live fixture pending |
| `catalyst_auth_user_role` | Assign an existing user to an existing role | Offline lifecycle/import/drift/recovery/destroy tests; live writes pending |

IDs remain strings to preserve precision. Email, name, locale and other profiles
are not exposed in the schemas or stored in state. IDs and role metadata remain
in state. The client reads the email in memory because the official user-update
contract requires it.

The resource requires `ZohoCatalyst.projects.users.READ` and `.UPDATE`.
The data source requires only `.READ`. These are the Terraform operator's OAuth
scopes; they do not configure end-user permissions.

```terraform
resource "catalyst_auth_user_role" "editor" {
  project_id      = var.project_id
  user_id         = var.existing_test_user_id
  role_id         = var.existing_editor_role_id
  destroy_role_id = var.existing_app_user_role_id
}
```

Create attaches Terraform to an existing user's assignment without inviting a
user. Update changes the role in place. Refresh detects assignment drift. Destroy
assigns the explicit `destroy_role_id` fallback and verifies the result; it never
deletes the account or restores a profile. Keep the chosen least-privilege fallback
role available until destroy succeeds. Apply fallback changes before destroying.
Administrator and collaborator accounts are refused.

Import only reads the current assignment:

```sh
terraform import catalyst_auth_user_role.editor PROJECT_ID/USER_ID/FALLBACK_ROLE_ID
```

Use one resource/state per project/user across all tools: each user has one role,
and the documented update API has no conditional assignment operation. Permission
failures retain state. A user's HTTP 404 is treated as confirmed absence only when
its project remains readable; other missing-user error shapes fail closed.
Ambiguous writes are not automatically replayed. Failed creates after ownership
was recorded keep recovery state for refresh or fallback assignment on destroy.

## Authentication setup and role permissions

The official documentation describes hosted/embedded/third-party authentication,
social logins, signup, custom roles/defaults and table scopes/permissions through
console workflows. This audit did not establish supported public API lifecycle
contracts for those configurations. That is a provider implementation boundary;
Catalyst itself has these features.

Configure the chosen login method, roles and Data Store permissions in the console
before using the existing-user assignment resource. A role assignment does not
change the permissions attached to that role. These Terraform types send no user
invitations or password-reset emails.

Sources:

- [Authentication overview](https://docs.catalyst.zoho.com/en/cloud-scale/help/authentication/introduction/)
- [Role creation, editing and deletion](https://docs.catalyst.zoho.com/en/cloud-scale/help/authentication/user-management/roles/implementation/)
- [Table scopes and permissions](https://docs.catalyst.zoho.com/en/cloud-scale/help/data-store/scopes-and-permissions/)
- [Get a specific user API](https://docs.catalyst.zoho.com/en/api/code-reference/cloud-scale/authentication/get-specific-user/)
- [Current SDK user-update contract](https://docs.catalyst.zoho.com/en/sdk/javascript/v1/cloudscale/authentication/update-user-details/)

The role page contains conflicting historical text about deletion. Console
instructions alone are not treated as a supported role-delete API contract.

## Connections integration

Connections provisioning remains unimplemented. The current JavaScript SDK lists
one operational method, `getConnectionCredentials`, and restricts it to Catalyst
Functions/AppSail. The inspected official CLI/SDK and REST catalog did not establish
Connection-definition CRUD. The provider does not save changing Connection access
tokens in state or run browser login/consent during apply.

The available application integration is:

1. Create the default/custom service Connection in the Catalyst console.
2. Complete any required external-service authorization.
3. Pass the non-secret Connection link name as application configuration.
4. Deploy the application using `catalyst_appsail` or `catalyst_function`.
5. Retrieve and use credentials inside that application through the runtime SDK.

For JavaScript, install `@zcatalyst/connections`, configure its SDK authentication
context, and use the following inside the Catalyst runtime:

```javascript
const { Connections } = require('@zcatalyst/connections');

const connections = new Connections();
const credentials = await connections.getConnectionCredentials(
  process.env.CATALYST_CONNECTION_LINK_NAME
);
// Use credentials server-side. Do not log or return them to the browser.
```

This illustrates application code. It is not a Terraform Connection resource or
a live-tested integration with an external account. Java and Python SDKs also
offer runtime credential retrieval; application language is not restricted to Node.

Sources:

- [Connections introduction](https://docs.catalyst.zoho.com/en/cloud-scale/help/connections/introduction/)
- [Default service setup](https://docs.catalyst.zoho.com/en/cloud-scale/help/connections/establish-default-connection/)
- [Custom service setup](https://docs.catalyst.zoho.com/en/cloud-scale/help/connections/establish-custom-connection/)
- [Edit/revoke/delete workflow](https://docs.catalyst.zoho.com/en/cloud-scale/help/connections/manage-connections/)
- [Current JavaScript Connections SDK overview](https://docs.catalyst.zoho.com/en/sdk/javascript/v1/cloudscale/connections/overview/)
- [Runtime restriction and credential retrieval](https://docs.catalyst.zoho.com/en/sdk/javascript/v1/cloudscale/connections/get-auth-credentials/)
- [Java credential retrieval](https://docs.catalyst.zoho.com/en/sdk/java/v1/cloud-scale/connections/get-credentials/)
- [Python credential retrieval](https://docs.catalyst.zoho.com/en/sdk/python/v1/cloud-scale/connections/get-credentials/)
- [Runtime SDK authentication configuration](https://docs.catalyst.zoho.com/en/sdk/javascript/v1/auth-config/node-consideration/)

## Requirements for complete management

| Area | Missing evidence required for a supported resource |
| --- | --- |
| Authentication setup | Stable configuration read/write/reset contracts for login modes, signup, providers, domains and validation |
| Roles | Stable CRUD contract, clone semantics, default-role rules and user-transfer behavior |
| Permissions | Stable role/table permission read/write/reset contract, including row scopes and revocation |
| Connections | Stable definition CRUD, secret-update semantics, ownership/environment rules and separation from interactive consent |
| Live verification | Isolated fixtures and complete lifecycle/import/drift/recovery/cleanup evidence for every implemented subset |

No disposable application user was available during this milestone. Offline tests
do not prove live user-role assignment compatibility. The opt-in
`make testacc-auth-user-role` target is ready for later verification; see the
repository README for fixture IDs and required flags. No hosted CI was queried.
