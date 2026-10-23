#!/usr/bin/env python3
"""Actual Go-only build with Node/Electron/package-manager executables forbidden."""
import json,os,pathlib,shlex,subprocess,tempfile,time,tarfile
root=pathlib.Path(__file__).resolve().parents[1];reports=root/'reports';reports.mkdir(exist_ok=True)
def packages():
 return {str(p.relative_to(root)):[p.stat().st_size,p.stat().st_mtime_ns] for p in (root/'dist').glob('*.deb')}
start=time.monotonic();before=packages()
with tempfile.TemporaryDirectory(prefix='wallet-go-only-proof-') as tmp:
 folder=pathlib.Path(tmp);marker=folder/'unexpected-tool'
 for name in ['node','npm','electron','dpkg-deb']:
  p=folder/name;p.write_text('#!/bin/sh\nprintf "%s\\n" '+shlex.quote(name)+' >> '+shlex.quote(str(marker))+'\nexit 97\n');p.chmod(0o755)
 env={**os.environ,'PATH':str(folder)+':'+os.environ['PATH']}
 result=subprocess.run(['make','package-go'],cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
 (reports/'go-only-build.log').write_text(result.stdout)
 unexpected=marker.read_text().splitlines() if marker.exists() else []
 runtime_versions={}
 if result.returncode==0:
  manifest=json.loads((root/'dist/go-build-manifest.json').read_text())
  extracted=folder/'downloaded';extracted.mkdir()
  with tarfile.open(root/'dist'/manifest['archive']['file']) as tar:tar.extractall(extracted,filter='data')
  bundle=next(extracted.iterdir())
  for name in ['wallet-core','wallet-signer','wallet-launcher']:
   runtime_versions[name]=subprocess.check_output([str(bundle/name),'--version'],cwd=extracted,env={'PATH':'/usr/bin:/bin','HOME':str(folder)},text=True,timeout=10).strip()
 report={'runtimeVersionsWithoutCacheOrToolchain':runtime_versions,'status':'PASS' if result.returncode==0 and not unexpected and before==packages() else 'FAIL','command':['make','package-go'],'exit':result.returncode,'forbiddenToolsInvoked':unexpected,'debianArtifactsUnchanged':before==packages(),'seconds':round(time.monotonic()-start,3),'scope':'Actual compiled Go executables/archive; frontend and blockchain tests are separate'}
 (reports/'go-only-build.json').write_text(json.dumps(report,indent=2)+'\n');print(result.stdout);print(json.dumps(report,indent=2))
 raise SystemExit(report['status']!='PASS')
