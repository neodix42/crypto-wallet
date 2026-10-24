#!/usr/bin/env python3
"""Compile a fixed non-Decred soak test binary and retain live, source-bound state.

The deterministic fake-ledger workload uses public fixtures and production KDF
settings. It never contacts a public testnet or mainnet. A completed narrower
campaign cannot satisfy the all-chain production soak gate.
"""
import argparse
import hashlib
import json
import math
import os
import pathlib
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]


def hashes():
    paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others',
                                    '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    return {p: hashlib.sha256((ROOT / p).read_bytes()).hexdigest()
            for p in sorted(set(paths)) if p and (p.endswith('.go') or p in
                ('go.mod', 'go.sum', 'scripts/run-soak.py')) and (ROOT / p).is_file()
            and not p.startswith(('.tools/', 'reports/', 'bin/', 'dist/'))}


def save(path, data):
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(data, indent=2) + '\n')
    temporary.replace(path)


def validate_workload(result, seconds):
    elapsed = result.get('elapsedSeconds')
    requested = result.get('requestedSeconds')
    cycles = result.get('workloads', {}).get('cycles')
    if (result.get('executionStatus') != 'PASS' or result.get('status') != 'NOT_RUN'
            or type(requested) not in (int, float) or requested != seconds
            or type(elapsed) not in (int, float) or not math.isfinite(elapsed) or elapsed < seconds
            or type(cycles) is not int or cycles < 1
            or result.get('excluded') != ['Decred work deferred by owner; no public testnet activity']
            or result.get('campaign24hStatus') != 'NOT_RUN'):
        raise ValueError('Incomplete or wrong-scope soak evidence')
    final = result.get('finalState', {})
    if (any(type(final.get(k)) is not int or final[k] != cycles for k in
            ('lifecycleConfirmed', 'lifecycleCancelledBeforeSignature', 'lifecycleFakeLedgerPayments'))
            or type(final.get('lifecycleUnresolved')) is not int or final['lifecycleUnresolved'] != 0
            or type(result.get('workloads', {}).get('productionSignedJournalReconciled')) is not int
            or result['workloads']['productionSignedJournalReconciled'] != cycles
            or any(k.startswith('DCR') for k in result.get('workloads', {}))):
        raise ValueError('Soak journal/ledger reconciliation is incomplete')
    expected = 'PASS' if seconds >= 86400 and elapsed >= 86400 else 'NOT_RUN'
    if result.get('scopedCampaign24hStatus') != expected:
        raise ValueError('Soak duration does not establish the claimed campaign')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--seconds', type=int, default=86400)
    args = parser.parse_args()
    if not 1 <= args.seconds <= 86400:
        parser.error('--seconds must be 1..86400')
    reports = ROOT / 'reports'
    reports.mkdir(exist_ok=True)
    report_path = reports / 'nondecred-soak-run.json'
    report = dict(status='RUNNING', executionStatus='RUNNING', requestedSeconds=args.seconds,
        sourceSHA256=hashes(), sourceCommit=subprocess.check_output(
            ['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
        startedAt=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        scope='Production signer/KDF and exact EVM signed-journal fake-ledger lifecycle; explicit non-Decred scope',
        excluded=['Decred work', 'Public testnet tests', 'Desktop/OS lifecycle and other-chain consensus acceptance'],
        campaign24hStatus='NOT_RUN', allChainReleaseGate='NOT_RUN')
    save(report_path, report)
    binary = reports / 'nondecred-soak.test'
    start = time.monotonic()
    try:
        subprocess.run(['go', 'test', '-c', '-o', str(binary), './tests/stress'], cwd=ROOT,
                       check=True, timeout=120)
        if hashes() != report['sourceSHA256']:
            raise RuntimeError('Source changed during test compilation')
        report['testBinarySHA256'] = hashlib.sha256(binary.read_bytes()).hexdigest()
        # A zero-exit test selector that ran no test must not reuse an old PASS.
        workload_path = reports / 'soak.json'
        if workload_path.is_symlink():
            raise RuntimeError('Unsafe workload report target')
        workload_path.unlink(missing_ok=True)
        with (reports / 'nondecred-soak.log').open('w') as output:
            process = subprocess.Popen([str(binary), '-test.run=^TestTP072Soak$',
                '-test.count=1', '-test.timeout=' + str(args.seconds + 600) + 's',
                '-soak=' + str(args.seconds) + 's', '-soak-non-decred'], cwd=ROOT / 'tests/stress',
                stdout=output, stderr=subprocess.STDOUT, env={**os.environ, 'GOTRACEBACK': 'none'})
            report['pid'] = process.pid
            save(report_path, report)
            print('RUNNING: reports/nondecred-soak-run.json; live workload counters: reports/soak.json', flush=True)
            code = process.wait(timeout=args.seconds + 660)
        result = json.loads((reports / 'soak.json').read_text())
        validate_workload(result, args.seconds)
        ok = code == 0 and hashlib.sha256(binary.read_bytes()).hexdigest() == report['testBinarySHA256']
        if hashes() != report['sourceSHA256']:
            ok = False
            report['reason'] = 'Source changed during campaign; pinned binary was tested, current source needs rerun'
        report.update(status='PASS' if ok else 'FAIL', executionStatus='PASS' if ok else 'FAIL',
                      exit=code, elapsedSeconds=round(time.monotonic() - start, 3),
                      campaign24hStatus=result.get('scopedCampaign24hStatus', 'NOT_RUN'),
                      workloadReportSHA256=hashlib.sha256((reports / 'soak.json').read_bytes()).hexdigest())
        if ok and args.seconds < 86400:
            report['status'] = 'NOT_RUN'
            report['reason'] = 'Short execution passed; the required duration has not run'
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        report.update(status='FAIL', executionStatus='FAIL', reason='Soak compile/process/evidence check failed')
        if 'process' in locals() and process.poll() is None:
            process.kill()
            process.wait(timeout=10)
    finally:
        save(report_path, report)
    print('Soak execution: ' + report['executionStatus'] + '; scoped 24h: ' + report['campaign24hStatus'])
    return report['executionStatus'] != 'PASS'


if __name__ == '__main__':
    raise SystemExit(main())
