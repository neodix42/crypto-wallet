#!/usr/bin/env python3
"""Run security mutants using Go overlays; never modify the live source tree."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[3]
PACKAGE = ROOT / 'internal/exchange/bybit'
MUTANTS = [
    ('accept-margin', 'validation.go', 'o.IsLeverage != 0', 'false', 'TestCanonicalPostSchemasRejectAdditionalAuthority'),
    ('allow-offchain-withdrawal', 'validation.go', 'w.ForceChain != 1', 'false', 'TestCanonicalPostSchemasRejectAdditionalAuthority'),
    ('allow-different-wire-encoding', 'validation.go', 'string(b) != r.Payload', 'len(b) == 0', 'TestCanonicalPostSchemasRejectAdditionalAuthority'),
    ('allow-untrusted-authority', 'validation.go', 'endpoint == allowed', 'endpoint == allowed || endpoint != ""', 'TestAuthenticationAndEndpointBoundaries'),
    ('allow-post-replay-body', 'client.go', 'req.GetBody = nil', '// MUTANT: preserve the replayable request body', 'TestTypedAPIWireAndExactAmounts'),
]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    sources = {str(p.relative_to(ROOT)): digest(p) for p in sorted(PACKAGE.glob('*.go'))}
    rows = []
    with tempfile.TemporaryDirectory(prefix='wallet-bybit-mutants-') as scratch:
        scratch = Path(scratch)
        for name, filename, old, new, test in MUTANTS:
            original = PACKAGE / filename
            source = original.read_text()
            if source.count(old) != 1:
                raise RuntimeError('Mutation no longer identifies exactly one source block')
            replacement = scratch / filename
            replacement.write_text(source.replace(old, new))
            overlay = scratch / 'overlay.json'
            overlay.write_text(json.dumps({'Replace': {str(original): str(replacement)}}))
            result = subprocess.run(
                [str(ROOT / 'scripts/with-go.sh'), 'go', 'test', '-overlay', str(overlay), '-count=1', '-run', '^' + test + '$', './internal/exchange/bybit'],
                cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120,
            )
            output = result.stdout
            killed = result.returncode != 0 and '--- FAIL: ' + test in output and '[build failed]' not in output
            rows.append({'name': name, 'status': 'KILLED' if killed else 'SURVIVED_OR_INVALID', 'test': test,
                         'mutatedSourceSHA256': digest(replacement), 'outputSHA256': hashlib.sha256(output.encode()).hexdigest()})
            print(name + ': ' + rows[-1]['status'], flush=True)
    after = {str(p.relative_to(ROOT)): digest(p) for p in sorted(PACKAGE.glob('*.go'))}
    report = {'status': 'PASS' if sources == after and all(row['status'] == 'KILLED' for row in rows) else 'FAIL',
              'sourceStable': sources == after, 'sourceSHA256': sources, 'mutants': rows,
              'scope': 'Bybit client policy and transport overlays; no live credentials or exchange funds'}
    report_dir = ROOT / 'reports'
    report_dir.mkdir(exist_ok=True)
    (report_dir / 'bybit-client-mutations.json').write_text(json.dumps(report, indent=2) + '\n')
    if report['status'] != 'PASS':
        raise SystemExit(1)


if __name__ == '__main__':
    main()
