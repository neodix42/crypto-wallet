#!/usr/bin/env python3
"""Bounded, source-bound local fuzz campaigns; never public-network tests."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import signal
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
TARGETS = [('uiprompt', 'FuzzPromptProtocol'), ('vault', 'FuzzTP012Envelope'),
    ('derivation', 'FuzzTP012Mnemonic'), ('ipc', 'FuzzTP012IPC'),
    ('strictjson', 'FuzzTP012JSON'), ('amount', 'FuzzTP012Amount'),
    ('marketdata', 'FuzzPublicPriceJSON'),
    ('qr', 'FuzzTP012QR'), ('chains/chainutil', 'FuzzJSON'),
    ('chains/evm', 'FuzzTP012AddressAndPlan'), ('chains/evm', 'FuzzERC20TokenPolicy'), ('chains/hypercore', 'FuzzTP012CoreAction'),
    ('chains/near', 'FuzzAddress'), ('chains/near', 'FuzzStakingCallPolicy'),
    ('chains/near', 'FuzzNEP141Policy'), ('chains/near', 'FuzzTokenPlan'),
    ('chains/near', 'FuzzNEARHistoricalNativeActions'), ('chains/aptos', 'FuzzAddress'),
    ('chains/aptos', 'FuzzAPTNativeHistoryInterpretation'),
    ('chains/decred', 'FuzzAddressesScriptsOutpoints'), ('chains/ton', 'FuzzAddressAndBOC'),
    ('exchange/bybit', 'FuzzBybitAuthRequest'), ('exchange/bybit', 'FuzzBybitExecutionExtraFees')]


def executed_fuzz(log):
    # `go test -fuzz` exits zero when the pattern matches nothing. A campaign
    # must actually leave corpus gathering and execute mutations to count.
    return ('now fuzzing with ' in log and
            any(int(count) > 0 for count in re.findall(r'execs: ([0-9]+)', log)))


def sources(root):
    paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others',
                                     '--exclude-standard', '-z'], cwd=root).decode().split('\0')
    return {p: hashlib.sha256((root / p).read_bytes()).hexdigest()
            for p in sorted(set(paths)) if p and (p.endswith('.go') or p in
                ('go.mod', 'go.sum', 'scripts/fuzz.py')) and (root / p).is_file()
            and not p.startswith(('.tools/', 'bin/', 'dist/', 'reports/'))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--seconds', type=int, default=3, help='actual fuzz time per target, 1..3600')
    parser.add_argument('--exclude-package', action='append', default=[],
                        choices=[p for p, _ in TARGETS], help='explicitly deferred scope; full gate stays NOT_RUN')
    args = parser.parse_args()
    if not 1 <= args.seconds <= 3600:
        parser.error('--seconds must be 1..3600')
    excluded = sorted(set(args.exclude_package))
    reports = ROOT / 'reports'
    reports.mkdir(exist_ok=True)
    name = 'fuzz-long' if args.seconds > 3 else 'fuzz'
    before = sources(ROOT)
    report = dict(status='RUNNING', executionStatus='RUNNING', secondsPerTarget=args.seconds,
                  scope='Time-bounded local fuzz plus retained corpus; no public network or consensus acceptance',
                  excludedPackages=excluded, sourceSHA256=before, targets=[])
    def save():
        target = reports / (name + '.json')
        temporary = target.with_suffix('.tmp')
        temporary.write_text(json.dumps(report, indent=2) + '\n')
        temporary.replace(target)
    save()
    for package, target in TARGETS:
        if package in excluded:
            report['targets'].append(dict(package=package, target=target, status='NOT_RUN',
                                         reason='Explicit command scope exclusion'))
            save()
            continue
        start = time.monotonic()
        command = ['go', 'test', './internal/' + package, '-run', '^$', '-fuzz',
                   '^' + target + '$', '-fuzztime=' + str(args.seconds) + 's', '-parallel=2']
        log = reports / (name + '-' + package.replace('/', '-') + '-' + target + '.log')
        row = dict(package=package, target=target, status='FAIL', output=str(log.relative_to(ROOT)))
        try:
            with log.open('w') as output:
                process = subprocess.Popen(command, cwd=ROOT, env={**os.environ, 'GOMAXPROCS': '2'},
                                           stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
                try:
                    code = process.wait(timeout=args.seconds + 120)
                finally:
                    if process.poll() is None:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait(timeout=10)
            ran = executed_fuzz(log.read_text())
            row.update(status='PASS' if code == 0 and ran else 'FAIL', exit=code,
                       fuzzExecuted=ran)
            if code == 0 and not ran:
                row['reason'] = 'No measured mutation execution; missing target or baseline-only run'
        except (OSError, subprocess.TimeoutExpired):
            row['reason'] = 'Fuzz process unavailable or exceeded its bounded deadline'
        row['seconds'] = round(time.monotonic() - start, 3)
        report['targets'].append(row)
        print(json.dumps(row), flush=True)
        save()
    changed = sources(ROOT) != before
    executed = [r for r in report['targets'] if r['status'] != 'NOT_RUN']
    ok = bool(executed) and not changed and all(r['status'] == 'PASS' for r in executed)
    report['executionStatus'] = 'PASS' if ok else 'FAIL'
    report['status'] = ('NOT_RUN' if excluded else 'PASS') if ok else 'FAIL'
    report['sourceStable'] = not changed
    if changed:
        report['reason'] = 'Go sources/test/module pins changed during campaign; rerun on stable source'
    save()
    return not ok


if __name__ == '__main__':
    raise SystemExit(main())
