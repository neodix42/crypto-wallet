#!/usr/bin/env python3
"""Run real local protocol gates, refuse missing prerequisites and retain honest
statuses. No public endpoint or mainnet signing is used by this command."""
import json,os,pathlib,signal,subprocess,time,urllib.request
root=pathlib.Path(__file__).resolve().parents[1];reports=root/'reports';reports.mkdir(exist_ok=True);rows=[]
def run(name,args,env=None):
    start=time.monotonic();p=subprocess.run(args,cwd=root,env=env or os.environ.copy());rows.append({'gate':name,'status':'PASS' if p.returncode==0 else 'FAIL','exit':p.returncode,'seconds':round(time.monotonic()-start,3)});return p.returncode==0
def node(name,args,url,test,extra=None):
    log_path=reports/(name+'-node.log')
    log=log_path.open('wb')
    process=subprocess.Popen(args,cwd=root,start_new_session=True,stdout=log,stderr=subprocess.STDOUT,env={**os.environ,**(extra or {})})
    try:
        ready=False;last_head=None;last_error=None
        for _ in range(120):
            if process.poll() is not None:break
            try:
                request=urllib.request.Request(url,data=b'{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",false]}',headers={'content-type':'application/json'})
                with urllib.request.urlopen(request,timeout=2) as response:
                    head=json.load(response).get("result") or {};last_head={'number':head.get('number'),'timestamp':head.get('timestamp')};ready=int(head.get("number","0x0"),16)>0 and abs(time.time()-int(head.get("timestamp","0x0"),16))<120
                if ready:break
            except (OSError,ValueError,TypeError,AttributeError) as error:last_error=type(error).__name__
            # A healthy genesis response is not ready yet. Pace successful
            # polls too, allowing the Engine driver to publish its first block.
            time.sleep(.25)
        if not ready:rows.append({'gate':name,'status':'BLOCKED','reason':'pinned local node failed readiness','nodeExit':process.poll(),'lastHead':last_head,'lastError':last_error,'log':str(log_path.relative_to(root))});return
        run(name,test,{**os.environ,**(extra or {})})
    finally:
        if process.poll() is None:os.killpg(process.pid,signal.SIGINT)
        try:process.wait(timeout=15)
        except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
        log.close()
run('near-sandbox',['node','tests/oracles/near/run-local.mjs'])
run('aptos-local-validator',['node','tests/oracles/aptos/run-local.mjs'])
binary=root/'.tools/dcrd/dcrd'
if not binary.exists():
    existing=pathlib.Path('/tmp/localwallet-dcr-bin/dcrd')
    if existing.exists():binary=existing
if binary.exists():run('decred-simnet',['go','test','./internal/chains/decred','-run','TestDcrdSimnet','-v','-count=1'],{**os.environ,'WALLET_DCRD':str(binary),'WALLET_EVIDENCE_DIR':str(reports)})
else:rows.append({'gate':'decred-simnet','status':'BLOCKED','reason':'Install pinned dcrd v1.10.9 under .tools/dcrd/dcrd'})
run('ton-v5r1-emulator',['go','test','./internal/chains/ton','-run','TestWalletV5R1Emulator','-v','-count=1'],{**os.environ,'WALLET_TON_EMULATOR':'1','WALLET_EVIDENCE_DIR':str(reports)})
node('geth-native',['python3','tests/oracles/evm/geth-node.py'],'http://127.0.0.1:18547',['go','test','./internal/chains/evm','-run','TestTP050LocalEVMProtocolAcceptance','-v','-count=1'],{'LOCALWALLET_EVM_NODE':'http://127.0.0.1:18547'})
node('evm-canonical-ZEN-runtime',['node','tests/oracles/evm/local-node.mjs'],'http://127.0.0.1:18546',['go','test','./internal/chains/evm','-run','TestTP050LocalEVMProtocolAcceptance','-v','-count=1'],{'LOCAL_CHAIN_ID':'84532','LOCAL_NODE_PORT':'18546','LOCALWALLET_EVM_NETWORK':'base-sepolia','LOCALWALLET_EVM_NODE':'http://127.0.0.1:18546'})
report={'status':'PASS' if all(r['status']=='PASS' for r in rows) else 'FAIL','scope':'Real local protocol acceptance, not public testnets; Core SDK fixtures have no official local validator gate','gates':rows}
(reports/'integration.json').write_text(json.dumps(report,indent=2)+'\n');raise SystemExit(report['status']!='PASS')
