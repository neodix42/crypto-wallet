"""Read-only official metadata evidence; no wallet secrets or exchange requests."""
import json,time,urllib.request
from pathlib import Path

observations=[]
for name,url in [('Mainnet','https://api.hyperliquid.xyz'),('Testnet','https://api.hyperliquid-testnet.xyz')]:
    try:
        request=urllib.request.Request(url+'/info',data=b'{"type":"spotMeta"}',headers={'Content-Type':'application/json'})
        meta=json.load(urllib.request.urlopen(request,timeout=15))
        hype=[token for token in meta['tokens'] if token['name']=='HYPE']
        observations.append({'environment':name,'endpoint':url,'hype':hype,'status':'PASS' if len(hype)==1 and hype[0]['weiDecimals']==8 else 'FAIL'})
    except Exception as error:
        observations.append({'environment':name,'status':'BLOCKED','error':type(error).__name__})
Path(__file__).with_name('network-observations.json').write_text(json.dumps({'observedUTC':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'readOnly':True,'networks':observations},indent=2)+'\n')
print(json.dumps([{'environment':x['environment'],'status':x['status']} for x in observations]))
if any(x['status']!='PASS' for x in observations):raise SystemExit(1)
