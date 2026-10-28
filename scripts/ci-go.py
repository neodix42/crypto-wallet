#!/usr/bin/env python3
"""Run current Go checks without executing the historical Electron launcher tests.

Package discovery remains dynamic: new current packages are included by default.
The exact legacy launcher package is the sole exclusion. Local make test and
test-race still exercise their original complete package sets.

Race-instrumented stress runs after the other race packages have completed. Its
production-KDF latency budgets measure an isolated workload rather than competing
with the signer suite for the hosted runner's two CPUs and memory bandwidth.
"""
import argparse
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
LEGACY_PACKAGE = 'localwallet/cmd/wallet-launcher'
STRESS_PACKAGE = 'localwallet/tests/stress'
REQUIRED = {'localwallet/cmd/cryptowallet', 'localwallet/internal/webui',
            'localwallet/internal/nativewallet', 'localwallet/internal/walletcore',
            'localwallet/internal/walletsigner', 'localwallet/internal/signer',
            STRESS_PACKAGE}


def packages(root=ROOT):
    listed = subprocess.check_output(['go', 'list', './...'], cwd=root, text=True).splitlines()
    if (not listed or len(set(listed)) != len(listed) or
            any(not re.fullmatch(r'localwallet/[A-Za-z0-9_./-]+', name) for name in listed) or
            not REQUIRED <= set(listed)):
        raise ValueError('Current Go package discovery is incomplete or invalid')
    return [name for name in listed if name != LEGACY_PACKAGE]


def run(action, root=ROOT):
    selected = packages(root)
    if action == 'race':
        commands = [
            ['go', 'test', '-race', '-p', '2'] + [name for name in selected if name != STRESS_PACKAGE],
            # Keep the complete stress package and every existing budget. A
            # fresh execution is required for meaningful performance evidence.
            ['go', 'test', '-race', '-p', '1', '-count=1', STRESS_PACKAGE],
        ]
    else:
        commands = [{'test': ['go', 'test'], 'vet': ['go', 'vet']}[action] + selected]
    failure = 0
    for command in commands:
        result = subprocess.run(command, cwd=root, check=False).returncode
        # Like the previous single go-test invocation, still attempt every
        # selected package after a test failure, while preserving a failing exit.
        if result and not failure:
            failure = result
    return failure


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('test', 'race', 'vet'))
    args = parser.parse_args()
    return run(args.action)


if __name__ == '__main__':
    raise SystemExit(main())
