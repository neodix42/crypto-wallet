#!/usr/bin/env python3
"""Synthetic pagination/funds-policy mutants in private copies; no chain access."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parents[3]
mutants = [
    ('CORE_SKIP_EQUAL_TIMESTAMP', 'pagination.go', 'out.Next = last', 'out.Next = last + 1',
     'TestTP050CorePagedHistoryKeepsTimestampTiesAndWindow'),
    ('CORE_CHANGED_PROVIDER_PAGE', 'history.go', 'c.Offset > len(page.Events) || c.Offset > 0 && c.Hash != page.Hash',
     'c.Offset > len(page.Events)', 'TestTP041CoreHistoryCursorBindingAndProviderChanges'),
    ('CORE_IGNORE_EXTRA_FEE_EFFECT', 'adapter.go',
     'r.Detail = "unrecognized same-hash ledger effect; reservation remains held"\n\t\t\t\treturn r, nil',
     'continue', 'TestTP060CoreReconcileTraversesBothLedgersAndRejectsExtraEffects'),
]
results = []
for name, file, before, after, test in mutants:
    with tempfile.TemporaryDirectory(prefix='localwallet-core-pagination-mutant-') as temporary:
        target = Path(temporary)
        for module in ('go.mod', 'go.sum'):
            shutil.copy2(root / module, target / module)
        for directory in ('internal/model', 'internal/chains/evm', 'internal/chains/hypercore'):
            shutil.copytree(root / directory, target / directory)
        fixtures = target / 'tests/oracles/hypercore'
        fixtures.mkdir(parents=True)
        shutil.copy2(root / 'tests/oracles/hypercore/fixtures.json', fixtures / 'fixtures.json')
        source = target / 'internal/chains/hypercore' / file
        original = source.read_text()
        if original.count(before) != 1:
            raise RuntimeError('Mutation source changed: ' + name)
        source.write_text(original.replace(before, after, 1))
        run = subprocess.run(['go', 'test', './internal/chains/hypercore', '-run', '^' + test + '$', '-count=1'],
                             cwd=target, capture_output=True, text=True, timeout=90)
        killed = run.returncode != 0 and '--- FAIL:' in run.stdout and '[build failed]' not in run.stdout + run.stderr
        results.append(dict(mutant=name, test=test, status='PASS' if killed else 'FAIL',
                            result='KILLED' if killed else 'SURVIVED_OR_INVALID',
                            sourceSHA256=hashlib.sha256(original.encode()).hexdigest()))
        print(name + ': ' + results[-1]['result'], flush=True)
Path(__file__).with_name('mutation-pagination-results.json').write_text(json.dumps(results, indent=2) + '\n')
raise SystemExit(any(result['status'] != 'PASS' for result in results))
