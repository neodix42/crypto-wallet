#!/usr/bin/env python3
"""Measure all production JS and retain the exact stable source/runtime inputs."""
import json
import pathlib
import subprocess

from evidence_binding import (EvidenceError, frontend_rows, frontend_runtime,
                              frontend_sources, read_json, sha256)

root = pathlib.Path(__file__).resolve().parents[1]


def measure(base=root):
    base = pathlib.Path(base)
    reports = base / 'reports'
    reports.mkdir(exist_ok=True)
    report = {'status': 'FAIL', 'sourceStable': False,
              'measurement': 'Istanbul statement/branch coverage from c8/V8; every shipped JS file included, absent files fail',
              'files': []}
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
                      status='PASS' if all(row['status'] == 'PASS' for row in rows) else 'FAIL')
    except EvidenceError as error:
        report['reason'] = str(error)
    except (OSError, ValueError, TypeError, subprocess.SubprocessError):
        report['reason'] = 'frontend-measurement-failed'
    (reports / 'frontend-coverage.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(measure())
