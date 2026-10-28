#!/usr/bin/env python3
"""Check the complete pinned Wails source against its explicit reviewed patches."""
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
FORK = ROOT / 'third_party/wails'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify(directory=FORK):
    upstream = json.loads((directory / 'WALLET-UPSTREAM.json').read_text())
    patches = json.loads((directory / 'WALLET-PATCHES.json').read_text())
    if upstream['module'] != 'github.com/wailsapp/wails/v2' or upstream['version'] != 'v2.17.0':
        raise ValueError('Unexpected upstream module pin')
    if patches['upstreamManifestSHA256'] != digest(directory / 'WALLET-UPSTREAM.json'):
        raise ValueError('Upstream source inventory changed')
    originals = upstream['files']
    if len(originals) != 963:
        raise ValueError('Incomplete pinned upstream source inventory')
    expected = dict(originals)
    for name, entry in patches['files'].items():
        if entry['upstreamSHA256'] != originals.get(name):
            raise ValueError('Patch baseline changed: ' + name)
        expected[name] = entry['sha256']
    expected['WALLET-UPSTREAM.json'] = patches['upstreamManifestSHA256']
    ignored = {'WALLET-PATCHES.json'}
    actual = {}
    for path in directory.rglob('*'):
        if path.is_symlink():
            raise ValueError('Symbolic link in pinned fork')
        if path.is_file() and path.relative_to(directory).as_posix() not in ignored:
            actual[path.relative_to(directory).as_posix()] = digest(path)
    if actual != expected:
        failures = sorted(set(actual) ^ set(expected))
        failures += sorted(name for name in actual.keys() & expected.keys() if actual[name] != expected[name])
        raise ValueError('Unlisted or changed Wails source: ' + ', '.join(failures[:20]))
    return {'status': 'PASS', 'module': upstream['module'], 'version': upstream['version'],
            'upstreamFiles': len(originals), 'explicitWalletFiles': len(patches['files']),
            'finalFiles': len(actual) + 1}


if __name__ == '__main__':
    try:
        print(json.dumps(verify(), indent=2))
    except (OSError, ValueError, KeyError) as error:
        print('FAIL Wails fork: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
