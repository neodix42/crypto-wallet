#!/usr/bin/env python3
"""A missing evidence row is never implicitly passed."""
import json,pathlib,sys,hashlib,subprocess
root=pathlib.Path(__file__).resolve().parents[1];trace=json.loads((root/'docs/traceability.json').read_text());failed=[]
for row in trace['requirements']:
    if row['status']!='PASS':failed.append(row['id']+': '+row['status'])
coverage=root/'reports/coverage.json'
if not coverage.exists():failed.append('coverage evidence missing')
else:
    for row in json.loads(coverage.read_text())['packages']:
        if row['status']!='PASS':failed.append('coverage '+row['package'])
for required in ['go-coverage','frontend-coverage','artifact-security','integration','fuzz','mutation','stress','soak','desktop-e2e','public-testnets','independent-review','packaged-recovery']:
    p=root/'reports'/f'{required}.json'
    if not p.exists() or json.loads(p.read_text()).get('status')!='PASS':failed.append(required+' missing or incomplete')
snapshot=root/'reports/go-coverage.json'
if snapshot.exists():
    for name,expected in json.loads(snapshot.read_text()).get('sourceSHA256',{}).items():
        path=root/name
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest()!=expected:failed.append('stale coverage source: '+name)
if (root/'dist/build-manifest.json').exists():
    version=subprocess.run([str(root/'scripts/with-toolchain.sh'),'python3','scripts/verify-version.py'],cwd=root,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
    if version.returncode:failed.append('artifact/version integrity: '+version.stdout.strip())
else:failed.append('package build manifest missing')
print('FAIL: production release blocked' if failed else 'PASS: required evidence present')
for line in failed:print(' - '+line)
sys.exit(bool(failed))
