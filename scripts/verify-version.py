#!/usr/bin/env python3
"""Check actual public runtime versions, package metadata and built-file identity."""
import hashlib
import argparse
import json
import pathlib
import re
import subprocess
import tarfile
from install import verify_binaries
from version import selected_version, validate_version

root = pathlib.Path(__file__).resolve().parents[1]
_COMPONENTS = ('wallet-core', 'wallet-signer', 'wallet-launcher')
_GO_INVENTORIES = ('go-sbom.cdx.json', 'go-dependency-licenses.json', 'go-source-manifest.json',
                   'go-THIRD-PARTY-NOTICES.txt', 'go-THIRD-PARTY-NOTICES.json')


def _sha256(filename):
    with pathlib.Path(filename).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def _electron_version(base, appdir):
    script = "import('./desktop/node_modules/@electron/asar/lib/asar.js').then(a=>process.stdout.write(JSON.parse(a.extractFile(process.argv[1],'package.json').toString()).version));"
    return subprocess.check_output(['node', '-e', script, str(appdir / 'resources/app.asar')], cwd=base, text=True, timeout=20)


def verify_go_versions(base, version, packaged=True):
    validate_version(version)
    base = pathlib.Path(base)
    appdir = base / 'dist/CryptoWallet-linux-x64'
    binaries = [base / 'bin' / name for name in _COMPONENTS]
    if packaged:
        binaries += [appdir / 'resources/bin' / name for name in _COMPONENTS]
        binaries.append(appdir / 'cryptowallet')
    for binary in binaries:
        actual = subprocess.check_output([str(binary), '--version'], text=True, timeout=10)
        if actual != version + '\n':
            raise RuntimeError('Go runtime version differs: ' + str(binary.relative_to(base)))


def _decode_json(content, expected_type=dict):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise RuntimeError('Duplicate manifest member')
            value[key] = item
        return value
    value = json.loads(content, object_pairs_hook=unique)
    if not isinstance(value, expected_type):
        raise RuntimeError('Manifest must be an object' if expected_type is dict else 'Invalid Go dependency license inventory')
    return value


def _read_json(filename):
    filename = pathlib.Path(filename)
    if filename.is_symlink() or not filename.is_file() or filename.stat().st_size > 1048576:
        raise RuntimeError('Unsafe manifest file')
    return _decode_json(filename.read_text())


def verify_go_only(base=root):
    base = pathlib.Path(base)
    version = selected_version(base)
    source = base / 'bin'
    manifest = _read_json(source / 'go-build-manifest.json')
    if type(manifest.get('schemaVersion')) is not int or manifest['schemaVersion'] != 1 or manifest.get('kind') != 'go-components' or manifest.get('platform') != 'linux-amd64':
        raise RuntimeError('Unsupported Go component manifest')
    if manifest.get('version') != version or manifest.get('status') != 'DEVELOPMENT_UNSIGNED':
        raise RuntimeError('Go component build version/status differs')
    binaries = _read_json(source / 'manifest.json')
    if not isinstance(binaries, dict) or set(binaries) != set(_COMPONENTS) or manifest.get('binaryManifest') != binaries:
        raise RuntimeError('Go component binary manifest differs')
    for name in _COMPONENTS:
        path = source / name
        if path.is_symlink() or not path.is_file() or not path.stat().st_mode & 0o111 or _sha256(path) != binaries[name]:
            raise RuntimeError('Go component binary hash differs: ' + name)
    verify_go_versions(base, version, packaged=False)
    if 'archive' in manifest:
        archive = manifest['archive']
        expected = f'cryptowallet-go-{version}-linux-amd64.tar.gz'
        if not isinstance(archive, dict) or set(archive) != {'file', 'sha256'} or archive['file'] != expected or _sha256(base / 'dist' / expected) != archive['sha256']:
            raise RuntimeError('Go archive identity differs')
        if _read_json(base / 'dist/go-build-manifest.json') != manifest or _read_json(base / 'dist/manifest.json') != binaries:
            raise RuntimeError('Distributed Go manifest differs')
        for name in _COMPONENTS:
            path = base / 'dist' / name
            if path.is_symlink() or not path.is_file() or not path.stat().st_mode & 0o111 or _sha256(path) != binaries[name]:
                raise RuntimeError('Distributed Go binary hash differs: ' + name)
        verify_go_archive(base / 'dist' / expected, source, manifest)
    print('PASS version ' + version + ' matches Go component runtime versions, manifests and any packaged archive')


def _verify_go_inventories(dist, manifest):
    hashes = manifest.get('inventorySHA256')
    if not isinstance(hashes, dict) or set(hashes) != set(_GO_INVENTORIES):
        raise RuntimeError('Incomplete Go release inventory manifest')
    for name, expected in hashes.items():
        path = dist / name
        if not isinstance(expected, str) or not re.fullmatch('[0-9a-f]{64}', expected) or path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1 or not 0 < path.stat().st_size <= 64 << 20 or _sha256(path) != expected:
            raise RuntimeError('Distributed Go inventory hash differs: ' + name)
        if name.endswith('.json'):
            # Dependency license inventories are lists, while the remaining
            # machine manifests are objects. Reject duplicate keys in both.
            _decode_json(path.read_bytes(), list if name == 'go-dependency-licenses.json' else dict)
    notices = _decode_json((dist / 'go-THIRD-PARTY-NOTICES.json').read_bytes())
    if notices.get('noticeSHA256') != hashes['go-THIRD-PARTY-NOTICES.txt']:
        raise RuntimeError('Go redistribution notice pair differs')


