#!/usr/bin/env python3
"""Measure all production JS and retain the exact stable source/runtime inputs."""
import json
import pathlib
import subprocess

from evidence_binding import (EvidenceError, frontend_rows, frontend_runtime,
                              frontend_sources, read_json, sha256, wails_build,
                              wails_frontend_rows, wails_frontend_runtime,
                              wails_frontend_sources)

root = pathlib.Path(__file__).resolve().parents[1]


def measure_wails(base):
    report = {'status': 'FAIL', 'sourceStable': False,
              'measurement': 'Vitest 5.0.5 / coverage-v8 5.0.5, AST-remapped V8 statement/branch coverage; all shipped TS/TSX modules including both real entrypoints',
              'files': []}
    try:
        before = wails_frontend_sources(base)
        subprocess.run(['npm', '--prefix', 'internal/webui/frontend', 'ci', '--ignore-scripts', '--no-audit', '--no-fund'], cwd=base, check=True)
        subprocess.run(['npm', '--prefix', 'internal/webui/frontend', 'run', 'build'], cwd=base, check=True)
        compiled = wails_frontend_runtime(base)
        result = subprocess.run(['npm', '--prefix', 'internal/webui/frontend', 'run', 'coverage'], cwd=base, check=False)
        if before != wails_frontend_sources(base) or compiled != wails_frontend_runtime(base):
            raise EvidenceError('wails-frontend-source-or-runtime-changed-during-measurement')
        summary = read_json(base, 'reports/wails-frontend/coverage-summary.json')
        rows = wails_frontend_rows(base, summary)
        report.update(sourceStable=True, sourceSHA256=before, runtimeSHA256=compiled,
                      coverageSummarySHA256=sha256(base / 'reports/wails-frontend/coverage-summary.json'),
                      files=rows, status='PASS' if result.returncode == 0 and rows and all(row['status'] != 'FAIL' for row in rows) else 'FAIL')
        if result.returncode != 0:
            report['reason'] = 'wails-frontend-tests-or-coverage-failed'
    except EvidenceError as error:
        report['reason'] = str(error)
    except (OSError, ValueError, TypeError, subprocess.SubprocessError):
        report['reason'] = 'wails-frontend-measurement-failed'
    return report


def measure(base=root):
    base = pathlib.Path(base)
    reports = base / 'reports'
    reports.mkdir(exist_ok=True)
    report = {'status': 'FAIL', 'sourceStable': False,
              'measurement': 'Istanbul statement/branch coverage from c8/V8; every shipped JS file included, absent files fail',
              'files': []}
    if wails_build(base):
        report['wails'] = measure_wails(base)
    try:
        before = frontend_sources(base)
        subprocess.run(['npm', '--prefix', 'desktop', 'run', 'build'], cwd=base, check=True)
        compiled = frontend_runtime(base)
        tests = [str(path.relative_to(base / 'desktop')) for path in sorted((base / 'desktop/tests').rglob('*.test.cjs'))]
        if not tests:
            raise EvidenceError('frontend-test-set-empty')
        subprocess.run(['./node_modules/.bin/c8', '--all', '--include=app/**/*.js', '--include=ui/**/*.js',
                        '--exclude=tests/**', '--reporter=json-summary', '--reporter=text',
                        '--reports-dir=../reports/desktop', 'node', '--test', *tests], cwd=base / 'desktop', check=True)
        after = frontend_sources(base)
        runtime_after = frontend_runtime(base)
        if before != after or compiled != runtime_after:
            raise EvidenceError('frontend-source-or-runtime-changed-during-measurement')
        summary = read_json(base, 'reports/desktop/coverage-summary.json')
        rows = frontend_rows(base, summary)
        report.update(sourceStable=True, sourceSHA256=before, runtimeSHA256=compiled,
                      coverageSummarySHA256=sha256(reports / 'desktop/coverage-summary.json'), files=rows,
                      status='PASS' if all(row['status'] == 'PASS' for row in rows) and report.get('wails', {'status': 'PASS'})['status'] == 'PASS' else 'FAIL')
        if 'wails' in report and (report['wails'].get('sourceSHA256') != wails_frontend_sources(base) or
                                  report['wails'].get('runtimeSHA256') != wails_frontend_runtime(base)):
            report.update(status='FAIL', sourceStable=False, reason='wails-frontend-source-or-runtime-changed-during-legacy-measurement')
    except EvidenceError as error:
        report['reason'] = str(error)
    except (OSError, ValueError, TypeError, subprocess.SubprocessError):
        report['reason'] = 'frontend-measurement-failed'
    (reports / 'frontend-coverage.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(measure())
