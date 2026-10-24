#!/usr/bin/env python3
import json,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parents[1]
subprocess.run(['npm','--prefix','desktop','run','build'],cwd=root,check=True)
subprocess.run(['./node_modules/.bin/c8','--all','--include=app/**/*.js','--include=ui/*.js','--exclude=tests/**','--reporter=json-summary','--reporter=text','--reports-dir=../reports/desktop','node','--test',*[str(p.relative_to(root/'desktop')) for p in sorted((root/'desktop/tests').glob('*.test.cjs'))]],cwd=root/'desktop',check=True)
data=json.loads((root/'reports/desktop/coverage-summary.json').read_text());rows=[]
required={'app/main.js','app/preload.js','app/validation.js','app/codec.js','app/prompts.js','app/prompt-preload.js','ui/renderer.js','ui/prompt.js'}
actual={str(p.relative_to(root/'desktop')) for folder in ['app','ui'] for p in (root/'desktop'/folder).glob('*.js')}
for relative in sorted(required|actual):
    key=str(root/'desktop'/relative);v=data.get(key);critical=not relative.startswith('ui/') or relative=='ui/prompt.js';sf,bf=(95,90) if critical else (90,85)
    statements=v['statements']['pct'] if v else 0;branches=v['branches']['pct'] if v else 0
    rows.append({'file':relative,'statements':statements,'branches':branches,'floors':{'statements':sf,'branches':bf},'status':'PASS' if statements>=sf and branches>=bf else 'FAIL'})
report={'status':'PASS' if all(r['status']=='PASS' for r in rows) else 'FAIL','measurement':'Istanbul statement/branch coverage from c8/V8; every shipped JS file included, absent files fail','files':rows}
(root/'reports/frontend-coverage.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2));sys.exit(report['status']!='PASS')