def verify_go_archive(filename, source, manifest):
    source = pathlib.Path(source)
    dist = source.parent / 'dist'
    _verify_go_inventories(dist, manifest)
    prefix = f"cryptowallet-go-{manifest['version']}-linux-amd64"
    required = set(_COMPONENTS) | set(_GO_INVENTORIES) | {'manifest.json', 'go-build-manifest.json', 'README.md'}
    seen = set()
    with tarfile.open(filename, 'r:gz') as archive:
        for member in archive:
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not path.parts or path.parts[0] != prefix:
                raise RuntimeError('Unsafe Go archive path')
            if len(path.parts) == 1 and member.isdir():
                continue
            relative = pathlib.PurePosixPath(*path.parts[1:]).as_posix()
            if relative not in required or relative in seen or not member.isfile():
                raise RuntimeError('Unexpected or ambiguous Go archive entry')
            seen.add(relative)
            if relative in _COMPONENTS and not member.mode & 0o111:
                raise RuntimeError('Go archive executable permission differs')
            comparison = (dist if relative in _GO_INVENTORIES else source) / relative
            if relative in ('README.md', 'go-build-manifest.json'):
                if member.size > 1048576:
                    raise RuntimeError('Oversized Go archive metadata')
            elif member.size != comparison.stat().st_size:
                raise RuntimeError('Archived Go payload size differs: ' + relative)
            with archive.extractfile(member) as stream:
                if relative == 'go-build-manifest.json':
                    archived = _decode_json(stream.read())
                    expected = {key: value for key, value in manifest.items() if key != 'archive'}
                    if archived != expected:
                        raise RuntimeError('Archived Go build identity differs')
                elif relative != 'README.md':
                    if hashlib.file_digest(stream, 'sha256').hexdigest() != _sha256(comparison):
                        raise RuntimeError('Archived Go payload differs: ' + relative)
    if seen != required:
        raise RuntimeError('Incomplete Go component archive')



def verify_portable_payload(archive_path, appdir):
    required = {'cryptowallet', 'cryptowallet-ui', 'resources/app.asar', 'resources/bin/manifest.json'}
    required.update('resources/bin/' + name for name in _COMPONENTS)
    seen = set()
    with tarfile.open(archive_path, 'r:gz') as archive:
        for member in archive:
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not path.parts or path.parts[0] != appdir.name:
                raise RuntimeError('Unsafe portable archive path')
            relative = pathlib.PurePosixPath(*path.parts[1:]).as_posix()
            if relative not in required:
                continue
            if relative in seen or not member.isfile():
                raise RuntimeError('Ambiguous portable runtime entry')
            seen.add(relative)
            if member.size != (appdir / relative).stat().st_size:
                raise RuntimeError('Portable runtime payload differs: ' + relative)
            with archive.extractfile(member) as stream:
                actual = hashlib.file_digest(stream, 'sha256').hexdigest()
            if actual != _sha256(appdir / relative):
                raise RuntimeError('Portable runtime payload differs: ' + relative)
    if seen != required:
        raise RuntimeError('Incomplete portable runtime archive')


def verify_version(base=root):
    base = pathlib.Path(base)
    version = selected_version(base)
    dist = base / 'dist'
    manifest = json.loads((dist / 'build-manifest.json').read_text())
    if manifest['version'] != version:
        raise RuntimeError('Build manifest version differs')
    appdir = dist / 'CryptoWallet-linux-x64'
    actual = _electron_version(base, appdir)
    if actual != version:
        raise RuntimeError('Electron app.getVersion() source differs')
    binary_manifest = verify_binaries(appdir)
    if manifest['binaryManifest'] != binary_manifest:
        raise RuntimeError('Packaged binary manifest differs')
    source_manifest = json.loads((base / 'bin/manifest.json').read_text())
    if source_manifest != binary_manifest:
        raise RuntimeError('Source/packaged binary manifest differs')
    for name in _COMPONENTS:
        if _sha256(base / 'bin' / name) != binary_manifest[name]:
            raise RuntimeError('Source binary hash differs: ' + name)
    portable = manifest['portable']
    expected_name = f'cryptowallet-{version}-linux-amd64.tar.gz'
    if portable['file'] != expected_name or _sha256(dist / expected_name) != portable['sha256']:
        raise RuntimeError('Portable artifact identity differs')
    verify_portable_payload(dist / expected_name, appdir)
    verify_go_versions(base, version)
    print('PASS version ' + version + ' matches portable archive, Electron app.getVersion(), all source/packaged Go runtime versions and manifests')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go-only', action='store_true', help='verify Go components without Node or desktop artifacts')
    args = parser.parse_args()
    try:
        verify_go_only() if args.go_only else verify_version()
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, tarfile.TarError, subprocess.SubprocessError) as error:
        raise SystemExit(str(error))
