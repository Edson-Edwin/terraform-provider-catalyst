#!/usr/bin/env python3
"""Build and try this checkout without publishing or storing OAuth credentials."""

import argparse
import getpass
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import zipfile


ROOT = Path(__file__).resolve().parent.parent
TRIAL = ROOT / ".trial"
EXAMPLES = ROOT / "examples" / "quickstart"
REGIONS = ("us", "eu", "in", "au", "ca", "jp", "sa")
REFRESH_KEYS = ("CATALYST_CLIENT_ID", "CATALYST_CLIENT_SECRET", "CATALYST_REFRESH_TOKEN")


def prompt_value(message):
    if not sys.stdin.isatty():
        raise ValueError("Supply data center, organization and project inputs through flags or environment for non-interactive use.")
    return input(message).strip()


def child_environment(source):
    """Exclude inherited Terraform logging, injected flags and workspace routing."""
    env = {
        key: value for key, value in source.items()
        if not key.startswith(("TF_LOG", "TF_CLI_ARGS", "TF_VAR_"))
        and key not in ("TF_DATA_DIR", "TF_WORKSPACE", "TF_PLUGIN_CACHE_DIR", "TF_REATTACH_PROVIDERS")
    }
    env["TF_CLI_CONFIG_FILE"] = str(TRIAL / "terraform.rc")
    env["CHECKPOINT_DISABLE"] = "1"
    return env


def account_environment(env, dc=None, org=None):
    env = env.copy()
    region = dc or env.get("CATALYST_DATA_CENTER") or prompt_value("Catalyst data center (in/us/eu/au/ca/jp/sa): ")
    organization = org or env.get("CATALYST_ORGANIZATION_ID") or prompt_value("Catalyst organization ID: ")
    if region not in REGIONS or not organization.isascii() or not organization.isdigit() or int(organization) <= 0:
        raise ValueError("Use a supported data center and a positive numeric organization ID.")
    if env.get("CATALYST_ENVIRONMENT", "Development") != "Development":
        raise ValueError("This trial only runs in Development. Unset CATALYST_ENVIRONMENT or select Development.")
    env.update(CATALYST_DATA_CENTER=region, CATALYST_ORGANIZATION_ID=organization, CATALYST_ENVIRONMENT="Development")
    access = bool(env.get("CATALYST_ACCESS_TOKEN"))
    refresh = [bool(env.get(key)) for key in REFRESH_KEYS]
    if access and (any(refresh) or env.get("CATALYST_REDIRECT_URI")):
        raise ValueError("Choose access-token OR refresh-token authentication; unset the unused credentials.")
    if any(refresh) and not all(refresh):
        raise ValueError("Refresh authentication requires client ID, client secret and refresh token.")
    if not access and not any(refresh):
        if env.get("CATALYST_REDIRECT_URI"):
            raise ValueError("Unset CATALYST_REDIRECT_URI before using access-token authentication.")
        if not sys.stdin.isatty():
            raise ValueError("Set OAuth credentials in the environment for non-interactive use.")
        env["CATALYST_ACCESS_TOKEN"] = getpass.getpass("Your Zoho OAuth access token (hidden; not saved): ")
        if not env["CATALYST_ACCESS_TOKEN"].strip():
            raise ValueError("An OAuth token is required; see examples/quickstart/README.md.")
    return env


def run(command, env, capture=False):
    # Commands contain paths and public flags only; credentials stay in child env.
    return subprocess.run(command, cwd=ROOT, env=env, check=True, text=True, capture_output=capture)


def prepare(env):
    if not shutil.which("terraform") or not shutil.which("go"):
        raise ValueError("Install Terraform >=1.13 and Go >=1.25.8, then rerun this command.")
    TRIAL.mkdir(mode=0o700, exist_ok=True)
    binary_dir = TRIAL / "bin"
    binary_dir.mkdir(exist_ok=True)
    binary = binary_dir / ("terraform-provider-catalyst.exe" if os.name == "nt" else "terraform-provider-catalyst")
    print("Building the provider from this checkout...", flush=True)
    run(["go", "build", "-mod=readonly", "-trimpath", "-o", str(binary), "."], env)
    config = ('provider_installation {\n  dev_overrides {\n'
              '    "registry.terraform.io/edson-edwin/catalyst" = '
              + json.dumps(str(binary_dir)) + '\n  }\n  direct {}\n}\n')
    (TRIAL / "terraform.rc").write_text(config, encoding="utf-8")
    for example in ("read-only", "appsail"):
        work = TRIAL / example
        work.mkdir(exist_ok=True)
        shutil.copyfile(EXAMPLES / example / "main.tf", work / "main.tf")
    with zipfile.ZipFile(TRIAL / "site.zip", "w", compression=zipfile.ZIP_DEFLATED) as bundle:
        for path in sorted((EXAMPLES / "site").iterdir()):
            if path.is_file():
                bundle.write(path, path.name)


