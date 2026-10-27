#!/usr/bin/env python3
"""Install the portable Go executable; retain legacy desktop installer APIs."""
import argparse
import hashlib
import grp
import pwd
import importlib.util
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


_PORTABLE_MARKER = '.cryptowallet-install.json'


def _owned_regular(path, maximum=None):
    info = path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or
            info.st_nlink != 1 or info.st_mode & 0o022 or
            maximum is not None and info.st_size > maximum):
        raise RuntimeError('Unsafe portable installation file: ' + path.name)
    return info


def _file_digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def _read_portable_marker(marker):
    if not marker.exists() and not marker.is_symlink():
        return None
    _owned_regular(marker, 4096)
    data = json.loads(marker.read_text(), object_pairs_hook=_unique_members)
    if (not isinstance(data, dict) or set(data) != {'schemaVersion', 'kind', 'allowedSHA256'} or
            type(data['schemaVersion']) is not int or data['schemaVersion'] != 1 or
            data['kind'] != 'cryptowallet-portable-install' or
            not isinstance(data['allowedSHA256'], list) or not 1 <= len(data['allowedSHA256']) <= 2 or
            any(not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{64}', value)
                for value in data['allowedSHA256'])):
        raise RuntimeError('Invalid portable installation marker')
    return data


def _check_portable_target(target, runtime, marker):
    previous = _read_portable_marker(marker)
    if not target.exists() and not target.is_symlink():
        return None
    if target.is_symlink():
        if target.lstat().st_uid != os.getuid():
            raise RuntimeError('Existing launcher is not owned by the current user')
        _check_managed_runtime(runtime)
        _check_managed_launcher(target, runtime)
        return None
    _owned_regular(target)
    digest = _file_digest(target)
    if previous is None or digest not in previous['allowedSHA256']:
        raise RuntimeError('Refusing to replace an unrelated ./cryptowallet file; move it aside explicitly')
    return digest


def _safe_destination(info):
    if info.st_uid != os.getuid() or info.st_mode & 0o002:
        return False
    if not info.st_mode & 0o020:
        return True
    # Ubuntu commonly uses umask 0002 with a user-private primary group. Keep
    # these ordinary project directories usable without accepting shared-group
    # write access or changing any directory permission on the user's behalf.
    try:
        owner = pwd.getpwuid(os.getuid())
        group = grp.getgrgid(info.st_gid)
        return (info.st_gid == owner.pw_gid and group.gr_name == owner.pw_name and
                set(group.gr_mem) <= {owner.pw_name} and
                all(account.pw_uid == owner.pw_uid or account.pw_gid != info.st_gid
                    for account in pwd.getpwall()))
    except KeyError:
        return False


def install_portable(source, destination):
    source, destination = pathlib.Path(source).absolute(), pathlib.Path(destination).absolute()
    if destination.is_symlink() or not destination.is_dir():
        raise RuntimeError('Installation destination must be an existing real directory')
    info = destination.stat()
    if not _safe_destination(info):
        raise RuntimeError('Installation destination must be owned by this user and not writable by others')
    # Source is the locally built candidate. The sidecar is an internal build
    # record, never needed when users execute an extracted release directly.
    specification = importlib.util.spec_from_file_location('portable_package', root / 'scripts/package-portable.py')
    portable = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(portable)
    manifest = json.loads((source.parent / 'portable-build-manifest.json').read_text(), object_pairs_hook=_unique_members)
    portable.verify_binary(source, manifest.get('version'), manifest)
    target, runtime = destination / 'cryptowallet', destination / 'wallet-runtime'
    marker = destination / _PORTABLE_MARKER
    previous_digest = _check_portable_target(target, runtime, marker)
    # Stage and recheck before changing any public executable. Existing runtime
    # generations and all wallet/profile files are left in their original paths.
    with tempfile.NamedTemporaryFile(prefix='.cryptowallet-stage-', dir=destination, delete=False) as stream:
        staged = pathlib.Path(stream.name)
        try:
            with source.open('rb') as payload:
                shutil.copyfileobj(payload, stream)
            stream.flush()
            os.fchmod(stream.fileno(), 0o755)
            os.fsync(stream.fileno())
            if _file_digest(staged) != manifest['sha256']:
                raise RuntimeError('Staged portable executable hash differs')
            _check_portable_target(target, runtime, marker)
            hashes = [manifest['sha256']]
            if previous_digest is not None and previous_digest not in hashes:
                hashes.append(previous_digest)
            # Record both sides of the atomic executable switch. An interruption
            # after marker persistence can safely retry either recognized binary.
            marker_value = {'schemaVersion': 1, 'kind': 'cryptowallet-portable-install', 'allowedSHA256': hashes}
            with tempfile.NamedTemporaryFile(prefix='.cryptowallet-marker-', dir=destination, mode='w', delete=False) as output:
                staged_marker = pathlib.Path(output.name)
                try:
                    json.dump(marker_value, output, sort_keys=True)
                    output.write('\n')
                    output.flush()
                    os.fchmod(output.fileno(), 0o600)
                    os.fsync(output.fileno())
                    staged_marker.replace(marker)
                finally:
                    staged_marker.unlink(missing_ok=True)
            staged.replace(target)
            descriptor = os.open(destination, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
        finally:
            staged.unlink(missing_ok=True)
    print('Installed portable Go wallet at ' + str(target) + '; run ./cryptowallet as an ordinary user.')
    print('One executable; system GTK3 required. No Electron or sandbox runtime. Existing data is preserved.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--legacy-desktop', action='store_true', help='Explicitly install the retained Electron regression runtime')
    arguments = parser.parse_args()
    if arguments.legacy_desktop:
        install(root / 'dist/CryptoWallet-linux-x64', root)
    else:
        install_portable(root / 'bin/cryptowallet', root)
