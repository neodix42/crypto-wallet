#!/usr/bin/env python3
"""Conservative pinned dependency/build inventory; no audit certification."""
import argparse,hashlib,json,os,pathlib,subprocess
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--go-only',action='store_true',help='inventory Go components without reading npm or invoking Node')
go_only=parser.parse_args().go_only
prefix='go-' if go_only else ''
root=pathlib.Path(__file__).resolve().parents[1];out=root/'dist';out.mkdir(exist_ok=True)
raw=subprocess.check_output(['go','list','-m','-json','all'],cwd=root,text=True);decoder=json.JSONDecoder();modules=[]
while raw.strip():
    value,end=decoder.raw_decode(raw.lstrip());modules.append(value);raw=raw.lstrip()[end:]
components=[];licenses=[]
for m in modules:
    if m.get('Main'):continue
    c={'type':'library','name':m['Path'],'version':m.get('Version','unknown'),'purl':'pkg:golang/'+m['Path']+'@'+m.get('Version','unknown'),'properties':[{'name':'review','value':'Automated reachability scan; independent license/cryptographic review incomplete'}]}
    if m.get('Sum','').startswith('h1:'):c['properties'].append({'name':'goModuleDirectorySum','value':m['Sum']})
    components.append(c);d=pathlib.Path(m.get('Dir','/nonexistent'))
    paths=[p for name in ['LICENSE','LICENSE.txt','LICENSE.md','COPYING','COPYING.LESSER'] if (p:=d/name).is_file()]
    entry={'module':m['Path'],'version':m.get('Version'),'licenseFiles':[str(p) for p in paths],'review':'NOT_RUN: legal/license obligation review'}
    if go_only:
        c['properties'][0]['value']='Pinned Go module graph inventory; independent license/cryptographic review incomplete'
        entry['licenseFiles']=[p.name for p in paths]
        entry['licenseSHA256']={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in paths if not p.is_symlink()}
    licenses.append(entry)
if not go_only:
    lock=json.loads((root/'desktop/package-lock.json').read_text())
    for location,item in lock['packages'].items():
        if not location:continue
        components.append({'type':'library','name':location.rsplit('node_modules/',1)[-1],'version':item.get('version','unknown'),'scope':'required' if location=='node_modules/electron' or not item.get('dev') else 'excluded','properties':[{'name':'buildDependency','value':str(bool(item.get('dev'))).lower()},{'name':'npmIntegrity','value':item.get('integrity','missing')}]})
sbom={'bomFormat':'CycloneDX','specVersion':'1.6','version':1,'metadata':{'component':{'type':'application','name':'LocalWallet Go Components Development' if go_only else 'LocalWallet Development','version':subprocess.check_output(['python3','scripts/version.py'],cwd=root,text=True).strip()}},'components':components}
(out/(prefix+'sbom.cdx.json')).write_text(json.dumps(sbom,indent=2)+'\n');(out/(prefix+'dependency-licenses.json')).write_text(json.dumps(licenses,indent=2)+'\n')
dirty=bool(subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip())
sources=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0');entries={}
for name in sorted(set(sources)):
    p=root/name
    if name and not name.startswith(('dist/','bin/','reports/','.idea/')):
        if p.is_symlink():entries[name]=hashlib.sha256(os.readlink(p).encode()).hexdigest()
        elif p.is_file():entries[name]=hashlib.sha256(p.read_bytes()).hexdigest()
(out/(prefix+'source-manifest.json')).write_text(json.dumps({'baseCommit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'workingTree':'dirty' if dirty else 'clean','symlinkHashMeaning':'SHA256 of literal link target; external file contents are never read','sha256':entries},indent=2)+'\n')
print('Wrote '+('Go-only ' if go_only else '')+'SBOM, dependency/license review inventory and source manifest under dist/')
