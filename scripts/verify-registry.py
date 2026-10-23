#!/usr/bin/env python3
"""Install an exact public release from Terraform Registry in an isolated directory.

Run after publication. Requires network access and Terraform, but no Catalyst credentials.
Never applies resources or uses development overrides, mirrors, or shared plugin caches.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile


def verify(version):
    if not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9a-z]+(?:[.-][0-9a-z]+)*)?', version):
        raise ValueError('Use an exact lowercase semantic version without a leading v')
    with tempfile.TemporaryDirectory(prefix='catalyst-registry-check-') as tmp:
        root = Path(tmp)
        config = root / 'terraform.rc'
        config.write_text('provider_installation {\n direct {\n include = ["registry.terraform.io/edson-edwin/catalyst"]\n }\n}\ndisable_checkpoint = true\n')
        (root / 'main.tf').write_text('terraform {\n required_providers {\n catalyst = { source = "Edson-Edwin/catalyst", version = ' + json.dumps(version) + ' }\n }\n}\n')
        # These variables can bypass provider discovery, redirect state, inject
        # arguments, or write logs. None belongs in an isolated public install.
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('TF_', 'CATALYST_'))}
        env.update(TF_CLI_CONFIG_FILE=str(config), CHECKPOINT_DISABLE='1', TF_IN_AUTOMATION='1')
        subprocess.run(['terraform', 'init', '-backend=false', '-input=false', '-no-color'],
                       cwd=root, env=env, capture_output=True, text=True, check=True, timeout=180)
        lock = (root / '.terraform.lock.hcl').read_text()
        if not re.search(r'version\s*=\s*"' + re.escape(version) + '"', lock) or 'zh:' not in lock:
            raise ValueError('Installed provider version or archive checksums are missing from the lock file')
        result = subprocess.run(['terraform', 'providers', 'schema', '-json'],
                                cwd=root, env=env, capture_output=True, text=True, check=True, timeout=60)
        provider = json.loads(result.stdout)['provider_schemas']['registry.terraform.io/edson-edwin/catalyst']
        if not {'catalyst_webhook_cron', 'catalyst_filestore_file', 'catalyst_function', 'catalyst_appsail'} <= provider.get('resource_schemas', {}).keys():
            raise ValueError('Published package is missing required resources')
        expected = {'catalyst_project', 'catalyst_projects', 'catalyst_job_pools', 'catalyst_datastore_tables',
                    'catalyst_filestore_folders', 'catalyst_cache_segments', 'catalyst_stratus_buckets', 'catalyst_functions'}
        if not expected <= provider.get('data_source_schemas', {}).keys():
            raise ValueError('Published package is missing required data sources')
    print(f'Public Registry installation and schema verified for {version}. No Catalyst API calls or resources were created.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('version')
    args = parser.parse_args()
    try:
        verify(args.version)
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        # Do not print arbitrary Terraform output, which may contain environment details.
        parser.exit(1, 'Registry verification failed (' + type(error).__name__ + '). Verify publication, network access and the exact version.\n')
