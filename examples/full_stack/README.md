# Website deployment with existing database and bucket prerequisites

This example connects the provider's supported pieces. It is not automatic provisioning of an entire Catalyst account, nor a prebuilt website. Terraform validates existing Data Store/NoSQL schemas, manages one persistent NoSQL configuration item and a private Stratus asset, deploys your prebuilt AppSail website, and passes the verified database/storage identifiers to it. The application must implement the corresponding database/storage calls and its own routes.

Optionally set `datastore_config_values_json` to a nonempty scalar JSON object
for existing application columns, for example `jsonencode({ message = "site configuration" })`.
Terraform then manages that seed/configuration row and passes its generated ID
as `DATASTORE_CONFIG_ROW_ID`. Null (the default) skips row creation and passes an
empty ID. The application must read/use the row explicitly. Values remain in
sensitive state; removing a previously managed column clears its value to null.
This does not provision columns or manage changing customer transactions.

Before applying:

1. Create a Catalyst project, a Data Store table with the application's columns, an ONLINE NoSQL table with string keys `pk` and `sk`, and a private authenticated **unversioned** Stratus bucket in that project/environment. These are explicit manual prerequisites: table/schema and bucket provisioning are not implemented by the provider.
2. Build a ZIP containing your frontend and backend, installed dependencies, and runtime entry point. Listen on `0.0.0.0` and `X_ZOHO_CATALYST_LISTEN_PORT` (9000 by default). Catalyst-managed runtimes are supported; custom containers and Java WAR are not. Node defaults to `node24` / `node index.js`; Python and Java SE require their matching `stack` and `startup_command`.
3. Supply your own OAuth credentials, region and environment through `CATALYST_` variables. Include project/table metadata, NoSQL item, Stratus bucket/object/metadata, and AppSail permissions. No account IDs or credentials are embedded here.
4. Set the required Terraform variables: `project_id`, `datastore_table_id`, `nosql_table_id`, `bucket_name`, `application_zip`. Application environment values are sensitive but remain in Terraform state. Do not embed OAuth tokens in the application archive or pass provider credentials to the website.
5. Build the provider with `make dev` in the repository root and select its `.terraformrc.local` using `TF_CLI_CONFIG_FILE`. Run `terraform plan` and inspect the managed item/key before `terraform apply`. Skip Registry initialization while using the unpublished development provider.

`catalyst_auth` defaults to true. A private Stratus object does not become a public CDN asset; serve it through your authenticated backend or a separately reviewed delivery configuration. Public bucket policies, CORS, custom domains/TLS, database permissions, production promotion, scaling and a public frontend hosting service are not configured here. If you explicitly disable Catalyst authentication for a public website, implement any required application login and authorization.

Keep customer records, transactions, uploads and other changing application data outside Terraform. The configuration item and `website/site.json` key belong exclusively to this configuration. Destroy deletes that item, that object and the AppSail service; it leaves the manually created project, tables and bucket in place.

After apply, test the reported URL, frontend routes, backend API, database reads/writes, storage access and authentication with your application. A successful Terraform apply verifies deployment persistence; it does not by itself prove your application's runtime behavior or security. This combined configuration is locally validated. A separate generated Node24 application passed a live India Development smoke test: frontend HTML, AppSail backend, NoSQL configuration reads, private Stratus object download, and Data Store create/read/update/delete, followed by managed-resource and generated-row cleanup. This proves the tested integration with existing prerequisites; it does not prove automatic table/bucket provisioning or the behavior of your application.

## Repeat the isolated application smoke test

The opt-in Go acceptance test packages a tiny generated website and the official `zcatalyst-sdk-node` 3.4.0 test dependency. Install that pinned SDK in a temporary directory with npm lifecycle scripts disabled, then point `CATALYST_TEST_WEBSITE_NODE_MODULES` to its absolute `node_modules` path. Node/npm are needed only to prepare that optional Node application fixture, not to build or run the Go provider. The legacy SDK is used for this tested fixture; review Zoho's current SDK migration guidance for production applications.

Set `CATALYST_TEST_ALLOW_FULLSTACK_CREATE=1`, `CATALYST_TEST_PROJECT_ID`, `CATALYST_TEST_DATASTORE_TABLE_ID`, `CATALYST_TEST_NOSQL_TABLE_ID`, `CATALYST_TEST_BUCKET` and the normal provider credentials. The test requires India Development, a dedicated Data Store table named `TerraformProviderDataStoreTests` with an optional text column `message`, and a dedicated ONLINE NoSQL table named `TerraformProviderNoSQLTests` with string keys `pk`/`sk`. The existing bucket must be private/authenticated and unversioned. Run `make testacc-full-stack`.

The test deploys only generated code, guards the backend probe with a random temporary request key, reads only its own configuration/object and writes only generated Data Store rows. It never returns row values, SDK credentials or signed URLs. It removes the deployed application, item and object, confirms row cleanup independently, and leaves the reusable project/schema/bucket prerequisites in place.
