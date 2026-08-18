---
page_title: "Project creation support request draft"
subcategory: "Projects"
description: "Unsent request for Zoho clarification on unattended project creation."
---

# Project creation support request draft

Prepared on 2026-11-24. This draft has not been sent. Replace placeholders and
review diagnostics before sharing; include no credentials, cookies or private keys.

## Suggested subject

Supported API and OAuth requirements for unattended Catalyst project creation

## Message

Hello Catalyst team,

I am trying to create an empty Catalyst project through a direct HTTP request,
without a browser interaction. The project creation reference still documents
`POST /baas/v1/project`, but my latest test on 2026-11-01 in the India data center
returned HTTP 400 with `PROJECT_CREATION_NOT_ALLOWED` at the first creation step.
An earlier direct-HTTP attempt also referred to unsupported CLI project creation.
No new project was created by these attempts.

The request shape is:

```http
POST https://api.catalyst.zoho.in/baas/v1/project
Authorization: Zoho-oauthtoken <REDACTED>
Content-Type: application/json
CATALYST-ORG: <ORGANIZATION_ID>
Environment: Development

{"project_name":"<UNIQUE_TEST_PROJECT_NAME>"}
```

The published reference specifies `ZOHOCATALYST.projects.CREATE`. The official
JavaScript SDK initialization code works with an existing project ID, and I could
not find a project-provisioning method there.

Could you clarify:

1. Is this endpoint currently supported for unattended project creation? If a
   different public API or SDK method is supported, please share its contract.
2. Which OAuth client type, grant flow and scopes are required? Does the API
   require a particular organization-owner role, account entitlement, plan or
   regional enablement?
3. Does `PROJECT_CREATION_NOT_ALLOWED` indicate an eligibility restriction, or is
   project creation intentionally limited to console-assisted flows?
4. If API creation is supported, what readback/readiness and deletion contract
   should an automation client use to verify and clean up its own test project?

Related report:
https://github.com/catalystbyzoho/zcatalyst-cli/issues/1

Project creation reference:
https://docs.catalyst.zoho.com/en/api/code-reference/general/projects/create-new-project/

Thank you.

## Before sending

Add a request ID only if it was actually captured and reviewed. The recorded
2026-11-01 test did not establish that every OAuth client or account is affected.
Do not claim the modular SDK was tested as a creation workaround: its source was
inspected, but no creation method was available to execute.
