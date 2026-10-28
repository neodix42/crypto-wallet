#!/usr/bin/env python3
"""Fail before dependency caching when a checkout omits Wails build inputs.

Only Python's standard library and Git are needed. Read the index rather than
HEAD so a developer can verify a fully staged change before committing it.
Generated frontend output and installed npm dependencies are not source inputs.
"""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
REQUIRED = (
    'go.mod', 'go.sum', 'Makefile', 'VERSION', 'cmd/cryptowallet/main.go',
    'scripts/build.sh', 'scripts/build-pinned.sh', 'scripts/build-frontend.sh',
    'scripts/native-env.sh', 'scripts/with-go.sh', 'scripts/with-toolchain.sh',
    'scripts/check-build-inputs.py', 'scripts/verify-wails-fork.py',
    'scripts/license-notices.py', 'scripts/package-portable.py', 'scripts/install-user.sh',
    'internal/webui/window.go', 'internal/webui/prompt.go',
    'internal/webui/frontend/package.json', 'internal/webui/frontend/package-lock.json',
    'internal/webui/frontend/tsconfig.json', 'internal/webui/frontend/vite.config.ts',
    'internal/webui/frontend/index.html', 'internal/webui/frontend/secret/index.html',
    'internal/webui/frontend/src/main.tsx', 'internal/webui/frontend/src/private-main.tsx',
    'third_party/wails/go.mod', 'third_party/wails/WALLET-UPSTREAM.json',
    'third_party/wails/WALLET-PATCHES.json', '.github/provenance/package-lock.json',
)
SOURCE_TREES = ('internal/webui', 'internal/branding', 'internal/profilepath', 'third_party/wails')
GENERATED = ('internal/webui/frontend/dist', 'internal/webui/frontend/node_modules')


class BuildInputError(ValueError):
    pass


def generated(name):
    return any(name == prefix or name.startswith(prefix + '/') for prefix in GENERATED)


def verify(root=ROOT):
    root = Path(root)
    tracked = {}
    listing = subprocess.check_output(
        ['git', '-C', str(root), 'ls-files', '--stage', '-z', '--', *REQUIRED, *SOURCE_TREES],
        stderr=subprocess.PIPE)
    for record in listing.decode('utf-8').split('\0'):
        if not record:
            continue
        metadata, name = record.split('\t', 1)
        mode, _object, stage = metadata.split()
        if stage != '0':
            raise BuildInputError('Unmerged build input: ' + name)
        tracked[name] = mode

    actual = set(REQUIRED)
    for name in SOURCE_TREES:
        directory = root / name
        if directory.is_symlink() or not directory.is_dir():
            raise BuildInputError('Missing or linked source directory: ' + name)
        for path in directory.rglob('*'):
            relative = path.relative_to(root).as_posix()
            if generated(relative):
                continue
            if path.is_symlink():
                raise BuildInputError('Linked build input: ' + relative)
            if path.is_file():
                actual.add(relative)

    for name in sorted(actual | {name for name in tracked if not generated(name)}):
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise BuildInputError('Missing or linked build input: ' + name)
        if name not in tracked:
            raise BuildInputError('Untracked build input; add it to the commit: ' + name)
        if tracked[name] not in ('100644', '100755'):
            raise BuildInputError('Build input is not a regular tracked file: ' + name)

    frontend = root / 'internal/webui/frontend'
    package = json.loads((frontend / 'package.json').read_text())
    lock = json.loads((frontend / 'package-lock.json').read_text())
    locked_package = lock['packages']['']
    for key in ('name', 'version', 'dependencies', 'devDependencies'):
        if package.get(key) != locked_package.get(key):
            raise BuildInputError('Frontend package/lock mismatch: ' + key)

    spec = importlib.util.spec_from_file_location('wallet_build_fork', root / 'scripts/verify-wails-fork.py')
    fork = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(fork)
    result = fork.verify(root / 'third_party/wails')
    return {'status': 'PASS', 'trackedBuildInputs': len(actual), 'wails': result}


if __name__ == '__main__':
    try:
        print(json.dumps(verify(), indent=2))
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        # Inputs and paths are source-only; do not print subprocess environments.
        print('FAIL build inputs: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