def deployment_inputs(env, project_id, mode):
    path = TRIAL / "deployment.json"
    if path.exists():
        saved = json.loads(path.read_text(encoding="utf-8"))
        if saved["data_center"] != env["CATALYST_DATA_CENTER"] or saved["organization_id"] != env["CATALYST_ORGANIZATION_ID"]:
            raise ValueError("Use the original trial organization and data center; trial state is bound to that routing.")
        if project_id and project_id != saved["project_id"]:
            raise ValueError("Use the original project ID for this trial; destroy it before starting another trial.")
    else:
        if mode == "destroy":
            raise ValueError("No saved trial deployment exists in this checkout.")
        project_id = project_id or prompt_value("Existing isolated Development project ID: ")
        if not project_id.isascii() or not project_id.isdigit() or int(project_id) <= 0:
            raise ValueError("Use a positive numeric project ID.")
        saved = {"data_center": env["CATALYST_DATA_CENTER"], "organization_id": env["CATALYST_ORGANIZATION_ID"],
                 "project_id": project_id, "app_name": "tfpreview" + secrets.token_hex(8)}
        path.write_text(json.dumps(saved, indent=2) + "\n", encoding="utf-8")
    variables = {key: saved[key] for key in ("project_id", "app_name")}
    variables["source_zip"] = str(TRIAL / "site.zip")
    (TRIAL / "appsail" / "trial.auto.tfvars.json").write_text(json.dumps(variables, indent=2) + "\n", encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("check", "read", "plan", "apply", "destroy"), nargs="?", default="read")
    parser.add_argument("--data-center", choices=REGIONS)
    parser.add_argument("--organization-id")
    parser.add_argument("--project-id")
    args = parser.parse_args()
    env = child_environment(os.environ)
    prepare(env)
    if args.mode == "check":
        schema = run(["terraform", "-chdir=" + str(TRIAL / "read-only"), "providers", "schema", "-json"], env, capture=True)
        provider = json.loads(schema.stdout)["provider_schemas"]["registry.terraform.io/edson-edwin/catalyst"]
        for example in ("read-only", "appsail"):
            run(["terraform", "-chdir=" + str(TRIAL / example), "validate", "-no-color"], env)
        print(f"Offline check passed: {len(provider.get('resource_schemas', {}))} resource types and "
              f"{len(provider.get('data_source_schemas', {}))} data sources. No Catalyst API calls made.")
        return
    env = account_environment(env, args.data_center, args.organization_id)
    if args.mode == "read":
        print("Read-only: listing visible projects in your account. No resources will be created.", flush=True)
        run(["terraform", "-chdir=" + str(TRIAL / "read-only"), "plan", "-input=false", "-no-color"], env)
        return
    deployment_inputs(env, args.project_id, args.mode)
    print("Trial uses one public constant-response AppSail in your existing Development project. "
          "Service usage may incur charges. Keep .trial/ until cleanup completes.", flush=True)
    command = ["terraform", "-chdir=" + str(TRIAL / "appsail"), args.mode, "-no-color"]
    if args.mode == "plan":
        command.append("-input=false")
    run(command, env)
    if args.mode == "apply":
        print("Next: edit examples/quickstart/site/index.js and apply again, or run "
              "python3 scripts/try-provider.py destroy to remove the trial app.")
    elif args.mode == "destroy":
        (TRIAL / "deployment.json").unlink()
        print("Terraform destroy completed. Your existing Catalyst project was not managed by this trial.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError, KeyboardInterrupt) as error:
        if isinstance(error, subprocess.CalledProcessError):
            print("Trial command failed; review the Terraform diagnostic above. Retain .trial/ for recovery.", file=sys.stderr)
        elif isinstance(error, KeyboardInterrupt):
            print("Trial interrupted. Retain .trial/ and inspect the plan before retrying.", file=sys.stderr)
        else:
            print(str(error), file=sys.stderr)
        sys.exit(1)
