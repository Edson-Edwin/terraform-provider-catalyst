# Trial guide for testers

This community provider is under development. You can try the source checkout now;
it is not yet published on the Terraform Registry. Use your own Catalyst account
and credentials. No credentials, state or account access are provided by the
maintainer. Project creation is experimental and is not part of this trial.

The provider runs on your computer. You do not need to host it, sign up for the
Terraform Registry or wait for a production release. Start with the offline check;
continue to account access and deployment only when you want to test those steps.

## What you can test

| Stage | What it checks | Expected result |
| --- | --- | --- |
| `check` | Local build, Terraform provider schema and example configuration | Command exits successfully without Catalyst credentials or API calls |
| `read` | OAuth access and project discovery | Terraform displays the visible project IDs and names, or an empty list |
| `plan` | Proposed AppSail deployment | Terraform proposes one AppSail in your selected existing Development project |
| `apply` | AppSail creation and deployment | Terraform completes and prints a `demo_url` to open after startup |
| Edit and `apply` | Application code update | The demonstration page shows your change after deployment and refresh |
| `destroy` | AppSail removal | Terraform removes the tracked trial app; your existing project remains |

This is a focused first trial, not verification of every resource in the provider.
For broader testing, use the [all-services instructions](../all_services/README.md)
and check [coverage and live-test limits](../../API_COVERAGE.md) first. Some services
need existing tables, buckets or other prerequisites. The trial launcher accepts
Development only; it refuses Production.

## Requirements

- Git to download the repository.
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

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `git`, `go`, `terraform` or Python is not found | Install the required tool, check its version and reopen your terminal so PATH is refreshed. On Windows, try `py -3` for Python. |
| First build takes a while or dependency downloads fail | The first run downloads Go modules. Check your internet or proxy settings and retry `check`. |
| Terraform tries to install the provider from the Registry | Use the launcher commands in this guide and skip `terraform init`; the preview builds a local provider. |
| Authentication fails | Check your data center, organization, OAuth scopes and permissions. A Catalyst CLI login alone does not configure the provider. Replace an expired access token. |
| Credential configuration is rejected | Choose either an access token or the complete client ID, client secret and refresh-token set. Do not mix the two modes. |
| No projects are listed | Check the selected organization and data center and whether your OAuth identity can see the intended project. |
| Deployment or cleanup fails | Keep `.trial/`, confirm the selected project and required AppSail permissions, and follow the state and recovery instructions above. |
| New-project creation returns `PROJECT_CREATION_NOT_ALLOWED` | Use an existing Development project for this trial. The latest live creation test was rejected in the tested India OAuth context; project creation is still experimental. |

## Record and share your results

Use this checklist to show which steps you actually tested:

- [ ] Offline `check` completed.
- [ ] Read-only project lookup completed.
- [ ] AppSail plan showed the intended Development project and one trial app.
- [ ] AppSail apply completed and the demonstration URL responded.
- [ ] Editing `examples/quickstart/site/index.js` and applying again updated the page.
- [ ] Destroy completed and the trial AppSail was removed.

You can share your findings with the person who invited you or
[open a GitHub issue](https://github.com/Edson-Edwin/terraform-provider-catalyst/issues/new).
Include the tested commit (`git rev-parse HEAD`), operating system, Terraform/Go/Python
versions, data center, command or stage, expected result and actual result. Mark
untested stages as not tested. Include error text only after removing sensitive
details. Do not attach OAuth credentials, `.trial/`, state files or saved plans.
Report sensitive vulnerabilities through the [security policy](../../SECURITY.md).
