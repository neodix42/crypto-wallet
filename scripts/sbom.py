#!/usr/bin/env python3
"""Conservative pinned dependency/build inventory; no audit certification."""
import base64,hashlib,json,pathlib,subprocess,urllib.parse
root=pathlib.Path(__file__).resolve().parents[1];out=root/'dist';out.mkdir(exist_ok=True)
raw=subprocess.check_output(['go','list','-m','-json','all'],cwd=root,text=True);decoder=json.JSONDecoder();modules=[]
while raw.strip():
    value,end=decoder.raw_decode(raw.lstrip());modules.append(value);raw=raw.lstrip()[end:]
components=[];licenses=[]
for m in modules:
    if m.get('Main'):continue
    c={'type':'library','name':m['Path'],'version':m.get('Version','unknown'),'purl':'pkg:golang/'+m['Path']+'@'+m.get('Version','unknown'),'properties':[{'name':'review','value':'Automated reachability scan; independent license/cryptographic review incomplete'}]}
    if m.get('Sum','').startswith('h1:'):c['hashes']=[{'alg':'SHA-256','content':base64.b64decode(m['Sum'][3:]).hex()}]
    components.append(c);d=pathlib.Path(m.get('Dir','/nonexistent'))
    paths=[p for name in ['LICENSE','LICENSE.txt','LICENSE.md','COPYING','COPYING.LESSER'] if (p:=d/name).is_file()]
    licenses.append({'module':m['Path'],'version':m.get('Version'),'licenseFiles':[str(p) for p in paths],'review':'NOT_RUN: legal/license obligation review'})
lock=json.loads((root/'desktop/package-lock.json').read_text())
for location,item in lock['packages'].items():
    if not location:continue
    components.append({'type':'library','name':location.rsplit('node_modules/',1)[-1],'version':item.get('version','unknown'),'scope':'excluded' if item.get('dev') else 'required','properties':[{'name':'buildDependency','value':str(bool(item.get('dev'))).lower()},{'name':'npmIntegrity','value':item.get('integrity','missing')}]})
sbom={'bomFormat':'CycloneDX','specVersion':'1.6','version':1,'metadata':{'component':{'type':'application','name':'LocalWallet Development','version':'0.1.0'}},'components':components}
(out/'sbom.cdx.json').write_text(json.dumps(sbom,indent=2)+'\n');(out/'dependency-licenses.json').write_text(json.dumps(licenses,indent=2)+'\n')
sources=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0');entries={}
for name in sorted(set(sources)):
    p=root/name
    if p.is_file() and name and not name.startswith(('dist/','bin/','reports/','.idea/')):entries[name]=hashlib.sha256(p.read_bytes()).hexdigest()
(out/'source-manifest.json').write_text(json.dumps({'baseCommit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'workingTree':'uncommitted owner specifications and implementation; no commit created','sha256':entries},indent=2)+'\n')
print('Wrote SBOM, dependency/license review inventory and source manifest under dist/')
