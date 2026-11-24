# Stratus preview test

This test exercises the provider from a fresh checkout against one disposable
object in an existing India Development project. It creates the object, verifies
Terraform import, changes its content and metadata, checks drift repair and an
unchanged plan, then destroys the tracked object and verifies that the key is gone.

## 1. Check the provider locally

Install Git, Go 1.25.8 or newer, Terraform 1.13 or newer, and Python 3.9 or newer.
Clone the preview branch and validate the provider locally.

```sh
git clone --branch development https://github.com/Edson-Edwin/terraform-provider-catalyst.git
cd terraform-provider-catalyst
git rev-parse HEAD
python3 scripts/try-provider.py check
```

The offline check builds the provider and validates the trial examples without
using a Catalyst account. On Windows you can use `py -3` in place of `python3`.

## 2. Prepare the Development fixture

Use your own isolated India Development project, an existing private Stratus
bucket with versioning disabled, and your own OAuth access token. Give the OAuth
identity permission to read the project and bucket and manage the test objects.
The trial uses `ZOHOCATALYST.projects.Read`, `ZohoCatalyst.buckets.READ`, and
`ZohoCatalyst.buckets.objects.READ`, `.CREATE`, `.UPDATE`, and `.DELETE`.
Zoho's guides explain [creating a bucket](https://docs.catalyst.zoho.com/en/cloud-scale/help/stratus/buckets/create-bucket/)
and [authorizing OAuth scopes](https://docs.catalyst.zoho.com/en/api/oauth2/generate-grant-token/).

## 3. Run the lifecycle

Set these variables in a fresh terminal with Terraform debug logging turned off.
Replace the three uppercase placeholders with your Development fixture values.
Use your shell's secure prompt or secret manager to set `CATALYST_ACCESS_TOKEN`.
Use the access-token authentication mode for this test.

```sh
export CATALYST_DATA_CENTER=in
export CATALYST_ENVIRONMENT=Development
export CATALYST_ORGANIZATION_ID=YOUR_ORGANIZATION_ID
export CATALYST_TEST_PROJECT_ID=YOUR_DEVELOPMENT_PROJECT_ID
export CATALYST_TEST_BUCKET=YOUR_PRIVATE_UNVERSIONED_BUCKET

TF_ACC=1 CATALYST_TEST_ALLOW_STRATUS_OBJECT_CREATE=1 \
  go test -count=1 -v -timeout=15m -run '^TestAccStratusObject$' ./internal/provider
```

Look for `PASS: TestAccStratusObject`. A skip does not count as a live pass. The test
uses a newly generated key and preserves the existing project and bucket. If the
test is interrupted, check the isolated bucket for any leftover `tfacc-` object
created by your run and verify its exact key before cleaning it up.

## 4. Report the result

Send the maintainer the commit SHA, operating system, Go and Terraform versions,
and the results of the offline check and live test. For a failure, include the
failing step and sanitized error. Keep OAuth tokens, state files, and account
identifiers private. Use the [security policy](../../SECURITY.md) for vulnerabilities.
