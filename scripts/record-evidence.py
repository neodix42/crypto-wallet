#!/usr/bin/env python3
"""Retain compact executed evidence in source; missing reports stay explicit."""
import hashlib,json,pathlib,shutil,subprocess
root=pathlib.Path(__file__).resolve().parents[1];out=root/'docs/evidence';out.mkdir(exist_ok=True)
names=['coverage','go-coverage','frontend-coverage','mutation','fuzz','integration','stress','stress-race','stress-race-reference-budget','soak','native-input','native-input-long','native-duration','packaged-recovery','desktop-e2e','public-testnets','independent-review','build-tools','nondefault-go-version','artifact-security']
index=[]
for name in names:
 source=root/'reports'/(name+'.json')
 if not source.is_file():index.append({'report':name,'status':'NOT_RUN','reason':'report absent'});continue
 data=json.loads(source.read_text());shutil.copyfile(source,out/source.name)
 status=data.get('status','PASS' if name=='coverage' and all(r['status']=='PASS' for r in data.get('packages',[])) else 'NOT_RUN')
 index.append({'report':name,'status':status,'file':source.name,'sha256':hashlib.sha256(source.read_bytes()).hexdigest()})
for name in ['govulncheck.txt','npm-audit.json','race.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
manifest=json.loads((root/'dist/build-manifest.json').read_text())
candidate={'sourceCommit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'version':manifest['version'],'artifact':manifest['package'],'sha256':manifest['sha256'],'binaryManifest':manifest['binaryManifest'],'go':manifest['go'],'node':manifest['node'],'electron':manifest['electron'],'status':'DEVELOPMENT_UNSIGNED','productionRelease':'NOT_RUN: mandatory external gates remain incomplete','reports':index}
(out/'candidate.json').write_text(json.dumps(candidate,indent=2)+'\n')
print('Retained compact measured evidence at docs/evidence; generated full logs/HTML remain in reports/')
