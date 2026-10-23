#!/usr/bin/env python3
"""A missing evidence row is never implicitly passed."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[1];trace=json.loads((root/'docs/traceability.json').read_text());failed=[]
for row in trace['requirements']:
    if row['status']!='PASS':failed.append(row['id']+': '+row['status'])
coverage=root/'reports/coverage.json'
if not coverage.exists():failed.append('coverage evidence missing')
else:
    for row in json.loads(coverage.read_text())['packages']:
        if row['status']!='PASS':failed.append('coverage '+row['package'])
for required in ['mutation','stress','soak','desktop-e2e','public-testnets','independent-review']:
    p=root/'reports'/f'{required}.json'
    if not p.exists() or json.loads(p.read_text()).get('status')!='PASS':failed.append(required+' missing or incomplete')
print('FAIL: production release blocked' if failed else 'PASS: required evidence present')
for line in failed:print(' - '+line)
sys.exit(bool(failed))
