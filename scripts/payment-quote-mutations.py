#!/usr/bin/env python3
"""Compile three quote/reservation security mutants in a disposable source copy.

Controls must execute the exact test successfully. Only its named runtime
assertion kills a mutant; build failures, skips, timeouts and panics are invalid.
No wallet profile, public RPC, secret input or funds are used.
"""
import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / 'reports/payment-quote-mutations.json'
spec = importlib.util.spec_from_file_location('payment_quote_mutation_helpers', ROOT / 'scripts/near-token-mutation.py')
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)

MUTANTS = [
    helpers.mutant('quote-cannot-create-reservation', 'Max and fee previews never durably prepare an operation.',
        'internal/walletcore/main.go', [('if readOnly {\n\t\t\tif !validPaymentQuotePlan', 'if false && readOnly {\n\t\t\tif !validPaymentQuotePlan')],
        './internal/walletcore', 'TestPaymentQuoteAndLegacyMaxNeverReserveAnAccount', '',
        'read-only quote created a durable PREPARED operation'),
    helpers.mutant('pending-original-signed-bytes-stay-private', 'Pending review exposes original public effects without signed bytes.',
        'internal/walletcore/payment_quote.go', [('public := *operation\n\t\tpublic.Signed = nil', 'public := *operation\n\t\t// MUTANT: retain the original signed artifact')],
        './internal/walletcore', 'TestPaymentQuotePreservesOriginalSignedReservationAndPendingLookupStripsBytes', '',
        'pending lookup leaked signed authority or lost original plan'),
    helpers.mutant('prepare-keeps-original-account-reservation', 'Explicit preparation cannot overwrite an unresolved original operation.',
        'internal/walletcore/main.go', [('if pending != nil {\n\t\t\t\treturn nil, ipc.NewRequestError("PAYMENT_PENDING")', 'if false && pending != nil {\n\t\t\t\treturn nil, ipc.NewRequestError("PAYMENT_PENDING")')],
        './internal/walletcore', 'TestPaymentQuoteAndLegacyMaxNeverReserveAnAccount', '',
        'existing prepared operation was replaced or fresh RPC ran'),
]


def source_paths(env):
    raw = subprocess.check_output(['go', 'list', '-deps', '-test', '-json', './internal/walletcore'],
                                  cwd=ROOT, env=env, text=True, timeout=60)
    decoder = json.JSONDecoder()
    paths = {'go.mod', 'go.sum', 'third_party/wails/go.mod', 'third_party/wails/go.sum',
             'scripts/payment-quote-mutations.py', 'scripts/payment-quote-mutation-test.py',
             'scripts/near-token-mutation.py'}
    while raw.strip():
        item, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        directory = pathlib.Path(item.get('Dir', '/'))
        for key in ('GoFiles', 'CgoFiles', 'TestGoFiles', 'XTestGoFiles', 'EmbedFiles'):
            for name in item.get(key, []):
                path = directory / name
                if path.is_file() and path.is_relative_to(ROOT):
                    paths.add(str(path.relative_to(ROOT)))
    for path in (ROOT / 'tests/oracles').rglob('*'):
        if path.is_file() and not {'node_modules', '__pycache__'} & set(path.parts):
            paths.add(str(path.relative_to(ROOT)))
    return sorted(paths)


def write_report(report):
    REPORT.parent.mkdir(exist_ok=True)
    temporary = REPORT.with_suffix('.tmp')
    temporary.write_text(json.dumps(report, indent=2) + '\n')
    temporary.replace(REPORT)


def main():
    os.umask(0o077)
    started = time.monotonic()
    report = dict(status='RUNNING', scope=__doc__, controls=[], mutants=[])
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('WALLET_', 'LOCALWALLET_'))}
    env['GOWORK'] = 'off'
    try:
        subprocess.run([sys.executable, str(ROOT / 'scripts/payment-quote-mutation-test.py')],
                       cwd=ROOT, env=env, check=True, timeout=30)
        paths = source_paths(env)
        report['sourceSHA256'] = {name: helpers.sha(ROOT / name) for name in paths}
        report['goVersion'] = subprocess.check_output(['go', 'version'], env=env, text=True, timeout=30).strip()
        write_report(report)
        with tempfile.TemporaryDirectory(prefix='cryptowallet-payment-quote-mutants-') as directory:
            temporary = pathlib.Path(directory)
            snapshot = temporary / 'source'
            snapshot.mkdir()
            ignore = shutil.ignore_patterns('node_modules', 'dist', '__pycache__')
            for name in ('internal', 'tests/oracles'):
                shutil.copytree(ROOT / name, snapshot / name, ignore=ignore)
            for name in ('go.mod', 'go.sum', 'third_party/wails/go.mod', 'third_party/wails/go.sum',
                         'scripts/payment-quote-mutations.py', 'scripts/payment-quote-mutation-test.py', 'scripts/near-token-mutation.py'):
                target = snapshot / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            if any(helpers.sha(snapshot / name) != expected for name, expected in report['sourceSHA256'].items()):
                raise RuntimeError('source changed while copying mutation inputs')
            for item in MUTANTS:
                row = {'id': item['id'], **helpers.run(snapshot, item, env, control=True)}
                report['controls'].append(row)
                write_report(report)
                if row['status'] != 'PASS':
                    raise RuntimeError('unmodified exact control failed')
            print('Payment quote mutation controls: PASS', flush=True)
            for index, item in enumerate(MUTANTS):
                overlay, digest = helpers.overlay_mutant(snapshot, temporary, index, item)
                row = {'id': item['id'], 'policy': item['policy'], 'mutantSHA256': digest,
                       **helpers.run(snapshot, item, env, overlay)}
                report['mutants'].append(row)
                write_report(report)
                print(item['id'], row['status'], flush=True)
        report['sourceSHA256After'] = {name: helpers.sha(ROOT / name) for name in source_paths(env)}
        report['sourceStable'] = report['sourceSHA256After'] == report['sourceSHA256']
        report['status'] = 'PASS' if report['sourceStable'] and len(report['mutants']) == len(MUTANTS) and all(row['status'] == 'KILLED' for row in report['mutants']) else 'FAIL'
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        report.update(status='FAIL', reason='Control, source snapshot or mutation execution did not establish the required assertions')
    report['elapsedSeconds'] = round(time.monotonic() - started, 3)
    write_report(report)
    return int(report['status'] != 'PASS')


if __name__ == '__main__':
    raise SystemExit(main())
