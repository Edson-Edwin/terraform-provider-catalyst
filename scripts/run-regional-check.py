#!/usr/bin/env python3
"""Map one protected CI environment's secrets to isolated regional fixtures."""

import argparse
import os
import subprocess


FIELDS = (
    "PROJECT_ID", "ACCESS_TOKEN", "CLIENT_ID", "CLIENT_SECRET", "REFRESH_TOKEN",
    "REDIRECT_URI", "ORGANIZATION_ID", "STRATUS_BUCKET", "STRATUS_OBJECT_KEY",
    "STRATUS_SHA256", "ALLOW_STRATUS_WRITES",
)


def fixture_environment(source, region, environment, mode):
    """Do not carry other fixtures or global provider credentials into a CI check."""
    result = {key: value for key, value in source.items()
              if not key.startswith(("CATALYST_", "TF_ACC", "TF_LOG"))}
    prefix = f"CATALYST_TEST_{region.upper()}_{environment.upper()}_"
    for field in FIELDS:
        value = source.get(f"CATALYST_FIXTURE_{field}", "")
        if "\n" in value or "\r" in value or "\x00" in value:
            raise ValueError(f"Fixture {field} contains unsupported control characters")
        result[prefix + field] = value
    result.update({
        "TF_ACC": "1",
        "CATALYST_TEST_REGIONS": region,
        "CATALYST_TEST_ENVIRONMENTS": environment,
        "CATALYST_TEST_REQUIRE_BOTH_AUTH_MODES": "1",
    })
    if mode == "stratus-lifecycle":
        result["CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES"] = "1"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--region", choices=("us", "eu", "in", "au", "ca", "jp", "sa"), required=True)
    parser.add_argument("--environment", choices=("Development", "Production"), required=True)
    parser.add_argument("--mode", choices=("read-only", "stratus-lifecycle"), default="read-only")
    args = parser.parse_args()
    try:
        env = fixture_environment(os.environ, args.region, args.environment, args.mode)
    except ValueError as error:
        parser.exit(2, f"{error}\n")
    # Always prove authentication and the known read fixture before write tests.
    pattern = "^TestAccRegional(Authentication|StratusRead)$"
    if args.mode == "stratus-lifecycle":
        pattern = "^TestAccRegional(Authentication|StratusRead|StratusLifecycle)$"
    command = ["go", "test", "-race", "-count=1", "-v", "-timeout=30m", "-run", pattern, "./internal/catalyst"]
    raise SystemExit(subprocess.run(command, env=env, check=False).returncode)


if __name__ == "__main__":
    main()
