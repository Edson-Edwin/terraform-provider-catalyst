---
page_title: "Service expansion: implemented features and remaining contracts"
subcategory: "Getting Started"
description: "Current AppSail, Stratus, database, authentication and scheduling/Gateway implementation status and verification evidence."
---

# Service expansion: implemented features and remaining contracts

This guide describes the supported management subsets and remaining prerequisites.
Counts in the draft capability inventory describe areas, not fully managed
services. Use the resource references for configurable arguments and import IDs.

## Implemented and verified

| Area | Added behavior | Evidence |
| --- | --- | --- |
| Data Store | `catalyst_datastore_row`: generated/imported configuration rows, scalar JSON, owned-column updates, explicit null clearing, drift and deletion | Offline lifecycle/precision/privacy tests; live India Development create/import/update/null-clear/stable-plan/destroy |
| Stratus | Optional multipart uploads using `multipart_part_size_mb = 5..20`; single upload remains default | Offline protocol/isolation/failure tests; live 6 MiB/two-part create/import/update/stable-plan/destroy with deletion verified |
| Gateway | Advanced I/O Function targets and combinations of OAuth, APIKey and CatalystUserManagement | Expanded live Terraform lifecycle including import, update, clear and stable plans; generated Functions removed and disabled Gateway restored |
| Gateway | Custom web client GET routes with no authentication | Official CLI/help contract and offline lifecycle only; existing hosted client required |
| AppSail metadata | Custom runtimes without a `stack` field no longer break project AppSail inventory | Observed live response and offline regression test; this is not container deployment management |
| Full-stack website | AppSail reads a Terraform-managed Data Store configuration row alongside NoSQL and private Stratus content, then exercises SDK row CRUD | Live India Development smoke passed; generated-resource deletion and independent row cleanup confirmed |

No route or Function was invoked during Gateway testing. The multipart and row
tests used generated data in dedicated existing prerequisites. No hosted CI or
Production changes were performed.

## Data Store row ownership

`values_json` owns its listed application columns. Removing a previously managed
column clears it to null; it does not drop the column. Unlisted columns are not
adopted. Import initially adopts all scalar application fields from that row.
System fields such as ROWID, creator profiles and timestamps are excluded from
the values JSON. Generated row IDs remain precision-preserving strings.

Strings, numbers, booleans and nulls are supported, within 256 KiB. Use the
canonical representation expected by each table column. Nested objects/lists,
file fields and application-owned customer transactions are outside this subset.
Values remain in sensitive Terraform state. A create whose response loses ROWID
is not retried automatically; inspect the table and import before retrying.

The implementation follows the official SDK v3.4.0 `updateRows` PATCH contract,
including null clearing, which passed live testing. The REST help page currently
shows PUT; it must not be used to infer that null clearing has the same behavior.
Table/schema/index/permission provisioning is not included.

Sources:

