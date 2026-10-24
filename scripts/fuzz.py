#!/usr/bin/env python3
"""Bounded local fuzz campaigns; never a sustained/nightly coverage claim."""
import json,os,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[1];reports=root/'reports';reports.mkdir(exist_ok=True)
targets=[('uiprompt','FuzzPromptProtocol'),('vault','FuzzTP012Envelope'),('derivation','FuzzTP012Mnemonic'),('ipc','FuzzTP012IPC'),('strictjson','FuzzTP012JSON'),('amount','FuzzTP012Amount'),('qr','FuzzTP012QR'),('chains/chainutil','FuzzJSON'),('chains/evm','FuzzTP012AddressAndPlan'),('chains/hypercore','FuzzTP012CoreAction'),('chains/near','FuzzAddress'),('chains/aptos','FuzzAddress'),('chains/decred','FuzzAddressesScriptsOutpoints'),('chains/ton','FuzzAddressAndBOC')]
rows=[]
for package,target in targets:
 start=time.monotonic();command=['go','test','./internal/'+package,'-run','^$','-fuzz','^'+target+'$','-fuzztime=3s','-parallel=2']
 result=subprocess.run(command,cwd=root,env={**os.environ,'GOMAXPROCS':'2'},text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
 log=reports/('fuzz-'+package.replace('/','-')+'.log');log.write_text(result.stdout)
 rows.append({'package':package,'target':target,'status':'PASS' if result.returncode==0 else 'FAIL','exit':result.returncode,'seconds':round(time.monotonic()-start,3),'output':str(log.relative_to(root))})
 print(rows[-1],flush=True)
report={'status':'PASS' if all(r['status']=='PASS' for r in rows) else 'FAIL','scope':'3s per target bounded campaigns plus retained source corpus; not long scheduled fuzzing','targets':rows}
(reports/'fuzz.json').write_text(json.dumps(report,indent=2)+'\n');raise SystemExit(report['status']!='PASS')
