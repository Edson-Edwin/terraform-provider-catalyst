# Try the Catalyst Terraform provider

This community provider is under development. You can try the source checkout now;
it is not yet published on the Terraform Registry. Use your own Catalyst account
and credentials. No credentials, state or account access are provided by the
maintainer. Project creation is experimental and is not part of this trial.

## Requirements

- Terraform 1.13 or newer, Go 1.25.8 or newer, and Python 3.9 or newer on your PATH.
- An internet connection for initial Go dependency downloads and live API requests.
- Your Catalyst organization ID and data center (`in`, `us`, `eu`, `au`, `ca`, `jp` or `sa`).
- For the optional deployment: an existing isolated Development project you own,
  with AppSail available. Live validation so far is limited to India Development.

On Windows, use `py -3` instead of `python3` if needed. Node.js is not required on
your machine: the small sample has no dependencies and runs on Catalyst's Node 24
runtime. The launcher builds the provider for your own operating system.

## 1. Download and check without account access

```sh
git clone https://github.com/Edson-Edwin/terraform-provider-catalyst.git
cd terraform-provider-catalyst
python3 scripts/try-provider.py check
```

This builds the provider, checks the Terraform schema handshake and validates the
two examples. It makes no Catalyst API calls. The first build may take a few minutes.
The launcher uses a checkout-local development override: skip `terraform init`
for these commands. No provider is downloaded from the Registry or installed in
your global Terraform configuration.

## 2. Set up your own OAuth access

Follow Zoho's [OAuth setup documentation](https://docs.catalyst.zoho.com/en/api/oauth2/generate-grant-token/)
to authorize your own account and obtain an access token. A Catalyst CLI login is
not used by this provider.

For the read-only trial, request only `ZOHOCATALYST.projects.Read`.
For the optional AppSail trial, add `ZohoCatalyst.appsail.Read`,
`ZohoCatalyst.appsail.Create`, `ZohoCatalyst.appsail.Update` and
`ZohoCatalyst.appsail.Delete`, matching the official CLI's scope names.
Have permission to read the selected project and manage its AppSail apps.

The launcher asks for data center, organization ID and a hidden access token. It
does not write credentials to disk or put them in command arguments. Access-token
mode cannot refresh expired tokens; obtain another token when needed. If you
already use refresh authentication, supply `CATALYST_CLIENT_ID`,
`CATALYST_CLIENT_SECRET` and `CATALYST_REFRESH_TOKEN` through your secret manager
or protected environment. Do not also set `CATALYST_ACCESS_TOKEN`. See the full
[authentication guide](../../docs/index.md#authentication-and-configuration).

## 3. Read-only account trial

```sh
python3 scripts/try-provider.py read
```

This runs a Terraform plan that lists projects visible to your OAuth identity.
It creates, updates and deletes no Catalyst resources. The plan contains your
project names and IDs; review it before sharing it publicly. An empty list means
the identity has no visible projects in the selected organization/data center.

## 4. Optional: deploy, update and remove a demonstration app

The following commands create one randomly named AppSail in your existing
Development project. The trial app serves only a constant demonstration page and
has public access enabled. Catalyst service usage may incur charges. The project
itself is never created or managed by this example.

Review the proposed changes first:

```sh
python3 scripts/try-provider.py plan --project-id YOUR_EXISTING_PROJECT_ID
```

Deploy after reviewing the plan; Terraform asks for confirmation:

```sh
python3 scripts/try-provider.py apply
```

Open the printed `demo_url` after the app has started. The launcher does not open
or invoke it automatically. Edit `examples/quickstart/site/index.js`, run the apply
command again and refresh the page to demonstrate a code update.

When finished, remove the trial app:

```sh
python3 scripts/try-provider.py destroy
```

Terraform asks for confirmation and deletes only the tracked AppSail. Keep the
same organization, data center, project and credentials with the required permissions.
The launcher remembers the generated app name and routing in `.trial/`; it refuses
to switch an existing trial to a different account routing or project.

## State and recovery

Keep `.trial/` until a successful destroy. It holds the Terraform state needed to
identify and clean up your app. It is excluded from Git, but still contains account
metadata and should remain private. Do not share a configured trial directory or
state files. The provider displays controlled errors; review any diagnostics before
posting them.

If an operation fails or is interrupted, retain `.trial/`, inspect the exact app
in your Development console, and run plan before retrying. Do not delete state or
change the generated app name to work around an error. If creation succeeded
remotely without a recorded ID, recover by importing that exact app as described
in the [AppSail resource documentation](../../docs/resources/appsail.md).

For a new independent trial after successful cleanup, use another checkout. Use
the normal examples and a shared state backend for team deployments; this launcher
is a local evaluation flow for one operator. For unsupported services and live-test
boundaries, see [API_COVERAGE.md](../../API_COVERAGE.md).
