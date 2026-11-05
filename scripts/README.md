# Developer scripts

## System Overview

These scripts support local provider setup, the public trial and protected
regional acceptance checks. Run the commands below from the repository root.

## Local Interface & Invariants

| Entry point | Purpose |
| --- | --- |
| `sh scripts/setup-dev.sh` | Writes the local provider override. Use `make dev` to build the provider first. See [setup-dev.sh](./setup-dev.sh), `repo_dir`. |
| `python3 scripts/try-provider.py check` | Builds the provider and validates the trial examples without Catalyst API calls. Its `main` also accepts `read` (the default), `plan`, `apply` and `destroy`; see [try-provider.py](./try-provider.py). |
| `python3 scripts/run-regional-check.py --help` | Shows the required region/environment flags and optional `read-only` or `stratus-lifecycle` mode. Actual runs launch live acceptance tests; see [run-regional-check.py](./run-regional-check.py), `main`. |

## Subdirectories & System Boundaries

There are no source subdirectories. The trial uses the checked-in quickstart
examples; regional checks invoke tests in `internal/catalyst`. First-time testers
should follow the [trial guide](https://github.com/Edson-Edwin/terraform-provider-catalyst/blob/main/examples/quickstart/README.md).

## Global Invariants

- `try-provider.py:account_environment` requires Development and rejects mixed
  or incomplete OAuth credentials. `child_environment` removes inherited Terraform
  logging, injected arguments and workspace settings.
- `try-provider.py:deployment_inputs` binds a trial to its original data center,
  organization and project. Keep `.trial/` until cleanup succeeds.
- `run-regional-check.py:fixture_environment` isolates the selected regional
  fixtures and strips inherited provider credentials and test/logging variables.

## Global Side Effects

- `setup-dev.sh` overwrites `.terraformrc.local` in the repository root.
- `try-provider.py:prepare` builds into `.trial/bin` and writes local configuration,
  example copies and a ZIP. Go may download dependencies. Account-backed modes
  contact Catalyst; `apply` and `destroy` can create, update or remove the trial app.
- `run-regional-check.py:main` starts live Go tests. `stratus-lifecycle` adds object
  write/delete tests and needs the fixture's separate write authorization.

## System Verification

Run the scripts' existing offline tests without Catalyst credentials:

```sh
python3 -m unittest discover -s scripts -p 'test_*.py' -v
```

The repository's `make test` target runs the Go provider's offline race tests.
