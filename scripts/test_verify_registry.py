"""The public installation check must not fall back to locally built binaries."""
import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('registry', Path(__file__).with_name('verify-registry.py'))
registry = importlib.util.module_from_spec(spec)
spec.loader.exec_module(registry)


class RegistryTests(unittest.TestCase):
    def test_rejects_non_exact_versions(self):
        for version in ['latest', 'v1.0.0', '1.0', '1.0.0-ALPHA', '../1.0.0', '01.0.0']:
            with mock.patch.object(registry.subprocess, 'run') as run:
                with self.assertRaises(ValueError):
                    registry.verify(version)
                run.assert_not_called()

    def test_isolated_direct_install(self):
        def run(args, **kwargs):
            root = kwargs['cwd']
            config = (root / 'terraform.rc').read_text()
            self.assertIn('direct', config)
            self.assertNotIn('mirror', config)
            self.assertNotIn('dev_overrides', config)
            for key in ['CATALYST_ACCESS_TOKEN', 'TF_PLUGIN_CACHE_DIR', 'TF_DATA_DIR', 'TF_CLI_ARGS', 'TF_REATTACH_PROVIDERS']:
                self.assertNotIn(key, kwargs['env'])
            if args[1] == 'init':
                (root / '.terraform.lock.hcl').write_text('version = "1.2.3"\nhashes = ["zh:fixture"]')
            schema = {'provider_schemas': {'registry.terraform.io/edson-edwin/catalyst': {
                'resource_schemas': {'catalyst_webhook_cron': {}, 'catalyst_filestore_file': {}, 'catalyst_function': {}, 'catalyst_appsail': {}},
                'data_source_schemas': {name: {} for name in ['catalyst_project', 'catalyst_projects', 'catalyst_job_pools',
                    'catalyst_datastore_tables', 'catalyst_filestore_folders', 'catalyst_cache_segments', 'catalyst_stratus_buckets', 'catalyst_functions']}}}}
            return subprocess.CompletedProcess(args, 0, stdout=json.dumps(schema), stderr='')
        with mock.patch.dict(registry.os.environ, {'TF_REATTACH_PROVIDERS': 'injected', 'CATALYST_ACCESS_TOKEN': 'secret'}), \
             mock.patch.object(registry.subprocess, 'run', side_effect=run) as execute:
            registry.verify('1.2.3')
            self.assertEqual(execute.call_count, 2)
