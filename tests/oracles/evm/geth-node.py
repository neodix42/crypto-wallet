"""Test-only actual geth node with synthetic funds and a local Engine API driver.
Image: ethereum/client-go:1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109.
No peer networking, loopback published RPC only. Ctrl-C removes its ephemeral node.
"""
import base64, hashlib, hmac, json, os, signal, subprocess, tempfile, time, urllib.request
from pathlib import Path

IMAGE='ethereum/client-go:v1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109'
NAME='localwallet-geth-interop'
genesis=json.loads(Path(__file__).with_name('geth-genesis.json').read_text())
for fork in ['osakaTime','bogotaTime']:
    genesis['config'].pop(fork,None)
# Public test authentication material, never a wallet or user secret.
jwt_key=bytes(range(32))
temp=tempfile.TemporaryDirectory(prefix='localwallet-geth-test-')
Path(temp.name,'genesis.json').write_text(json.dumps(genesis))
Path(temp.name,'jwt.hex').write_text(jwt_key.hex())
subprocess.run(['docker','run','--rm','-d','--name',NAME,'-p','127.0.0.1:18547:8545','-p','127.0.0.1:18548:8551','-v',temp.name+':/fixture:ro',IMAGE,'--syncmode','full','--override.genesis','/fixture/genesis.json','--authrpc.jwtsecret','/fixture/jwt.hex','--authrpc.addr','0.0.0.0','--authrpc.vhosts','localhost,127.0.0.1','--http','--http.addr','0.0.0.0','--http.api','eth,net,web3','--http.vhosts','localhost,127.0.0.1','--nodiscover','--maxpeers','0'],check=True,stdout=subprocess.DEVNULL)
def rpc(method,params,engine=False):
    headers={'Content-Type':'application/json'}
    if engine:
        def enc(v):return base64.urlsafe_b64encode(json.dumps(v,separators=(',',':')).encode()).rstrip(b'=')
        data=enc({'alg':'HS256','typ':'JWT'})+b'.'+enc({'iat':int(time.time())})
        token=data+b'.'+base64.urlsafe_b64encode(hmac.new(jwt_key,data,hashlib.sha256).digest()).rstrip(b'=')
        headers['Authorization']='Bearer '+token.decode()
    req=urllib.request.Request('http://127.0.0.1:'+('18548' if engine else '18547'),data=json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params}).encode(),headers=headers)
    result=json.load(urllib.request.urlopen(req,timeout=10))
    if 'error' in result:raise RuntimeError(method+': '+str(result['error'].get('code')))
    return result['result']
try:
    for _ in range(100):
        try:
            head=rpc('eth_getBlockByNumber',['latest',False]);break
        except (OSError,RuntimeError):time.sleep(.1)
    else:raise RuntimeError('geth did not initialize')
    zero='0x'+'00'*32
    print(json.dumps({'node':'geth 1.17.9 Engine API','chainId':11155111,'port':18547}),flush=True)
    while True:
        timestamp=max(int(head['timestamp'],16)+1,int(time.time()))
        fc={'headBlockHash':head['hash'],'safeBlockHash':head['hash'],'finalizedBlockHash':head['hash']}
        attrs={'timestamp':hex(timestamp),'prevRandao':zero,'suggestedFeeRecipient':'0x0000000000000000000000000000000000000000','withdrawals':[],'parentBeaconBlockRoot':zero}
        built=rpc('engine_forkchoiceUpdatedV3',[fc,attrs],True)
        if built['payloadStatus']['status']!='VALID':raise RuntimeError('forkchoice not valid')
        time.sleep(1)
        full=rpc('engine_getPayloadV4',[built['payloadId']],True)
        payload=full['executionPayload']
        result=rpc('engine_newPayloadV4',[payload,[],zero,full['executionRequests']],True)
        if result['status']!='VALID':raise RuntimeError('payload not valid: '+str(result))
        fc={'headBlockHash':payload['blockHash'],'safeBlockHash':payload['blockHash'],'finalizedBlockHash':payload['blockHash']}
        result=rpc('engine_forkchoiceUpdatedV3',[fc,None],True)
        if result['payloadStatus']['status']!='VALID':raise RuntimeError('canonical payload failed')
        head=rpc('eth_getBlockByNumber',['latest',False])
except KeyboardInterrupt:
    pass
finally:
    subprocess.run(['docker','stop','-t','2',NAME],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    temp.cleanup()
