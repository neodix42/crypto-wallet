#!/usr/bin/env python3
"""Verify one Wails Go executable plus its optional desktop installer and icons."""
import argparse
import gzip
import hashlib
import json
import pathlib
import re
import stat
import subprocess
import tarfile
import tempfile

from version import selected_version

ROOT = pathlib.Path(__file__).resolve().parents[1]
RESOURCES = {'install-user.sh': ('scripts/install-user.sh', 0o755),
             'cryptowallet.png': ('internal/branding/cryptowallet.png', 0o644),
             'cryptowallet.svg': ('internal/branding/cryptowallet.svg', 0o644)}


def sha256(path):
    with pathlib.Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def verify_binary(binary, version, manifest=None):
    binary = pathlib.Path(binary)
    info = binary.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_mode & 0o111 and
            not info.st_mode & 0o6000, 'Portable executable must be a regular executable without setuid/setgid')
    with binary.open('rb') as stream:
        header = stream.read(64)
    require(len(header) == 64 and header[:7] == b'\x7fELF\x02\x01\x01' and
            header[18:20] == b'\x3e\x00', 'Portable executable is not Linux amd64 ELF64')
    if manifest is not None:
        require(manifest.get('schemaVersion') == 1 and manifest.get('kind') == 'portable-go-wallet' and
                manifest.get('version') == version and manifest.get('platform') == 'linux-amd64' and
                manifest.get('file') == 'cryptowallet' and manifest.get('sandbox') == 'none' and
                manifest.get('sha256') == sha256(binary), 'Portable build identity/hash differs')
    # This is a locally compiled candidate, never an untrusted downloaded asset.
    require(subprocess.check_output([str(binary.resolve()), '--version'], text=True, timeout=15) == version + '\n',
            'Portable executable version differs')
    notices = subprocess.check_output([str(binary.resolve()), '--licenses'], timeout=15)
    require(b'CryptoWallet third-party redistribution texts' in notices and
            b'go-toolchain:' in notices and b'go:' in notices and
            b'not been generated' not in notices and len(notices) > 4096,
            'Required upstream notices are not embedded')
    require(subprocess.check_output([str(binary.resolve()), '--runtime-info'], text=True, timeout=15).strip() ==
            'wails-webkitgtk4.1; portable-go; sandbox=none', 'Portable runtime declaration differs')
    # The native executable may dynamically link system GTK/glib/libc, but must
    # not depend on a bundled Chromium/Electron or libseccomp runtime.
    result = subprocess.run(['readelf', '-d', str(binary)], check=True, capture_output=True, text=True, timeout=15)
    needed = re.findall(r'\(NEEDED\).*?\[([^\]]+)\]', result.stdout)
    require(needed and any('libgtk-3.so' in item for item in needed), 'Native GTK3 dependency is missing')
    require(any('libwebkit2gtk-4.1.so' in item for item in needed), 'System WebKitGTK 4.1 dependency is missing')
    require(not any(any(word in item.lower() for word in ('electron', 'chrom', 'seccomp')) for item in needed),
            'Forbidden Chromium/Electron/sandbox runtime dependency')
    return dict(file='cryptowallet', sha256=sha256(binary), version=version,
                dynamicLibraries=needed, licenses='embedded', sandbox='none')


def payloads(root, binary):
    entries = {'cryptowallet': (pathlib.Path(binary), 0o755)}
    for name, (relative, mode) in RESOURCES.items():
        path = pathlib.Path(root) / relative
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and not info.st_mode & 0o6000,
                'Unsafe portable resource: ' + name)
        require(0 < info.st_size < 1024 * 1024, 'Missing/oversized portable resource: ' + name)
        entries[name] = (path, mode)
    require(entries['install-user.sh'][0].read_bytes().startswith(b'#!/usr/bin/env bash\n'),
            'Desktop installer is not the expected Bash entrypoint')
    require(entries['cryptowallet.png'][0].read_bytes().startswith(b'\x89PNG\r\n\x1a\n'), 'Invalid PNG icon')
    require(b'<svg' in entries['cryptowallet.svg'][0].read_bytes(), 'Invalid SVG icon')
    return entries


def verify_archive(archive, binary, version, root=ROOT):
    require(not archive.is_symlink() and archive.is_file(), 'Missing portable archive')
    expected = payloads(root, binary)
    with tarfile.open(archive, 'r:gz') as package:
        members = package.getmembers()
        require([member.name for member in members] == list(expected),
                'Portable archive must contain only the exact executable, installer and icon inventory')
        for info in members:
            path, mode = expected[info.name]
            require(info.isfile() and info.mode == mode and info.uid == info.gid == info.mtime == 0 and
                    not info.uname and not info.gname and not info.linkname and info.size == path.stat().st_size,
                    'Portable archive member metadata differs: ' + info.name)
            member = package.extractfile(info)
            require(member is not None and hashlib.file_digest(member, 'sha256').hexdigest() == sha256(path),
                    'Portable archive bytes differ from the source payload: ' + info.name)
    return archive


def package(root=ROOT, verify_only=False):
    root = pathlib.Path(root)
    version = selected_version(root)
    binary = root / 'bin/cryptowallet'
    manifest = json.loads((root / 'bin/portable-build-manifest.json').read_text())
    checked = verify_binary(binary, version, manifest)
    entries = payloads(root, binary)
    dist = root / 'dist'
    require(not dist.is_symlink(), 'Portable output directory must not be a symlink')
    dist.mkdir(exist_ok=True)
    require(dist.is_dir(), 'Portable output must be a real directory')
    archive = dist / f'cryptowallet-{version}-linux-amd64.tar.gz'
    if not verify_only:
        # Fixed explicit inventory/metadata: no path/build-user leakage, browser
        # runtime or incidental build reports. The owner requested these extras.
        with tempfile.NamedTemporaryFile(prefix='.cryptowallet-', dir=dist, delete=False) as temporary:
            temporary_path = pathlib.Path(temporary.name)
            try:
                with gzip.GzipFile(filename='', fileobj=temporary, mode='wb', mtime=0) as zipped:
                    with tarfile.open(fileobj=zipped, mode='w', format=tarfile.USTAR_FORMAT) as package:
                        for name, (path, mode) in entries.items():
                            info = tarfile.TarInfo(name)
                            info.mode, info.size, info.mtime = mode, path.stat().st_size, 0
                            with path.open('rb') as payload:
                                package.addfile(info, payload)
                temporary.flush()
                temporary_path.replace(archive)
            finally:
                temporary_path.unlink(missing_ok=True)
    verify_archive(archive, binary, version, root)
    report = dict(schemaVersion=1, status='PASS', scope='Local Wails Go ELF/version, embedded frontend/notices, system WebKitGTK dependencies and exact executable/installer/icon archive',
                  checks=checked, archive=dict(file=archive.name, sha256=sha256(archive)),
                  payloads={name: {'mode': oct(mode), 'sha256': sha256(path)} for name, (path, mode) in entries.items()},
                  notRun=['clean Ubuntu X11/Wayland OS matrix', 'live funded testnets', 'independent security and redistribution review'])
    (dist / 'portable-build-verification.json').write_text(json.dumps(report, indent=2) + '\n')
    print('PASS portable Wails Go executable with optional desktop installer/icons: ' + str(archive))
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verify-only', action='store_true')
    args = parser.parse_args()
    try:
        package(verify_only=args.verify_only)
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        raise SystemExit('Portable packaging failed: ' + str(error))
