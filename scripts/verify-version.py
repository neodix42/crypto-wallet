#!/usr/bin/env python3
"""Check actual public runtime versions, package metadata and built-file identity."""
import hashlib
import json
import pathlib
import subprocess
from install import verify_binaries
from version import selected_version, validate_version

root = pathlib.Path(__file__).resolve().parents[1]
_COMPONENTS = ('wallet-core', 'wallet-signer', 'wallet-launcher')


def _sha256(filename):
    with pathlib.Path(filename).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def _electron_version(base, appdir):
    script = "import('./desktop/node_modules/@electron/asar/lib/asar.js').then(a=>process.stdout.write(JSON.parse(a.extractFile(process.argv[1],'package.json').toString()).version));"
    return subprocess.check_output(['node', '-e', script, str(appdir / 'resources/app.asar')], cwd=base, text=True, timeout=20)


def verify_go_versions(base, version):
    validate_version(version)
    base = pathlib.Path(base)
    appdir = base / 'dist/LocalWallet-linux-x64'
    binaries = [base / 'bin' / name for name in _COMPONENTS]
    binaries += [appdir / 'resources/bin' / name for name in _COMPONENTS]
    binaries.append(appdir / 'wallet-launcher')
    for binary in binaries:
        actual = subprocess.check_output([str(binary), '--version'], text=True, timeout=10)
        if actual != version + '\n':
            raise RuntimeError('Go runtime version differs: ' + str(binary.relative_to(base)))


def verify_version(base=root):
    base = pathlib.Path(base)
    version = selected_version(base)
    dist = base / 'dist'
    manifest = json.loads((dist / 'build-manifest.json').read_text())
    if manifest['version'] != version:
        raise RuntimeError('Build manifest version differs')
    package = f'localwallet-development_{version}_amd64.deb'
    if manifest['package'] != package:
        raise RuntimeError('Debian package name differs')
    deb = dist / package
    if _sha256(deb) != manifest['sha256']:
        raise RuntimeError('Debian artifact hash differs')
    actual = subprocess.check_output(['dpkg-deb', '-f', str(deb), 'Version'], text=True, timeout=20).strip()
    if actual != version:
        raise RuntimeError('Debian version differs')
    appdir = dist / 'LocalWallet-linux-x64'
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
    if 'portable' in manifest:
        portable = manifest['portable']
        expected_name = f'localwallet-{version}-linux-amd64.tar.gz'
        if portable['file'] != expected_name or _sha256(dist / expected_name) != portable['sha256']:
            raise RuntimeError('Portable artifact identity differs')
    verify_go_versions(base, version)
    print('PASS version ' + version + ' matches Debian, Electron app.getVersion(), all source/packaged Go runtime versions and manifests')


if __name__ == '__main__':
    try:
        verify_version()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        raise SystemExit(str(error))
