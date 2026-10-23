#!/usr/bin/env python3
"""Read-only GitHub/local preflight. Nonzero means at least one gate is unproven.

This does not publish, modify settings, read secret values or prove Registry installation.
"""
import argparse
import datetime
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent


def command(args):
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=60)
    if result.returncode:
        raise ValueError('Read failed or access is unavailable')
    return result.stdout.strip()


def github(path):
    return json.loads(command(['gh', 'api', path]))


def evaluate(repo, head, dirty, runs, secrets, security):
    checks = []
    def add(name, passed, detail):
        checks.append({'gate': name, 'passed': bool(passed), 'detail': detail})
    add('clean_source', not dirty, 'Commit the reviewed changes before tagging.' if dirty else 'Working tree is clean.')
    add('public_repository', repo.get('visibility') == 'public', 'Terraform Registry requires a public source repository.')
    # A previous successful run does not supersede a newer failed or pending rerun.
    matching = [r for r in runs if r.get('head_sha') == head and r.get('path') == '.github/workflows/test.yml' and r.get('event') == 'push']
    latest = max(matching, key=lambda r: (r.get('run_number', 0), r.get('run_attempt', 0)), default={})
    add('ci_for_head', latest.get('status') == 'completed' and latest.get('conclusion') == 'success', 'Require successful push checks for the exact local HEAD, including native OS tests.')
    names = {s.get('name') for s in secrets}
    add('signing_secret_names', {'GPG_PRIVATE_KEY', 'PASSPHRASE'} <= names, 'Checks repository secret names only; contents and signing identity still require release verification.')
    add('private_security_reporting', security.get('enabled') is True, 'Enable GitHub private vulnerability reporting or manually verify an alternative private contact.')
    return checks


def collect(repo_name):
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo_name):
        raise ValueError('Repository must be OWNER/NAME')
    head = command(['git', 'rev-parse', 'HEAD'])
    dirty = bool(command(['git', 'status', '--porcelain']))
    remote = github('repos/' + repo_name)
    # Filter server-side by SHA; no stale unrelated run can satisfy this gate.
    runs = github('repos/' + repo_name + '/actions/runs?head_sha=' + head + '&per_page=100')['workflow_runs']
    unknown = []
    try:
        secrets = json.loads(command(['gh', 'secret', 'list', '--repo', repo_name, '--json', 'name']))
    except ValueError:
        secrets = []
        unknown.append('signing_secret_names')
    try:
        security = github('repos/' + repo_name + '/private-vulnerability-reporting')
    except ValueError:
        security = {}
        unknown.append('private_security_reporting')
    checks = evaluate(remote, head, dirty, runs, secrets, security)
    for check in checks:
        if check['gate'] in unknown:
            check.update(passed=False, detail='Unverified: API read unavailable; do not interpret this as disabled or absent.')
    return {
        'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'repository': repo_name, 'head': head,
        'preflight_passed': all(c['passed'] for c in checks),
        'checks': checks,
        'remaining_manual_evidence': [
            'Review source and history for credentials before changing visibility.',
            'Verify signed draft downloads using the trusted maintainer fingerprint.',
            'Complete Registry registration and run verify-registry.py for the published version.',
            'Publish exact service and regional validation limits; preflight is not a coverage score.',
        ],
    }


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', default='Edson-Edwin/terraform-provider-catalyst')
    args = parser.parse_args()
    try:
        report = collect(args.repo)
    except (ValueError, KeyError, OSError, subprocess.TimeoutExpired):
        print(json.dumps({'preflight_passed': False, 'error': 'Read-only preflight could not complete. Check gh authentication, network access and repository permissions.'}))
        sys.exit(1)
    print(json.dumps(report, indent=2))
    sys.exit(0 if report['preflight_passed'] else 1)
