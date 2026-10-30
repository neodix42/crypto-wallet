#!/usr/bin/env python3
"""Enforce statement floors per shipped first-party package, including glue."""
import json,pathlib,sys,collections
totals=collections.defaultdict(lambda:[0,0])
for line in pathlib.Path(sys.argv[1]).read_text().splitlines()[1:]:
    location,n,count=line.rsplit(' ',2); source=location.rsplit(':',1)[0]
    if source.startswith('localwallet/tests/oracles/'):
        continue # independent reference programs are not shipped; explicitly reported
    package=str(pathlib.PurePosixPath(source).parent);totals[package][1]+=int(n)
    if int(count):totals[package][0]+=int(n)
rows=[]
for package,(hit,total) in sorted(totals.items()):
    critical=any('/'+x in package for x in ['marketdata','vault','derivation','signer','amount','chains','exchange','ipc','journal','qr','strictjson','isolation','secureui','uiprompt','supervisor','wallet-core','wallet-signer','walletcore','walletsigner','nativeui','nativewallet','webui','sessionwatch'])
    floor=95 if critical else 90;percent=100*hit/total if total else 100
    rows.append({'package':package,'statements':total,'covered':hit,'percent':round(percent,2),'floor':floor,'status':'PASS' if percent>=floor else 'FAIL'})
if not rows:
    raise SystemExit('FAIL: no shipped first-party statement blocks measured')
pathlib.Path('reports').mkdir(exist_ok=True);pathlib.Path('reports/coverage.json').write_text(json.dumps({'measurement':'Go statement coverage; not branch coverage','exclusions':['tests/oracles: independent non-shipped reference generators; third-party modules not instrumented'],'packages':rows},indent=2)+'\n')
for r in rows:print(f"{r['status']:4} {r['percent']:6.2f}% >= {r['floor']}% {r['package']}")
sys.exit(any(r['status']=='FAIL' for r in rows))
