#!/usr/bin/env python3
"""Check release checksums, platform coverage and the native Terraform handshake.

Uses only Python's standard library and a local Terraform binary; no cloud access.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tempfile
import zipfile


def prepare_snapshot(directory):
    # GoReleaser renames extra files on upload; snapshots skip that upload step.
    metadata = json.loads((directory / 'metadata.json').read_text())
    version = metadata['version']
    if metadata['project_name'] != 'terraform-provider-catalyst' or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+-dev\.[0-9a-f]+', version):
        raise ValueError('Manifest staging is allowed only for local snapshot artifacts')
    source = Path(__file__).resolve().parent.parent / 'terraform-registry-manifest.json'
    (directory / f'terraform-provider-catalyst_{version}_manifest.json').write_bytes(source.read_bytes())


def verify_signature(directory, fingerprint):
    """Require a valid signature from the independently supplied signing identity."""
    fingerprint = fingerprint.upper()
    if not re.fullmatch(r'(?:[0-9A-F]{40}|[0-9A-F]{64})', fingerprint):
        raise ValueError('Provide the full signing-key fingerprint')
    sums = list(directory.glob('*_SHA256SUMS'))
    if len(sums) != 1 or not Path(str(sums[0]) + '.sig').is_file():
        raise ValueError('Expected checksums and their detached signature')
    # --no-auto-key-retrieve prevents an unexpected network key lookup. Import
    # the trusted public key separately; never trust a key shipped with a release.
    result = subprocess.run(['gpg', '--batch', '--no-auto-key-retrieve', '--status-fd=1',
                             '--verify', str(sums[0]) + '.sig', str(sums[0])],
                            capture_output=True, text=True, timeout=30)
    signatures = [line.split()[2:] for line in result.stdout.splitlines()
                  if line.startswith('[GNUPG:] VALIDSIG ')]
    if result.returncode != 0 or not any(fields and fingerprint in (fields[0], fields[-1]) for fields in signatures):
        raise ValueError('Checksum signature is invalid or belongs to a different signing key')


def verify(directory, expected_version=None, signer_fingerprint=None):
    if signer_fingerprint is not None:
        verify_signature(directory, signer_fingerprint)
    sums = list(directory.glob('*_SHA256SUMS'))
    if len(sums) != 1:
        raise ValueError('Expected one checksum file')
    entries = {}
    for line in sums[0].read_text().splitlines():
        digest, name = line.split()
        if not re.fullmatch(r'[0-9a-f]{64}', digest) or Path(name).name != name:
            raise ValueError('Invalid checksum entry')
        if name in entries:
            raise ValueError('Duplicate checksum entry')
        artifact = directory / name
        if hashlib.sha256(artifact.read_bytes()).hexdigest() != digest:
            raise ValueError(f'Checksum mismatch: {name}')
        entries[name] = artifact
    manifests = [p for name, p in entries.items() if name.endswith('_manifest.json')]
    if len(manifests) != 1:
        raise ValueError('Expected one checksummed Registry manifest')
    manifest = json.loads(manifests[0].read_text())
    if manifest.get('version') != 1 or manifest.get('metadata', {}).get('protocol_versions') != ['6.0']:
        raise ValueError('Expected Terraform protocol 6.0 manifest')
    expected = {
        'linux_amd64', 'linux_arm64', 'linux_386', 'linux_arm', 'linux_s390x',
        'darwin_amd64', 'darwin_arm64',
        'windows_amd64', 'windows_arm64', 'windows_386',
        'freebsd_amd64', 'freebsd_arm64', 'freebsd_386', 'freebsd_arm',
    }
    host_os = {'Linux': 'linux', 'Darwin': 'darwin', 'Windows': 'windows', 'FreeBSD': 'freebsd'}.get(platform.system())
    host_arch = {'x86_64': 'amd64', 'AMD64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64', 'i386': '386', 'i686': '386'}.get(platform.machine())
    native = f'{host_os}_{host_arch}'
    found = set()
    native_archive = None
    versions = set()
    for name, archive in entries.items():
        if not name.endswith('.zip'):
            continue
        match = re.fullmatch(r'terraform-provider-catalyst_(.+)_(linux|darwin|windows|freebsd)_(amd64|arm64|386|arm|s390x)\.zip', name)
        if not match:
            raise ValueError(f'Unexpected archive name: {name}')
        version, target_os, arch = match.groups()
        if version != version.lower():
            raise ValueError("Use lowercase prerelease versions for Terraform mirror compatibility")
        versions.add(version)
        target = f'{target_os}_{arch}'
        if target in found:
            raise ValueError(f'Duplicate platform: {target}')
        found.add(target)
        binary = f'terraform-provider-catalyst_v{version}' + ('.exe' if target_os == 'windows' else '')
        with zipfile.ZipFile(archive) as package:
            if package.testzip() is not None:
                raise ValueError(f'Corrupt archive: {name}')
            if package.namelist().count(binary) != 1:
                raise ValueError(f'Expected one provider executable in {name}')
            if package.getinfo(binary).file_size == 0:
                raise ValueError(f'Empty executable in {name}')
            if target == native:
                native_archive = archive
    if found != expected:
        raise ValueError(f'Platform mismatch: missing={expected-found}, unexpected={found-expected}')
    if {p.name for p in directory.glob('*.zip')} != {n for n in entries if n.endswith('.zip')}:
        raise ValueError('An archive is missing from the checksum file')
    if len(versions) != 1:
        raise ValueError('Archives contain mixed release versions')
    version = versions.pop()
    if expected_version is not None and version != expected_version:
        raise ValueError('Release version does not match the requested version')
    prefix = f'terraform-provider-catalyst_{version}'
    if sums[0].name != prefix + '_SHA256SUMS' or manifests[0].name != prefix + '_manifest.json':
        raise ValueError('Manifest/checksum filenames do not match the archive version')
    if native_archive is None:
        raise ValueError(f'No native archive for {native}')
    with tempfile.TemporaryDirectory(prefix='catalyst-release-check-') as tmp:
        root = Path(tmp)
        mirror = root / 'mirror'
        provider_dir = mirror / 'registry.terraform.io' / 'edson-edwin' / 'catalyst'
        provider_dir.mkdir(parents=True)
        shutil.copy2(native_archive, provider_dir / native_archive.name)
        config = root / 'terraform.rc'
        config.write_text('provider_installation {\n filesystem_mirror {\n path = ' + json.dumps(str(mirror)) + '\n include = ["registry.terraform.io/edson-edwin/catalyst"]\n }\n}\n')
        (root / 'main.tf').write_text('terraform {\n required_providers {\n catalyst = { source = "Edson-Edwin/catalyst", version = ' + json.dumps(version) + ' }\n }\n}\n')
        # Prevent inherited CLI arguments or a shared data directory from changing the test.
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(('TF_CLI_ARGS', 'TF_REATTACH_PROVIDERS'))
               and key not in ('TF_PLUGIN_CACHE_DIR', 'TF_DATA_DIR')}
        env.update(TF_CLI_CONFIG_FILE=str(config), CHECKPOINT_DISABLE='1', TF_IN_AUTOMATION='1')
        subprocess.run(['terraform', 'init', '-backend=false', '-input=false', '-no-color'], cwd=root, env=env, capture_output=True, text=True, check=True, timeout=60)
        result = subprocess.run(['terraform', 'providers', 'schema', '-json'], cwd=root, env=env, capture_output=True, text=True, check=True, timeout=60)
        schemas = json.loads(result.stdout)['provider_schemas']['registry.terraform.io/edson-edwin/catalyst']
        if set(schemas['resource_schemas']) != {'catalyst_webhook_cron', 'catalyst_filestore_file', 'catalyst_function', 'catalyst_appsail'}:
            raise ValueError('Packaged provider has unexpected resources')
        if set(schemas['data_source_schemas']) != {'catalyst_project', 'catalyst_projects', 'catalyst_job_pools', 'catalyst_datastore_tables', 'catalyst_filestore_folders', 'catalyst_cache_segments', 'catalyst_stratus_buckets', 'catalyst_functions'}:
            raise ValueError('Packaged provider has unexpected data sources')
    print(f'Verified checksums, manifest, {len(found)} platform archives, local-mirror installation and native Terraform protocol handshake.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--snapshot', action='store_true', help='Stage the manifest that GoReleaser normally renames on upload')
    parser.add_argument('--version', help='Require this exact release version (without v)')
    parser.add_argument('--signer-fingerprint', help='Require the checksum signature to match this full trusted GPG fingerprint before installing')
    args = parser.parse_args()
    if args.snapshot:
        prepare_snapshot(args.directory)
    verify(args.directory, args.version, args.signer_fingerprint)
