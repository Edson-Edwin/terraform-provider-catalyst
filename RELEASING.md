# Releasing the Catalyst provider

A release must describe the capabilities actually supported. Passing offline checks does not establish broad Catalyst service coverage or successful public installation.

## Public source preview

Source access and Terraform Registry publication are separate milestones. After
reviewing tracked files and the complete Git history for credentials, the source
repository can be public while Registry signing and registration remain pending.
Keep the README explicit that the provider is under development and unpublished.
Do not represent source installation as a signed binary release or Registry install.

The [guided quick-start](examples/quickstart/README.md) builds the checkout locally,
verifies the provider schema without account access, and offers separate read-only
and opt-in AppSail trial commands. Run `python3 scripts/try-provider.py check`
before sharing it. The default command only plans the read-only example; deployment
and deletion require explicit commands and Terraform's normal confirmation.

Before changing visibility, scan history and the current source with Gitleaks
using the checked-in `.gitleaks.toml`, inspect every finding and review the diff.
The sole exception recognizes a fixed public `all` enum in GNUmakefile; it must
not be expanded into a file-wide exclusion. Do not publish `.trial/`, state,
credentials, debug logs or local build artifacts as source.

## Before the first public release

1. Resolve the GitHub Actions account/billing limitation and obtain successful checks for the intended commit.
2. Review tracked files and history before making the intended source repository public. Keep account credentials and state private.
3. Enable and test private vulnerability reporting, or publish a working private reporting contact.
4. Create or select the maintainer's release-signing key. Store its private key as `GPG_PRIVATE_KEY` and its passphrase as `PASSPHRASE` in GitHub Actions secrets. Keep a secure backup and a revocation plan. Register the public key with the Terraform Registry. Do not place the private key in this repository.
5. Verify the documented resource lifecycle with an isolated Development project. Report the tested region and any untested behavior in the release notes.
6. Run `make check`, `make lint`, `make generate` and `make release-check`. Review and include generated-document changes. Confirm dependency files and documentation remain unchanged after checks.

The release and test workflows pin GoReleaser v2.18.3 and golangci-lint v2.15.0. Documentation/package verification uses Terraform 1.16.5. Update these versions deliberately and repeat local checks.

## Build a candidate

Prepare a versioned changelog entry and ensure the exact intended commit passes checks. Use a semantic version tag such as `v0.1.0-alpha.1` (use lowercase prerelease identifiers) for the first limited preview. Do not tag an unreviewed working tree.

A tag starts the release workflow. It first calls the shared test workflow, including lint, dependency checks, generated-document checks, Terraform 1.13–1.16 tests and unsigned package verification. The signing job runs only after all those jobs succeed. It then verifies required inputs and creates a **draft** GitHub release with signed checksums. The workflow verifies the checksum signature after uploading the draft; a draft is not a public announcement.

The build uses the tagged source without running `go mod tidy` to rewrite dependencies. `go mod tidy -diff` checks both modules, and release builds use read-only dependency resolution. Actions use fixed commit SHAs.

## Verify and publish the draft

Download the draft's files into an empty directory. Verify that the release points to the intended commit and that its notes match the supported feature set. Check the public signing-key fingerprint independently, then run:

```sh
gpg --verify terraform-provider-catalyst_VERSION_SHA256SUMS.sig terraform-provider-catalyst_VERSION_SHA256SUMS
go run ./internal/cmd/provider-tools verify-release -version VERSION -signer-fingerprint FULL_TRUSTED_FINGERPRINT /path/to/downloaded-artifacts
```

Replace `VERSION` with the version without the leading `v`. The Go verifier checks SHA-256 hashes, the protocol-6 Registry manifest, all 14 configured platform archives, consistent version and executable names, local-mirror installation through `terraform init`, and the native provider's Terraform schema handshake. It needs Go and Terraform. Foreign platform binaries are cross-compiled and inspected, not executed by this check.

Only publish the draft after signature, artifact and lifecycle checks succeed. Complete provider registration at the Terraform Registry using the public repository and signing key. From a fresh directory with no development override, test `terraform init` using `Edson-Edwin/catalyst` pinned to the released version, then inspect its schema and run a read-only plan with test credentials. A successful local-mirror installation is not proof of public Registry installation or signature verification by the Registry.

## Failed or withdrawn releases

If checks fail, leave the draft unpublished and fix the problem in a new reviewed commit. Do not overwrite a version tag or replace already-published binaries. Use a new version for corrections and document affected behavior. Preserve old state compatibility or provide an explicit migration with tests. Follow GitHub/Registry procedures if a published release must be withdrawn.

The offline snapshot format uses lowercase `-dev.COMMIT` versions because Terraform filesystem-mirror discovery normalizes filename case. The snapshot manifest is staged under the upload name before checking its checksum. Windows/macOS native CI jobs are defined, but their runtime success must be established by hosted runs.

Local follow-up validation on 2026-10-23 exercised GoReleaser signing with a disposable test key, verified the checksum signature and mirror installation, and removed the key and signature afterward. This validates the local signing mechanics, not the maintainer identity, GitHub secrets or Registry trust setup. Filesystem-mirror layout follows the [Terraform CLI documentation](https://developer.hashicorp.com/terraform/cli/config/config-file#explicit-installation-method-configuration).

## Preparation without hosted CI

The maintainer has deferred hosted CI while resolving billing. Continue local implementation and validation with `make check`, `make lint`, `make generate`, and `make release-check`. This does not require a GitHub run. The release workflow remains configured to run checks when hosted execution is available; this task does not bypass that gate or publish anything.

The draft verification step now downloads the actual GitHub draft assets into a temporary directory, validates the detached signature against the imported maintainer fingerprint, checks every archive/manifest/version, and performs a native local-mirror install and schema handshake. It verifies the files a maintainer would publish, rather than just the build-directory signature.

For a manually downloaded candidate, import the maintainer public key from a trusted source and supply its full independently verified fingerprint to `provider-tools verify-release`. The verifier disables automatic key retrieval and rejects missing, invalid or wrong-key signatures before launching Terraform.

After the public repository, maintainer signing identity and Registry registration are ready, run:

```sh
go run ./internal/cmd/provider-tools verify-registry 0.1.0-alpha.1
```

Use the actual published version. This creates a fresh temporary Terraform directory with only direct Registry installation, strips inherited Terraform overrides/caches and Catalyst credentials, initializes the exact version, checks the lock file and validates the expected schema. It creates no cloud resources. No successful public installation is claimed until that check runs against a real release.

Optional read-only account preflight is available as `go run ./internal/cmd/provider-tools release-readiness`. It reports local changes, repository visibility, signing-secret names, private reporting and exact-HEAD hosted tests. Do not run it when intentionally deferring hosted checks. It cannot prove secret contents, signing-key ownership, Registry publication, or service coverage.

No new production signing key has been created in this task. Do not use the disposable local test key for a real release. Production key ownership, backups/revocation and Registry registration are maintainer-controlled steps.
