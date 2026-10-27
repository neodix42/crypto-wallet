#!/usr/bin/env python3
"""Retain compact executed evidence in source; missing reports stay explicit."""
import argparse,hashlib,json,pathlib,shutil,subprocess
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--go-only',action='store_true');args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1];out=root/'docs/evidence';out.mkdir(exist_ok=True)
names=['coverage','go-coverage','frontend-coverage','mutation','fuzz','integration','stress','stress-race','stress-race-reference-budget','soak','native-input','native-input-long','native-duration','packaged-recovery','display-startup','mainnet-rpc','mainnet-tabs','wallet-layout','wallet-actions','phrase-recovery','phrase-onboarding','phrase-native-focused','electron-interface','prompt-ui','release-banner-removal','receive-small-display','failure-feedback','password-backup-regression','launcher-thread-proof','supervisor-lifecycle','desktop-e2e','public-testnets','independent-review','build-tools','go-only-build','nondefault-go-version','artifact-security']
names += ['hypercore-pagination','license-notices','release-auth','release-evidence','crash-security','packaged-send','fuzz-long','nondecred-soak-run','production-hardening']
names += ['bybit-client','bybit-client-mutations','exchange-core','exchange-signermutation',
          'usdt-execution','exchange','exchange-live','exchange-settings','settings-main','x11-diagnostics','near-dcr-balances','wallet-status','wallet-status-x11-driver','pre-exchange-soak','pre-exchange-nondecred-soak-run']
names += sorted(path.stem for path in (root/'reports').glob('wallet-status-packaged-attempt[0-9]*.json') if path.is_file())
names += sorted(path.stem for path in (root/'reports').glob('wallet-status-late-probe*.json') if path.is_file())
names += ['window-compact','window-compact-x11-driver']
names += ['unlock-scroll','unlock-layout','unlock-layout-before-fix']
names += ['phrase-backup','phrase-backup-packaged','phrase-backup-mutation','ci-desktop-tools']
names += sorted(path.stem for path in (root/'reports').glob('phrase-backup-*-attempt[0-9]*.json') if path.is_file())
names += sorted(path.stem for path in (root/'reports').glob('window-compact-packaged-attempt[0-9]*.json') if path.is_file())
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
for name in ['exchange-settings-coverage.log','exchange-settings-install.log',
             'exchange-settings-packaged.log','exchange-settings-send.log','exchange-settings-surface.log',
             'exchange-settings-recovery.log','exchange-settings-crash.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for name in ['settings-main-coverage.log','settings-main-install.log','settings-main-packaged.log',
             'settings-main-send.log','settings-main-surface.log','settings-main-recovery.log',
             'settings-main-crash.log','settings-main-go-coverage.log','settings-main-fuzz.log',
             'settings-main-mutation.log','settings-main-version.log','settings-main-production-gate.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for name in ['x11-diagnostics-install.log','x11-diagnostics-packaged.log','x11-diagnostics-send.log',
             'x11-diagnostics-go-coverage.log','x11-diagnostics-race.log','x11-diagnostics-recovery.log',
             'x11-diagnostics-crash.log','x11-diagnostics-display.log','x11-diagnostics-version.log',
             'x11-diagnostics-production-gate.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for name in ['near-balance-unit.log','near-balance-local.log','near-balance-live.log',
             'near-balance-live-not-run.log','near-dcr-frontend-coverage.log',
             'near-dcr-frontend-first-attempt.log','near-dcr-go-coverage.log',
             'near-dcr-install.log','near-dcr-packaged.log','near-dcr-packaged-send.log',
             'near-dcr-recovery.log','near-dcr-crash.log','near-dcr-race.log',
             'near-dcr-version.log','near-dcr-production-gate.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for name in ['wallet-status-components-first.log','wallet-status-components.log',
             'wallet-status-coverage-before-notice.log','wallet-status-coverage-before-attribution.log',
             'wallet-status-coverage-before-pending-fix.log','wallet-status-coverage-first-pending-fix.log',
             'wallet-status-frontend-coverage.log',
             'wallet-status-amount-approval.log','wallet-status-install.log',
             'wallet-status-packaged.log','wallet-status-packaged-send.log',
             'wallet-status-version.log','wallet-status-recovery.log','wallet-status-crash.log',
             'wallet-status-production-gate.log']:
 source=root/'reports'/name
 if source.is_file():shutil.copyfile(source,out/name)
for source in sorted((root/'reports').glob('wallet-status-packaged-attempt[0-9]*.log')):
 if source.is_file():shutil.copyfile(source,out/source.name)
for pattern in ['wallet-status-late-probe*.log','wallet-status-late-probe*-producer.py']:
 for source in sorted((root/'reports').glob(pattern)):
  if source.is_file():shutil.copyfile(source,out/source.name)
for source in sorted((root/'reports').glob('window-compact-*.log')):
 if source.is_file():shutil.copyfile(source,out/source.name)
for pattern in ['phrase-backup-*.log','phrase-backup-*-producer.py','phrase-backup-producer.py']:
 for source in sorted((root/'reports').glob(pattern)):
  if source.is_file():shutil.copyfile(source,out/source.name)
for pattern in ['unlock-scroll-*.log','unlock-layout*.log','unlock-layout-before-fix-producer.py']:
 for source in sorted((root/'reports').glob(pattern)):
  if source.is_file():shutil.copyfile(source,out/source.name)
for pattern in ['window-compact-packaged-attempt*-producer.py','window-compact-x11-driver-probe.py','window-compact-x11-closed-query.json','window-compact-x11-unexpected-error.json']:
 for source in sorted((root/'reports').glob(pattern)):
  if source.is_file():shutil.copyfile(source,out/source.name)
manifest=json.loads((root/'dist'/('go-build-manifest.json' if args.go_only else 'build-manifest.json')).read_text())
artifact=manifest['archive'] if args.go_only else manifest['portable']
candidate={'sourceCommit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'version':manifest['version'],'artifact':artifact['file'],'sha256':artifact['sha256'],'binaryManifest':manifest['binaryManifest'],'go':manifest['go'],'status':'DEVELOPMENT_UNSIGNED','productionRelease':'NOT_RUN: mandatory external gates remain incomplete','reports':index}
if not args.go_only:candidate.update(node=manifest['node'],electron=manifest['electron'])
(out/'candidate.json').write_text(json.dumps(candidate,indent=2)+'\n')
print('Retained compact measured evidence at docs/evidence; generated full logs/HTML remain in reports/')
