"""Compiled semantic regressions for default stablecoin identity and promotions.

Copied sources and public fixtures only. An unmodified passing control is required;
only the named assertion failing in a compiling mutant is a kill, never a timeout.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT=Path(__file__).resolve().parents[3]
HOSTILE='TestDefaultStablecoinHostileStateIsUnknownAndCannotPrepare'
PROMOTION='TestPromotedDefaultPoliciesArePreservedInertAndDeduplicated'
MUTANTS=[
 ('STABLE_PROXY_IMPLEMENTATION_ADDRESS','stablecoins.go','if implementation != "0x"+strings.Repeat("0", 24)+policy.implementation[2:] {','if false && implementation != "0x"+strings.Repeat("0", 24)+policy.implementation[2:] {',HOSTILE),
 ('STABLE_PROXY_IMPLEMENTATION_CODE','stablecoins.go','if err != nil || crypto.Keccak256Hash(decoded).Hex() != policy.implementationHash {','if false && (err != nil || crypto.Keccak256Hash(decoded).Hex() != policy.implementationHash) {',HOSTILE),
 ('BASE_USDT_REMOTE_BRIDGE_IDENTITY','stablecoins.go','if raw != "0x"+strings.Repeat("0", 24)+strings.ToLower(address[2:]) {','if false && raw != "0x"+strings.Repeat("0", 24)+strings.ToLower(address[2:]) {',HOSTILE),
 ('STABLE_ISSUER_SYMBOL_IDENTITY','stablecoins.go','if symbol != "0x"+hex.EncodeToString(encoded) {','if false && symbol != "0x"+hex.EncodeToString(encoded) {',HOSTILE),
 ('USDC_PAUSED_ISSUER_ALLOWED','stablecoins.go','if err != nil || paused.Sign() != 0 {','if false && (err != nil || paused.Sign() != 0) {',HOSTILE),
 ('PROMOTED_IMPORTED_METADATA_OVERRIDES_DEFAULT','tokens.go','if !IsPromotedDefault(token) {','if true {',PROMOTION),
 ('PROMOTED_DEFAULT_IMPORT_REAPPROVED','tokens.go','if IsPromotedDefault(token) {','if false && IsPromotedDefault(token) {',PROMOTION),
]
FOLDERS=['internal/chains/evm','internal/chains/chainutil','internal/model','internal/strictjson','internal/amount']
def source_hashes():
 hashes={}
 for folder in FOLDERS:
  for path in sorted((ROOT/folder).rglob('*')):
   if path.is_file() and path.suffix in ('.go','.json'):hashes[str(path.relative_to(ROOT))]=hashlib.sha256(path.read_bytes()).hexdigest()
 for name in ['go.mod','go.sum','tests/oracles/evm/stablecoin-mutation.py']:
  hashes[name]=hashlib.sha256((ROOT/name).read_bytes()).hexdigest()
 return hashes

def copy(target):
 for name in ['go.mod','go.sum']:shutil.copyfile(ROOT/name,target/name)
 for folder in FOLDERS:shutil.copytree(ROOT/folder,target/folder)
 (target/'third_party/wails').mkdir(parents=True);shutil.copyfile(ROOT/'third_party/wails/go.mod',target/'third_party/wails/go.mod')
 fixture=target/'tests/oracles/evm';fixture.mkdir(parents=True)
 for path in (ROOT/'tests/oracles/evm').glob('*.json'):shutil.copyfile(path,fixture/path.name)

def run(target,test):
 env=dict(os.environ)
 for name in ['WALLET_STABLECOIN_EVM','WALLET_STABLECOIN_LIVE','CRYPTOWALLET_PUBLIC_PROVIDERS']:env.pop(name,None)
 try:
  process=subprocess.run(['go','test','./internal/chains/evm','-run','^('+test+')$','-count=1'],cwd=target,capture_output=True,text=True,timeout=120,env=env)
  output=process.stdout+process.stderr
  return process.returncode,output
 except subprocess.TimeoutExpired:return None,'TIMEOUT_INVALID'

before=source_hashes();results=[]
with tempfile.TemporaryDirectory(prefix='cryptowallet-stablecoin-mutants-')as temporary:
 target=Path(temporary);copy(target)
 code,output=run(target,HOSTILE+'|'+PROMOTION)
 control={'status':'PASS'if code==0 else'FAIL','compiled':code is not None and '[build failed]'not in output,'exitCode':code}
 print('UNMODIFIED_CONTROL',control['status'],flush=True)
 if code==0:
  for name,filename,original,replacement,test in MUTANTS:
   path=target/'internal/chains/evm'/filename;source=path.read_text()
   if source.count(original)!=1:raise RuntimeError('Mutant source does not match exactly once: '+name)
   path.write_text(source.replace(original,replacement,1))
   try:code,output=run(target,test)
   finally:path.write_text(source)
   compiled=code is not None and '[build failed]'not in output
   killed=compiled and code!=0 and ('--- FAIL: '+test)in output
   result={'mutant':name,'test':test,'status':'PASS'if killed else'FAIL','compiled':compiled,'result':'KILLED'if killed else'SURVIVED_OR_INVALID'};results.append(result);print(name,result['result'],flush=True)
   if not killed:print(output[-3000:],flush=True)
stable=before==source_hashes()
report={'status':'PASS'if stable and control['status']=='PASS'and control['compiled']and len(results)==len(MUTANTS)and all(row['status']=='PASS'for row in results)else'FAIL','sourceStable':stable,'scope':'Seven compiling default stablecoin proxy/issuer/bridge/promotion mutants with a passing unmodified control; public fixtures only, no RPC or funds','sourceSHA256':before,'control':control,'mutants':results}
(ROOT/'reports').mkdir(exist_ok=True);(ROOT/'reports/stablecoin-mutation.json').write_text(json.dumps(report,indent=2)+'\n')
if report['status']!='PASS':raise SystemExit(1)
