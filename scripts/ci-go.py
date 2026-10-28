#!/usr/bin/env python3
"""Run current Go checks without executing the historical Electron launcher tests.

Package discovery remains dynamic: new current packages are included by default.
The exact legacy launcher package is the sole exclusion. Local make test and
test-race still exercise their original complete package sets.
"""
import argparse
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
LEGACY_PACKAGE = 'localwallet/cmd/wallet-launcher'
REQUIRED = {'localwallet/cmd/cryptowallet', 'localwallet/internal/webui',
            'localwallet/internal/nativewallet', 'localwallet/internal/walletcore',
            'localwallet/internal/walletsigner', 'localwallet/internal/signer'}


def packages(root=ROOT):
    listed = subprocess.check_output(['go', 'list', './...'], cwd=root, text=True).splitlines()
    if (not listed or len(set(listed)) != len(listed) or
            any(not re.fullmatch(r'localwallet/[A-Za-z0-9_./-]+', name) for name in listed) or
            not REQUIRED <= set(listed)):
        raise ValueError('Current Go package discovery is incomplete or invalid')
    return [name for name in listed if name != LEGACY_PACKAGE]


def run(action, root=ROOT):
    commands = {'test': ['go', 'test'], 'race': ['go', 'test', '-race', '-p', '2'],
                'vet': ['go', 'vet']}
    command = commands[action] + packages(root)
    return subprocess.run(command, cwd=root, check=False).returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('test', 'race', 'vet'))
    args = parser.parse_args()
    return run(args.action)


if __name__ == '__main__':
    raise SystemExit(main())