- [Insert rows](https://docs.catalyst.zoho.com/en/api/code-reference/cloud-scale/data-store/insert-new-row/)
- [Update rows API](https://docs.catalyst.zoho.com/en/api/code-reference/cloud-scale/data-store/update-row/)
- [Update rows SDK](https://docs.catalyst.zoho.com/en/sdk/nodejs/v2/cloud-scale/data-store/update-rows/)
- [Current JavaScript row SDK](https://docs.catalyst.zoho.com/en/sdk/javascript/v1/cloudscale/datastore/update-rows/)
- [Delete rows](https://docs.catalyst.zoho.com/en/api/code-reference/cloud-scale/data-store/delete-row/)

## Multipart transfer limits

Multipart transfer does not provision a bucket, enable versioning or configure
CORS/policies. Existing unversioned buckets and exclusive key ownership are still
required. The provider source/download limit remains 100 MiB; this is not support
for arbitrarily large streaming objects.

Mutations are not replayed after ambiguous failures. A part failure reports the
incomplete session's upload ID. A lost/uncertain completion keeps the object key
in Terraform recovery state. The inspected SDK exposes initiation, numbered
parts, summary and completion; a public abort/resume lifecycle has not been
established by this implementation. Do not assume failed multipart sessions were
aborted. Temporary signatures are not stored in state or diagnostics, and OAuth
headers are never forwarded to the Stratus transfer host.

[Official multipart upload contract](https://docs.catalyst.zoho.com/en/sdk/nodejs/v2/cloud-scale/stratus/upload-object/)

## Remaining work across the six requested areas

| Requested feature | Current status / missing requirement |
| --- | --- |
| AppSail containers | OCI upload and callback accepted in a generated isolated probe; configuration readback still exposed only environment/latest-deployment status after a one-minute wait, and artifact download returned HTTP 500. No reliable managed-container refresh/drift contract established. Generated service deletion confirmed. No claim that the image ran. |
| AppSail scaling settings | Catalyst automatically scales running AppSail services. This provider does not expose verified min/max/concurrency control APIs. Automatic platform scaling is not missing merely because explicit Terraform controls are absent. |
| AppSail custom domains | Supported platform feature, but a verified provider lifecycle and domain/DNS/TLS test fixture are still needed. |
| Production promotion | Provider environment selection is not a project promotion operation. A supported, scoped migration/deletion/rollback contract and isolated Production verification are still needed; no project was promoted. |
| Stratus bucket provisioning | Console creation is documented; no stable bucket creation/update/deletion ownership contract was established by this audit. |
| Stratus policies, CORS and versioning | Platform features exist. Verified management read/write/reset contracts and isolated lifecycle evidence are still needed; multipart transfer does not configure them. |
| NoSQL tables and indexes | Current help restricts management to the remote console; the provider reads existing metadata and manages configuration items. |
| NoSQL table TTL configuration | No table-management lifecycle implemented. Existing typed item JSON can contain expiry values when the table already has a configured TTL attribute. |
| Data Store schema, indexes and permissions | Row CRUD is implemented, but table/column/index/permission ownership and reset/deletion contracts remain unverified. Project-wide import/export is not proof of safe individual-resource CRUD. |
| Authentication login setup, custom roles/defaults and permissions | Not implemented; no supported public management lifecycle established. Existing-user role assignment remains experimental and live-unverified because no disposable user is available. |
| Scheduling | Webhook, Function and AppSail schedules are supported. Circuit scheduling lacks an eligible verified test project. Job-pool and broader configuration management remain incomplete. |
| Gateway | Expanded targets and authentication modes are available. Client routes have offline-only verification; configured throttling and system routes remain unsupported and are rejected rather than silently discarded. |

Missing provider management does not mean Catalyst lacks these features. The
implementation boundary is the verified API contract plus lifecycle/test evidence.
The authentication details are maintained in the
[Authentication and Connections guide](authentication_connections.md).

Sources for the remaining platform/contract boundaries:

- [AppSail deployment options and automatic scaling](https://docs.catalyst.zoho.com/en/serverless/help/appsail/introduction/)
- [OCI deployment through the CLI](https://docs.catalyst.zoho.com/en/serverless/help/appsail/custom-runtimes/deploy-from-cli/)
- [AppSail configuration](https://docs.catalyst.zoho.com/en/serverless/help/appsail/appsail-configurations/)
- [Stratus bucket creation](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/buckets/create-bucket/)
- [Stratus versioning/settings](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/stratus-config/general-settings/)
- [Stratus permissions](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/stratus-permissions/)
- [Stratus CORS](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/stratus-config/bucket-cors/)
- [NoSQL table management](https://docs.catalyst.zoho.com/en/cloud-scale/help/nosql/create-manage-tables/)
- [NoSQL TTL](https://docs.catalyst.zoho.com/en/cloud-scale/help/nosql/components/)
- [Data Store scopes and permissions](https://docs.catalyst.zoho.com/en/cloud-scale/help/data-store/scopes-and-permissions/)
- [Gateway targets, authentication and throttling](https://docs.catalyst.zoho.com/en/cloud-scale/help/api-gateway/key-concepts/)
- [Gateway JSON contract](https://docs.catalyst.zoho.com/en/cli/v1/working-with-api-gateway/api-gateway-json-file/)

The opt-in acceptance commands and prerequisites are in the repository's
`CONTRIBUTING.md` Live tests section.
The failed container contract probe is diagnostic evidence for a remaining gap,
not a passing container resource test. Local offline checks skip live probes.
