#!/usr/bin/env python3
"""Package and verify the one-file native Go desktop. No Electron or sandbox."""
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
            'native-gtk3; portable-go; sandbox=none', 'Portable runtime declaration differs')
    # The native executable may dynamically link system GTK/glib/libc, but must
    # not depend on a bundled Chromium/Electron or libseccomp runtime.
    result = subprocess.run(['readelf', '-d', str(binary)], check=True, capture_output=True, text=True, timeout=15)
    needed = re.findall(r'\(NEEDED\).*?\[([^\]]+)\]', result.stdout)
    require(needed and any('libgtk-3.so' in item for item in needed), 'Native GTK3 dependency is missing')
    require(not any(any(word in item.lower() for word in ('electron', 'chrom', 'seccomp')) for item in needed),
            'Forbidden Chromium/Electron/sandbox runtime dependency')
    return dict(file='cryptowallet', sha256=sha256(binary), version=version,
                dynamicLibraries=needed, licenses='embedded', sandbox='none')


def verify_archive(archive, binary, version):
    require(not archive.is_symlink() and archive.is_file(), 'Missing portable archive')
    with tarfile.open(archive, 'r:gz') as package:
        members = package.getmembers()
        require(len(members) == 1 and members[0].name == 'cryptowallet' and
                members[0].isfile() and members[0].mode == 0o755,
                'Portable archive must contain only the regular cryptowallet executable')
        member = package.extractfile(members[0])
        require(member is not None and hashlib.file_digest(member, 'sha256').hexdigest() == sha256(binary),
                'Portable archive bytes differ from the built executable')
    return archive


def package(root=ROOT, verify_only=False):
    root = pathlib.Path(root)
    version = selected_version(root)
    binary = root / 'bin/cryptowallet'
    manifest = json.loads((root / 'bin/portable-build-manifest.json').read_text())
    checked = verify_binary(binary, version, manifest)
    dist = root / 'dist'
    require(not dist.is_symlink(), 'Portable output directory must not be a symlink')
    dist.mkdir(exist_ok=True)
    require(dist.is_dir(), 'Portable output must be a real directory')
    archive = dist / f'cryptowallet-{version}-linux-amd64.tar.gz'
    if not verify_only:
        # Fixed archive metadata: no path/build-user leakage or helper files.
        with tempfile.NamedTemporaryFile(prefix='.cryptowallet-', dir=dist, delete=False) as temporary:
            temporary_path = pathlib.Path(temporary.name)
            try:
                with gzip.GzipFile(filename='', fileobj=temporary, mode='wb', mtime=0) as zipped:
                    with tarfile.open(fileobj=zipped, mode='w', format=tarfile.USTAR_FORMAT) as package:
                        info = tarfile.TarInfo('cryptowallet')
                        info.mode, info.size, info.mtime = 0o755, binary.stat().st_size, 0
                        with binary.open('rb') as payload:
                            package.addfile(info, payload)
                temporary.flush()
                temporary_path.replace(archive)
            finally:
                temporary_path.unlink(missing_ok=True)
    verify_archive(archive, binary, version)
    report = dict(schemaVersion=1, status='PASS', scope='Local native Go ELF/version, embedded notices, dynamic dependencies and exact single-file archive',
                  checks=checked, archive=dict(file=archive.name, sha256=sha256(archive)),
                  notRun=['clean Ubuntu X11/Wayland OS matrix', 'live funded testnets', 'independent security and redistribution review'])
    (dist / 'portable-build-verification.json').write_text(json.dumps(report, indent=2) + '\n')
    print('PASS one-file native Go portable: ' + str(archive))
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verify-only', action='store_true')
    args = parser.parse_args()
    try:
        package(verify_only=args.verify_only)
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        raise SystemExit('Portable packaging failed: ' + str(error))
