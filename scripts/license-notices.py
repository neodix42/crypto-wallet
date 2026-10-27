#!/usr/bin/env python3
"""Deterministic redistribution texts for actual Go commands and desktop runtime.

This collects upstream texts and verifies provenance; it is not a license
classifier, obligation determination, redistribution approval or legal review.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
COMMANDS = ('wallet-core', 'wallet-signer', 'wallet-launcher')
MAX_FILE = 8 << 20
MAX_TOTAL = 32 << 20
MAX_FILES = 1024
MAX_COMPONENTS = 256
REVIEW = 'NOT_RUN: independent license and redistribution obligation review'
NOTICE_NAME = re.compile(r'^(?:LICENSE|LICENCE|COPYING|NOTICE|COPYRIGHT|PATENTS)(?:[._-].*)?$', re.I)
SOURCE_EXTENSIONS = {'.go', '.c', '.h', '.rs', '.py', '.js', '.ts', '.json', '.cc', '.cpp'}


class NoticeError(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise NoticeError(reason)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def json_bytes(value):
    return (json.dumps(value, sort_keys=True, indent=2, ensure_ascii=False) + '\n').encode()


def relative_parts(relative):
    require(isinstance(relative, str), 'Unsafe notice-relative path')
    path = PurePosixPath(relative)
    require(relative and not path.is_absolute() and
            all(part not in ('', '.', '..') for part in relative.split('/')) and '\\' not in relative and
            not any(ord(character) < 32 or ord(character) == 127 for character in relative), 'Unsafe notice-relative path')
    return path.parts


def directory(path):
    """Open an absolute directory without following any symlink component."""
    path = Path(path)
    require(path.is_absolute(), 'Notice source directory must be absolute')
    descriptor = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for part in path.parts[1:]:
            require(part not in ('.', '..'), 'Unsafe notice directory component')
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        return descriptor
    except Exception:
        os.close(descriptor)
        raise NoticeError('Unsafe or unavailable notice source directory') from None


def read_file(root, relative, maximum=MAX_FILE, text=True):
    parts = relative_parts(relative)
    parent = directory(root)
    descriptor = None
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=parent)
            os.close(parent)
            parent = child
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK,
                             dir_fd=parent)
        before = os.fstat(descriptor)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and 0 < before.st_size <= maximum,
                'Missing, linked, empty or oversized redistribution text')
        chunks = []
        size = 0
        while True:
            chunk = os.read(descriptor, min(65536, maximum + 1 - size))
            if not chunk:
                break
            chunks.append(chunk)
            size += len(chunk)
            require(size <= maximum, 'Redistribution text exceeded size bound')
        after = os.fstat(descriptor)
        require((before.st_ino, before.st_size, before.st_mtime_ns) ==
                (after.st_ino, after.st_size, after.st_mtime_ns) and size == before.st_size,
                'Redistribution source changed during read')
        data = b''.join(chunks)
        if text:
            require(b'\0' not in data, 'Binary redistribution text')
            try:
                data.decode('utf-8')
            except UnicodeDecodeError:
                raise NoticeError('Redistribution text is not UTF-8') from None
        return data
    except NoticeError:
        raise
    except OSError:
        raise NoticeError('Missing or unsafe redistribution source file') from None
    finally:
        if descriptor is not None:
            os.close(descriptor)
        os.close(parent)


def notice_filename(name):
    return bool(NOTICE_NAME.fullmatch(name)) and Path(name).suffix.lower() not in SOURCE_EXTENSIONS


def collect_texts(root, prune_node_modules=False):
    root = Path(root)
    descriptor = directory(root)
    os.close(descriptor)
    paths = []
    for parent, directories, files in os.walk(root, followlinks=False):
        directories.sort()
        files.sort()
        for name in directories + files:
            candidate = Path(parent) / name
            require(not candidate.is_symlink(), 'Symlink in redistribution source tree')
        if prune_node_modules:
            directories[:] = [name for name in directories if name != 'node_modules']
        for name in files:
            if notice_filename(name):
                paths.append((Path(parent) / name).relative_to(root).as_posix())
    require(any(PurePosixPath(path).name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING'))
                for path in paths), 'Dependency has no supplied license/copying text')
    require(len(paths) <= MAX_FILES, 'Redistribution file count bound')
    return [(path, read_file(root, path)) for path in sorted(paths)]


def add_component(components, texts, identity, provenance, files):
    require(identity and len(identity) <= 1024 and not any(ord(character) < 32 or ord(character) == 127 for character in identity),
            'Unsafe redistribution component identity')
    require(identity not in {component['id'] for component in components}, 'Duplicate redistribution component')
    require(len(components) < MAX_COMPONENTS, 'Redistribution component count bound')
    entries = []
    for path, data in files:
        relative_parts(path)
        require(len(texts) < MAX_FILES, 'Redistribution total file count bound')
        require(sum(len(value) for _, _, value in texts) + len(data) <= MAX_TOTAL,
                'Redistribution total size bound')
        entries.append(dict(path=path, sha256=sha256(data), bytes=len(data)))
        texts.append((identity, path, data))
    components.append(dict(id=identity, provenance=provenance, files=entries, review=REVIEW))


def json_stream(raw):
    decoder = json.JSONDecoder()
    while raw.strip():
        raw = raw.lstrip()
        item, end = decoder.raw_decode(raw)
        yield item
        raw = raw[end:]


def run(root, *arguments):
    return subprocess.check_output(arguments, cwd=root, text=True, timeout=120)


def valid_sum(value):
    if not isinstance(value, str) or not value.startswith('h1:'):
        return False
    try:
        return len(base64.b64decode(value[3:], validate=True)) == 32
    except ValueError:
        return False


def valid_integrity(value):
    if not isinstance(value, str) or len(value) > 1024 or '-' not in value:
        return False
    algorithm, encoded = value.split('-', 1)
    sizes = {'sha1': 20, 'sha256': 32, 'sha384': 48, 'sha512': 64}
    try:
        decoded = base64.b64decode(encoded, validate=True)
        return len(decoded) == sizes.get(algorithm, 0) and base64.b64encode(decoded).decode() == encoded
    except ValueError:
        return False


def verify_npm_paths(files, locations):
    expected = {'/' + location for location in locations}
    for path in files:
        if not path.startswith('/node_modules'):
            continue
        if path == '/node_modules' or path.endswith('/node_modules'):
            require(any(root.startswith(path + '/') for root in expected), 'Uninventoried npm directory in production ASAR')
            continue
        marker = path.rfind('/node_modules/')
        require(marker >= 0, 'Malformed npm path in production ASAR')
        tail = path[marker + len('/node_modules/'):].split('/')
        if tail[0].startswith('@') and len(tail) == 1:
            require(any(root.startswith(path + '/') for root in expected), 'Uninventoried npm scope in production ASAR')
            continue
        require(tail[0] and (not tail[0].startswith('@') or len(tail) > 1 and tail[1]), 'Invalid npm package path')
        name = '/'.join(tail[:2]) if tail[0].startswith('@') else tail[0]
        package = path[:marker] + '/node_modules/' + name
        require(package in expected, 'Uninventoried npm package in production ASAR')


def collect_go(root, components, texts):
    # Check downloaded content against the module checksums before copying text.
    run(root, 'go', 'mod', 'verify')
    cache = Path(run(root, 'go', 'env', 'GOMODCACHE').strip())
    modules = {}
    for package in json_stream(run(root, 'go', 'list', '-deps', '-json',
                                   '-mod=readonly',
                                   *('./cmd/' + command for command in COMMANDS))):
        module = package.get('Module')
        if module and not module.get('Main'):
            require('Replace' not in module and module.get('Version', '').startswith('v') and
                    valid_sum(module.get('Sum')), 'Unpinned or replaced shipped Go dependency')
            old = modules.setdefault(module['Path'], module)
            require((old['Version'], old['Sum']) == (module['Version'], module['Sum']),
                    'Conflicting shipped Go module identity')
    require(modules, 'Production Go dependency closure is empty')
    for name, module in sorted(modules.items()):
        source = Path(module.get('Dir', ''))
        require(source.is_absolute() and source.is_relative_to(cache) and source != cache,
                'Go module source is outside the pinned module cache')
        add_component(components, texts, 'go:' + name + '@' + module['Version'],
                      dict(kind='go-module', module=name, version=module['Version'],
                           goModuleDirectorySum=module['Sum'],
                           verification='go mod verify; actual command go list -deps closure'),
                      collect_texts(source))
    toolchain = Path(run(root, 'go', 'env', 'GOROOT').strip())
    tool_version = run(root, 'go', 'version').strip()
    add_component(components, texts, 'go-toolchain:' + tool_version,
                  dict(kind='go-standard-library', version=tool_version,
                       verification='Selected build toolchain GOROOT license and patent texts'),
                  [(name, read_file(toolchain, name)) for name in ('LICENSE', 'PATENTS')])
    return dict(commands=list(COMMANDS), dependencySelection='Actual production command dependency closure; excludes graph-only and test-only modules',
                goModSHA256=sha256(read_file(root, 'go.mod')), goSumSHA256=sha256(read_file(root, 'go.sum')),
                toolchain=tool_version)


ASAR_INSPECT = r'''
import fs from 'node:fs';import path from 'node:path';import crypto from 'node:crypto';
const root=process.argv[1],runtime=process.argv[2];
const asar=await import(path.join(root,'desktop/node_modules/@electron/asar/lib/asar.js'));
const requested=JSON.parse(fs.readFileSync(0,'utf8'));
const archive=path.join(runtime,'resources/app.asar'),files=asar.listPackage(archive),hashes={};
for(const file of requested){if(!files.includes('/'+file))throw Error('missing required package file');
hashes[file]=crypto.createHash('sha256').update(asar.extractFile(archive,file)).digest('hex');}
process.stdout.write(JSON.stringify({files,hashes}));
'''


def collect_desktop(root, runtime, components, texts):
    runtime = Path(runtime)
    # Validate every selected source with no-follow reads before Node inspection.
    lock_bytes = read_file(root, 'desktop/package-lock.json')
    lock = json.loads(lock_bytes)
    require(lock.get('lockfileVersion') == 3 and isinstance(lock.get('packages'), dict), 'Unsupported npm lock schema')
    electron = lock['packages'].get('node_modules/electron', {})
    require(electron.get('version') and valid_integrity(electron.get('integrity')), 'Unpinned Electron runtime')
    required = {location: item for location, item in lock['packages'].items()
                if location and not item.get('dev', False)}
    npm = []
    requested = []
    for location, item in sorted(required.items()):
        relative_parts(location)
        require(location.startswith('node_modules/') and not item.get('link') and item.get('version') and
                valid_integrity(item.get('integrity')), 'Unpinned or linked production npm package')
        source = root / 'desktop' / location
        package_bytes = read_file(source, 'package.json')
        package = json.loads(package_bytes)
        require(package.get('version') == item['version'] and package.get('name'), 'Installed npm identity differs from lock')
        files = collect_texts(source, prune_node_modules=True)
        npm.append((location, item, package, files))
        requested.append(location + '/package.json')
        requested.extend(location + '/' + path for path, _ in files)
    require(len(requested) <= MAX_FILES, 'Bundled npm notice file bound')
    read_file(runtime, 'resources/app.asar', maximum=64 << 20, text=False)
    inspected = json.loads(subprocess.check_output(['node', '--input-type=module', '-e', ASAR_INSPECT,
                           str(root), str(runtime)], input=json.dumps(requested), text=True, timeout=60, cwd=root))
    verify_npm_paths(inspected['files'], required)
    for location, item, package, files in npm:
        package_bytes = read_file(root / 'desktop' / location, 'package.json')
        require(inspected['hashes'].get(location + '/package.json') == sha256(package_bytes), 'Packaged npm metadata differs from pinned source')
        for path, data in files:
            require(inspected['hashes'].get(location + '/' + path) == sha256(data), 'Packaged npm license differs from pinned source')
        add_component(components, texts, 'npm:' + package['name'] + '@' + item['version'],
                      dict(kind='production-npm', name=package['name'], version=item['version'],
                           npmIntegrity=item['integrity'], packagedLocation=location,
                           verification='Pinned lock metadata and exact ASAR license/metadata byte comparison'), files)
    # Electron's Chromium file includes its bundled third-party notices. Preserve
    # the full upstream HTML verbatim as text; do not execute or reclassify it.
    add_component(components, texts, 'electron:' + electron['version'],
                  dict(kind='electron-runtime', version=electron['version'], npmIntegrity=electron['integrity'],
                       verification='Pinned Electron package version; actual packaged runtime license bytes',
                       runtimeSHA256=sha256(read_file(runtime, 'cryptowallet-ui', maximum=256 << 20, text=False))),
                  [(name, read_file(runtime, name)) for name in ('LICENSE', 'LICENSES.chromium.html')])
    return dict(electron=electron['version'], npmLockSHA256=sha256(lock_bytes), productionNpmPackages=len(npm),
                asarSHA256=sha256(read_file(runtime, 'resources/app.asar', maximum=64 << 20, text=False)))


def render(components, texts, scope, provenance):
    require(components and texts, 'No redistribution notices collected')
    components = sorted(components, key=lambda entry: entry['id'])
    header = ('LocalWallet third-party redistribution texts\n\n'
              'Technical inventory only. ' + REVIEW + '.\n'
              'License classifications, compatibility and obligations are not determined by this file.\n'
              'Upstream text bytes are reproduced below without substantive modification.\n\n')
    body = bytearray(header.encode())
    for identity, path, data in sorted(texts, key=lambda entry: (entry[0], entry[1])):
        body.extend(('===== ' + identity + ' / ' + path + ' =====\n'
                     'SHA256: ' + sha256(data) + '\n\n').encode())
        body.extend(data)
        body.extend(b'\n\n')
    manifest = dict(schemaVersion=1, status='PASS', scope=scope, review=REVIEW,
                    inventoryMeaning='Selected upstream text inventory, not legal clearance or a completeness opinion on obligations',
                    provenance=provenance, components=components, noticeSHA256=sha256(body),
                    externalRuntimeLibraries='System libc/libseccomp2 and desktop shared libraries are runtime prerequisites, not copied from the host into this notice inventory; review their use and distribution obligations separately.',
                    projectLicense='NOT_REVIEWED: this artifact does not assign a license to LocalWallet source')
    return bytes(body), json_bytes(manifest)


def write_output(folder, name, data):
    require(len(relative_parts(name)) == 1, 'Unsafe notice output name')
    folder = Path(folder)
    folder.mkdir(parents=True, exist_ok=True)
    descriptor = directory(folder)
    temporary = None
    try:
        require(os.fstat(descriptor).st_uid == os.getuid(), 'Notice output directory is not owned by this user')
        try:
            before = os.stat(name, dir_fd=descriptor, follow_symlinks=False)
            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == os.getuid(), 'Unsafe notice output target')
        except FileNotFoundError:
            pass
        handle, temporary = tempfile.mkstemp(prefix='.notice-', dir=folder)
        try:
            os.fchmod(handle, 0o644)
            with os.fdopen(handle, 'wb') as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(Path(temporary).name, name, src_dir_fd=descriptor, dst_dir_fd=descriptor)
            os.fsync(descriptor)
        finally:
            if temporary:
                Path(temporary).unlink(missing_ok=True)
    finally:
        os.close(descriptor)


def generate(root, go_only=False, runtime=None):
    root = Path(root).absolute()
    components, texts = [], []
    provenance = collect_go(root, components, texts)
    if not go_only:
        runtime = Path(runtime or root / 'dist/LocalWallet-linux-x64').absolute()
        provenance['desktop'] = collect_desktop(root, runtime, components, texts)
    scope = 'Actual production Go command dependencies and Go standard-library texts'
    if not go_only:
        scope += '; complete packaged Electron/Chromium runtime and exact production npm ASAR notices'
    text, manifest = render(components, texts, scope, provenance)
    prefix = 'go-' if go_only else ''
    for extension, data in (('txt', text), ('json', manifest)):
        name = prefix + 'THIRD-PARTY-NOTICES.' + extension
        write_output(root / 'dist', name, data)
        if not go_only:
            write_output(runtime, 'THIRD-PARTY-NOTICES.' + extension, data)
    return json.loads(manifest)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go-only', action='store_true', help='No npm/Node/Electron access')
    parser.add_argument('--desktop-runtime', type=Path, help='Actual packaged runtime, before archiving')
    arguments = parser.parse_args()
    try:
        manifest = generate(ROOT, arguments.go_only, arguments.desktop_runtime)
        print('PASS redistribution texts: ' + str(len(manifest['components'])) + ' components; legal review NOT_RUN')
        return 0
    except (NoticeError, OSError, subprocess.SubprocessError, ValueError, KeyError):
        print('FAIL redistribution notice collection: missing/unsafe/unpinned source or package mismatch')
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
