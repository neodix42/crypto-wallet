#!/usr/bin/env python3
"""Bind the executed local Exchange checks; this never performs exchange calls.

PASS is restricted to the local scopes named in the input reports. Private
Bybit acceptance, funded roundtrips and production approval remain separate.
"""
import json
import pathlib
import time

from evidence_binding import (candidate, go_sources, read_json, sha256,
                              validate_artifact, validate_frontend, validate_go,
                              validate_packaged, validate_source_report)

ROOT = pathlib.Path(__file__).resolve().parents[1]
NAMES = ('go-coverage', 'frontend-coverage', 'artifact-security', 'bybit-client',
         'bybit-client-mutations', 'exchange-core', 'exchange-signermutation',
         'usdt-execution', 'fuzz', 'mutation', 'phrase-recovery',
         'crash-security', 'packaged-recovery', 'packaged-send')


def main():
    report = dict(status='RUNNING', scope='Executed local Exchange/security/recovery '
                  'and isolated EVM checks only; no private Bybit or funded acceptance',
                  livePrivateExchange='NOT_RUN', mainnetFundMovement='NOT_RUN', checks=[])
    destination = ROOT / 'reports/exchange.json'

    def save():
        temporary = destination.with_suffix('.tmp')
        temporary.write_text(json.dumps(report, indent=2) + '\n')
        temporary.replace(destination)

    save()
    try:
        inputs = {}
        for name in NAMES:
            data = read_json(ROOT, 'reports/' + name + '.json')
            if data.get('status') != 'PASS':
                raise ValueError('A required local report has not passed')
            sources = data.get('sourceSHA256', data.get('sourceHashes'))
            if sources:
                validate_source_report(ROOT, {'status': data['status'], 'sourceSHA256': sources}, tuple(sources))
            inputs[name] = data
            report['checks'].append(dict(report=name, status='PASS',
                sha256=sha256(ROOT / ('reports/' + name + '.json'))))
        current = candidate(ROOT)
        validate_go(ROOT, inputs['go-coverage'], read_json(ROOT, 'reports/coverage.json'))
        validate_frontend(ROOT, inputs['frontend-coverage'])
        validate_artifact(ROOT, inputs['artifact-security'], current)
        for name in ('packaged-recovery', 'packaged-send'):
            validate_packaged(inputs[name], current)
        recovery = inputs['packaged-recovery']
        if (len(recovery.get('steps', [])) < 23 or recovery.get('sourceStable') is not True
                or recovery.get('producerSHA256') != sha256(ROOT / 'scripts/packaged-recovery.py')):
            raise ValueError('Packaged Exchange navigation/cancellation checks are incomplete')
        for name in ('phrase-recovery', 'crash-security'):
            if inputs[name].get('artifact', {}).get('binaryManifest') != current['binaryManifest']:
                raise ValueError('Recovery/crash binaries differ from the candidate')
        fuzz = inputs['fuzz']
        if len(fuzz.get('targets', [])) != 16 or any(row.get('fuzzExecuted') is not True for row in fuzz['targets']):
            raise ValueError('The configured local fuzz targets did not all execute')
        for name in ('bybit-client-mutations', 'exchange-signermutation'):
            rows = inputs[name].get('mutants', [])
            if len(rows) != 5 or any(row.get('status') != 'KILLED' for row in rows):
                raise ValueError('Exchange security policy mutants did not all fail at runtime')
        report.update(status='PASS', artifact=current, sourceSHA256=go_sources(ROOT),
                      completedAt=int(time.time()))
    except (OSError, ValueError, KeyError, TypeError):
        report.update(status='FAIL', reason='Missing, incomplete, stale or mismatched local evidence; inspect individual reports')
    save()
    print('Exchange local evidence:', report['status'], '(live Bybit acceptance NOT_RUN)')
    return report['status'] != 'PASS'


if __name__ == '__main__':
    raise SystemExit(main())
