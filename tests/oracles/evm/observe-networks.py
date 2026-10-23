"""Read-only network/contract provenance verifier; never signs or sends funds."""
import json,time,urllib.request
from pathlib import Path
from eth_utils import keccak

networks=[('base-mainnet','https://mainnet.base.org','0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229'),('base-sepolia','https://sepolia.base.org','0x107fdE93838e3404934877935993782F977324BB'),('horizen-mainnet','https://horizen.calderachain.xyz/http','0x57da2D504bf8b83Ef304759d9f2648522D7a9280'),('horizen-testnet','https://horizen-testnet.rpc.caldera.xyz/http','0xb06EC4ce262D8dbDc24Fac87479A49A7DC4cFb87')]
observations=[];deployed=[]
for name,url,contract in networks:
    def rpc(method,params):
        request=urllib.request.Request(url,data=json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params}).encode(),headers={'Content-Type':'application/json','User-Agent':'localwallet-development-verifier/1'})
        result=json.load(urllib.request.urlopen(request,timeout=15))
        if 'error' in result:raise RuntimeError('RPC code '+str(result['error']['code']))
        return result['result']
    try:
        block=rpc('eth_blockNumber',[]);code=rpc('eth_getCode',[contract,block])
        decimals=rpc('eth_call',[{'to':contract,'data':'0x313ce567'},block])
        implementation=rpc('eth_getStorageAt',[contract,'0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc',block])
        observation={'network':name,'rpc':url,'contract':contract,'chainId':int(rpc('eth_chainId',[]),16),'block':block,'decimals':int(decimals,16),'codeHash':'0x'+keccak(bytes.fromhex(code[2:])).hex(),'codeBytes':len(code[2:])//2,'eip1967Implementation':implementation,'status':'PASS'}
        observations.append(observation);deployed.append({**observation,'code':code})
    except Exception as error:
        observations.append({'network':name,'status':'BLOCKED','error':type(error).__name__})
envelope={'observedUTC':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'readOnly':True}
Path(__file__).with_name('network-observations.json').write_text(json.dumps({**envelope,'networks':observations},indent=2)+'\n')
Path(__file__).with_name('deployed-tokens.json').write_text(json.dumps({**envelope,'networks':deployed},indent=2)+'\n')
print(json.dumps([{'network':x['network'],'status':x['status']} for x in observations]))
if any(x['status']!='PASS' for x in observations):raise SystemExit(1)
