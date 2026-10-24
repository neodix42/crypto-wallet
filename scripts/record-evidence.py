#!/usr/bin/env python3
"""Retain compact executed evidence in source; missing reports stay explicit."""
import argparse,hashlib,json,pathlib,shutil,subprocess
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--go-only',action='store_true');args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1];out=root/'docs/evidence';out.mkdir(exist_ok=True)
names=['coverage','go-coverage','frontend-coverage','mutation','fuzz','integration','stress','stress-race','stress-race-reference-budget','soak','native-input','native-input-long','native-duration','packaged-recovery','display-startup','mainnet-rpc','mainnet-tabs','wallet-layout','wallet-actions','phrase-recovery','phrase-onboarding','phrase-native-focused','electron-interface','prompt-ui','release-banner-removal','receive-small-display','failure-feedback','password-backup-regression','launcher-thread-proof','supervisor-lifecycle','desktop-e2e','public-testnets','independent-review','build-tools','go-only-build','nondefault-go-version','artifact-security']
names += ['hypercore-pagination','license-notices','release-auth','release-evidence','crash-security','packaged-send','fuzz-long','nondecred-soak-run','production-hardening']
names += ['bybit-client','bybit-client-mutations','exchange-core','exchange-signermutation',
          'usdt-execution','exchange','exchange-live','pre-exchange-soak','pre-exchange-nondecred-soak-run']
index=[]
for name in names:
 source=root/'reports'/(name+'.json')
 if not source.is_file():index.append({'report':name,'status':'NOT_RUN','reason':'report absent'});continue
 data=json.loads(source.read_text());shutil.copyfile(source,out/source.name)
 status=data.get('status','PASS' if name=='coverage' and all(r['status']=='PASS' for r in data.get('packages',[])) else 'NOT_RUN')
 index.append({'report':name,'status':status,'file':source.name,'sha256':hashlib.sha256(source.read_bytes()).hexdigest()})
for name in ['govulncheck.txt','npm-audit.json','race.log','phrase-dependencies.log','phrase-race.log','phrase-recovery.log','phrase-packaged.log','electron-dependencies.log','electron-race.log','electron-recovery.log','electron-packaged.log','electron-build-tools.log','electron-coverage-integrity.log','electron-version.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for name in ['history-frontend-coverage.log','hardening-go-coverage.log','hardening-race.log','hardening-ton-race.log','hardening-mutation.log','hardening-dependencies.log','hardening-recovery.log','hardening-crash.log','hardening-packaged.log','packaged-send.log','license-notices-tests.log','license-notices-go.log','license-notices-desktop.log','license-notices-repeat.log','hardening-release-auth.log','hardening-release-evidence.log','hardening-build-tools.log','hardening-campaign.log','hardening-production-gate.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for name in ['exchange-go-coverage.log','exchange-race.log','exchange-frontend-coverage.log',
             'exchange-dependencies.log','exchange-recovery.log','exchange-crash.log',
             'exchange-packaged.log','exchange-packaged-send.log','exchange-mutation.log',
             'exchange-build-tools.log','exchange-coverage-integrity.log','exchange-campaign-tests.log',
             'exchange-release-auth.log','exchange-release-evidence.log','exchange-notice-tests.log',
             'exchange-fuzz.log','exchange-signermutation.log','exchange-production-gate.log',
             'usdt-adapter-race.log','exchange-ton-route-regression.log',
             'exchange-target.log','exchange-surface.log','exchange-install.log',
             'exchange-artifact.log','exchange-version.log','exchange-go-version.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
manifest=json.loads((root/'dist'/('go-build-manifest.json' if args.go_only else 'build-manifest.json')).read_text())
artifact=manifest['archive'] if args.go_only else manifest['portable']
candidate={'sourceCommit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'version':manifest['version'],'artifact':artifact['file'],'sha256':artifact['sha256'],'binaryManifest':manifest['binaryManifest'],'go':manifest['go'],'status':'DEVELOPMENT_UNSIGNED','productionRelease':'NOT_RUN: mandatory external gates remain incomplete','reports':index}
if not args.go_only:candidate.update(node=manifest['node'],electron=manifest['electron'])
(out/'candidate.json').write_text(json.dumps(candidate,indent=2)+'\n')
print('Retained compact measured evidence at docs/evidence; generated full logs/HTML remain in reports/')
