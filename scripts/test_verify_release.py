"""Exercise release rejection rules without executing an artifact."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import zipfile

spec = importlib.util.spec_from_file_location('verify_release', Path(__file__).with_name('verify-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)

TARGETS = ['linux_amd64', 'linux_arm64', 'linux_386', 'linux_arm', 'linux_s390x',
           'darwin_amd64', 'darwin_arm64', 'windows_amd64', 'windows_arm64', 'windows_386',
           'freebsd_amd64', 'freebsd_arm64', 'freebsd_386', 'freebsd_arm']


class ReleaseChecks(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix='catalyst-package-test-')
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)

    def fixture(self, version='1.0.0', mixed=False, manifest_version=1):
        files = []
        for index, target in enumerate(TARGETS):
            item_version = '1.0.1' if mixed and index == 0 else version
            archive = self.root / f'terraform-provider-catalyst_{item_version}_{target}.zip'
            binary = f'terraform-provider-catalyst_v{item_version}' + ('.exe' if target.startswith('windows') else '')
            with zipfile.ZipFile(archive, 'w') as package:
                package.writestr(binary, b'fixture, never execute')
            files.append(archive)
        manifest = self.root / f'terraform-provider-catalyst_{version}_manifest.json'
        manifest.write_text(json.dumps({'version': manifest_version, 'metadata': {'protocol_versions': ['6.0']}}))
        files.append(manifest)
        sums = self.root / f'terraform-provider-catalyst_{version}_SHA256SUMS'
        sums.write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n' for p in files))

    def assert_rejected(self, message, expected_version=None):
        with mock.patch.object(release.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, message):
                release.verify(self.root, expected_version)
            run.assert_not_called()

    def test_mixed_versions(self):
        self.fixture(mixed=True)
        self.assert_rejected('mixed release versions')

    def test_wrong_requested_version(self):
        self.fixture()
        self.assert_rejected('requested version', '2.0.0')

    def test_corrupt_artifact(self):
        self.fixture()
        next(self.root.glob('*.zip')).write_bytes(b'changed')
        self.assert_rejected('Checksum mismatch')

    def test_unsupported_manifest(self):
        self.fixture(manifest_version=2)
        self.assert_rejected('protocol 6.0 manifest')

    def test_uppercase_prerelease(self):
        self.fixture(version='1.0.0-ALPHA')
        self.assert_rejected('lowercase prerelease')

    def test_unsafe_checksum_path(self):
        self.fixture()
        next(self.root.glob('*_SHA256SUMS')).write_text('0' * 64 + '  ../escape\n')
        self.assert_rejected('Invalid checksum entry')

    def test_installation_uses_only_mirror(self):
        self.fixture()
        def run(args, **kwargs):
            root = kwargs['cwd']
            config = (root / 'terraform.rc').read_text()
            self.assertIn('filesystem_mirror', config)
            self.assertNotIn('dev_overrides', config)
            self.assertNotIn('direct', config)
            self.assertIn('version = "1.0.0"', (root / 'main.tf').read_text())
            self.assertNotIn('TF_CLI_ARGS', kwargs['env'])
            schemas = {'provider_schemas': {'registry.terraform.io/edson-edwin/catalyst': {
                'resource_schemas': {'catalyst_webhook_cron': {}, 'catalyst_filestore_file': {}, 'catalyst_function': {}},
                'data_source_schemas': {name: {} for name in ['catalyst_project', 'catalyst_projects', 'catalyst_job_pools', 'catalyst_datastore_tables', 'catalyst_filestore_folders', 'catalyst_cache_segments', 'catalyst_stratus_buckets', 'catalyst_functions']}}}}
            return subprocess.CompletedProcess(args, 0, stdout=json.dumps(schemas), stderr='')
        with mock.patch.object(release.platform, 'system', return_value='Linux'), \
             mock.patch.object(release.platform, 'machine', return_value='x86_64'), \
             mock.patch.dict(release.os.environ, {'TF_CLI_ARGS': '-invalid'}), \
             mock.patch.object(release.subprocess, 'run', side_effect=run) as invoke:
            release.verify(self.root, '1.0.0')
            self.assertEqual(invoke.call_count, 2)
            self.assertEqual(invoke.call_args_list[0].args[0][1], 'init')


class SignatureChecks(unittest.TestCase):
    def test_mismatched_signature_stops_before_install(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            sums = root / 'fixture_SHA256SUMS'
            sums.write_text('fixture')
            Path(str(sums) + '.sig').write_text('fixture')
            result = subprocess.CompletedProcess([], 0, stdout='[GNUPG:] VALIDSIG ' + 'B' * 40 + ' 2026-10-23 0 0 4 0 1 8 00 ' + 'B' * 40, stderr='')
            with mock.patch.object(release.subprocess, 'run', return_value=result) as run:
                with self.assertRaisesRegex(ValueError, 'different signing key'):
                    release.verify(root, signer_fingerprint='A' * 40)
                self.assertEqual(run.call_count, 1)
                self.assertEqual(run.call_args.args[0][0], 'gpg')

    def test_primary_key_accepts_signing_subkey(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            sums = root / 'fixture_SHA256SUMS'
            sums.write_text('fixture')
            Path(str(sums) + '.sig').write_text('fixture')
            status = '[GNUPG:] VALIDSIG ' + 'B' * 40 + ' 2026-10-23 0 0 4 0 1 8 00 ' + 'A' * 40
            for returncode in [0, 1]:
                result = subprocess.CompletedProcess([], returncode, stdout=status, stderr='')
                with mock.patch.object(release.subprocess, 'run', return_value=result):
                    if returncode:
                        with self.assertRaises(ValueError):
                            release.verify_signature(root, 'A' * 40)
                    else:
                        release.verify_signature(root, 'A' * 40)


if __name__ == '__main__':
    unittest.main()
