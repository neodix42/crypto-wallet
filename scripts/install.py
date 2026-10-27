#!/usr/bin/env python3
"""Install a complete local runtime; atomically switch only an owned launcher."""
import hashlib
import json
import os
import pathlib
import re
import shutil
import stat
import tempfile
import uuid

root = pathlib.Path(__file__).resolve().parents[1]
_COMPONENTS = ('wallet-launcher', 'wallet-core', 'wallet-signer')


def _unique_members(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise RuntimeError('Duplicate binary manifest member')
        value[key] = item
    return value


def verify_binaries(source):
    source = pathlib.Path(source)
    for filename in ('cryptowallet-ui', 'resources/app.asar'):
        entry = source / filename
        if entry.is_symlink() or not entry.is_file() or (filename == 'cryptowallet-ui' and not entry.stat().st_mode & 0o111):
            raise RuntimeError('Incomplete packaged runtime: ' + filename)
    manifest = json.loads((source / 'resources/bin/manifest.json').read_text(), object_pairs_hook=_unique_members)
    if not isinstance(manifest, dict) or set(manifest) != set(_COMPONENTS):
        raise RuntimeError('Unexpected binary manifest fields')
    for name in _COMPONENTS:
        paths = [source / 'resources/bin' / name]
        if name == 'wallet-launcher':
            paths.append(source / 'cryptowallet')
        expected = manifest[name]
        if not isinstance(expected, str) or not re.fullmatch(r'[0-9a-f]{64}', expected):
            raise RuntimeError('Invalid built binary digest')
        for binary in paths:
            if binary.is_symlink() or not binary.is_file() or not binary.stat().st_mode & 0o111:
                raise RuntimeError('Missing or invalid executable: ' + name)
            with binary.open('rb') as stream:
                actual = hashlib.file_digest(stream, 'sha256').hexdigest()
            if actual != expected:
                raise RuntimeError('Built binary manifest mismatch: ' + name)
    return manifest


def _check_managed_runtime(runtime):
    if runtime.is_symlink():
        raise RuntimeError('Refusing to follow a wallet-runtime symlink')
    if not runtime.exists():
        return
    info = runtime.lstat()
    if info.st_uid != os.getuid() or info.st_mode & 0o022:
        raise RuntimeError('Unsafe local runtime directory ownership or permissions')
    if not runtime.is_dir():
        raise RuntimeError('Refusing to replace an unrelated wallet-runtime file')
    marker = runtime / '.localwallet-managed'
    try:
        info = marker.lstat()
    except FileNotFoundError:
        raise RuntimeError('Refusing to replace an unrelated wallet-runtime directory') from None
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_nlink != 1:
        raise RuntimeError('Invalid local runtime ownership marker')


def _check_managed_launcher(target, runtime):
    if not target.exists() and not target.is_symlink():
        return
    if not target.is_symlink():
        raise RuntimeError('Refusing to replace an unrelated ./cryptowallet file')
    link = os.readlink(target)
    if not re.fullmatch(r'wallet-runtime/app-[0-9a-f]{32}/cryptowallet', link):
        raise RuntimeError('Refusing to replace an unrelated ./cryptowallet symlink')
    if not runtime.exists():
        # A cleanup can remove the runtime while leaving our canonical link.
        # install() already rejected unsafe runtime paths. Keep this dangling
        # link until the verified new generation is atomically selected.
        return
    generation = runtime / pathlib.PurePosixPath(link).parts[1]
    if generation.is_symlink() or (generation.exists() and not generation.is_dir()):
        raise RuntimeError('Existing wallet generation is not an owned directory')


def install(source, destination):
    source, destination = pathlib.Path(source), pathlib.Path(destination)
    if not source.is_dir() or source.is_symlink():
        raise RuntimeError('The built application must be a directory')
    verify_binaries(source)
    if not destination.is_dir() or destination.is_symlink():
        raise RuntimeError('Installation destination must be an existing directory')
    target, runtime = destination / 'cryptowallet', destination / 'wallet-runtime'
    _check_managed_runtime(runtime)
    _check_managed_launcher(target, runtime)
    runtime.mkdir(mode=0o700, exist_ok=True)
    (runtime / '.localwallet-managed').touch(mode=0o600)
    with tempfile.TemporaryDirectory(prefix='.staging-', dir=runtime) as temporary:
        staged = pathlib.Path(temporary) / 'app'
        shutil.copytree(source, staged)
        # Reverify the completed copy before making it reachable through ./cryptowallet.
        verify_binaries(staged)
        generation = 'app-' + uuid.uuid4().hex
        staged.rename(runtime / generation)
    link = destination / ('.wallet-link-' + uuid.uuid4().hex)
    try:
        link.symlink_to(pathlib.Path('wallet-runtime') / generation / 'cryptowallet')
        link.replace(target)
    finally:
        if link.is_symlink():
            link.unlink()
    print('Installed complete wallet at ' + str(target) + '; run ./cryptowallet as an ordinary user.')
    print('Runtime and resources: ' + str(runtime / generation))


if __name__ == '__main__':
    install(root / 'dist/CryptoWallet-linux-x64', root)
