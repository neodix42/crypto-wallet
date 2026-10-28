#!/usr/bin/env python3
"""Recover disposable Go-generated wallets through the typed signer protocol.

Three independent installations, homes and processes share only the recorded
phrase in test memory. This is a headless protocol integration; the separate
packaged drills operate actual graphical dialogs. No original vault, keyring,
user secret, secret screenshot, or mainnet payment is used.
"""
import argparse,base64,hashlib,json,os,pathlib,queue,secrets,shutil,signal,struct,subprocess,tempfile,threading
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--portable',action='store_true',help='Exercise the single cryptowallet executable through its internal core role')
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1];reports=root/'reports';reports.mkdir(exist_ok=True)
report={'status':'NOT_RUN','scope':__doc__,'mode':'portable-go-self-roles' if args.portable else 'legacy-go-components','producerSHA256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'steps':[],'notRun':['Graphical UI interaction','Separate physical machines','Full supported OS/Wayland matrix','Funded transactions']}
children=[];logs=[];canaries=[];public_frames=[]
def check(ok,message):
 if not ok:raise RuntimeError(message)
def passed(name):report['steps'].append({'name':name,'status':'PASS'});print('PASS '+name,flush=True)
class Client:
 def __init__(self,install,home,config,password):
  env={**os.environ,'HOME':str(home),'XDG_CONFIG_HOME':str(config)}
  for name in ['DISPLAY','WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:env.pop(name,None)
  self.log=tempfile.TemporaryFile();logs.append(self.log)
  command=[str(install/'cryptowallet'),'--internal-core','--environment=mainnet'] if args.portable else [str(install/'wallet-core'),'--environment=mainnet']
  self.process=subprocess.Popen(command,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=self.log,start_new_session=True)
  children.append(self.process);self.session=secrets.token_hex(16);self.counter=0;self.responses=queue.Queue();self.password=password;self.phrase=None;self.prompts=0
  threading.Thread(target=self.reader,daemon=True).start()
 def reader(self):
  try:
   while True:
    header=self.process.stdout.read(4);check(len(header)==4,'Application IPC ended unexpectedly')
    size=struct.unpack('>I',header)[0];check(0<size<=1<<20,'Invalid response size')
    data=self.process.stdout.read(size);check(len(data)==size,'Truncated response')
    message=json.loads(data)
    if message.get('method')!='prompt':public_frames.append(data)
    self.responses.put(message)
  except Exception:self.responses.put({'failure':True})
 def write(self,message):
  data=json.dumps(message).encode();self.process.stdin.write(struct.pack('>I',len(data))+data);self.process.stdin.flush()
 def call(self,method,body=None,inputs=None,wrong=False,expected_phrase=None):
  self.counter+=1;request={'version':1,'id':str(self.counter),'session':self.session,'method':method,'body':body or {}}
  self.write(request)
  while True:
   message=self.responses.get(timeout=60);check(not message.get('failure'),'Application prompt IPC failed')
   check(all(message.get(k)==request[k] for k in ['version','id','session']),'Mismatched response identity')
   if message['method']=='activity':check(message['body']=={},'Activity contains values');continue
   if message['method']!='prompt':
    check(message['method']==method and not message.get('error'),'Application rejected recovery operation')
    return message['body']
   self.prompts+=1;p=message['body'];check(isinstance(p,dict) and len(p.get('id',''))==32,'Invalid prompt identifier')
   kind=p.get('kind');reply={'promptId':p['id'],'accepted':True}
   if kind=='input':
    labels=p['labels'];title=p['title']
    if title.startswith('Confirm recovery phrase'):
     check(self.phrase is not None,'Challenge preceded phrase recording');words=self.phrase.split()
     reply['values']=['incorrect']*3 if wrong else [words[i].decode() for i in (1,5,len(words)-1)]
    elif title.startswith('Create local wallet'):
     check(inputs is not None,'Unexpected wallet input request');reply['values']=inputs
    elif title.startswith(('Complete recovery setup','Unlock local wallet')):reply['values']=[self.password]
    else:raise RuntimeError('Unexpected input prompt in recovery test')
    check(len(reply['values'])==len(labels),'Prompt input count mismatch')
   elif kind=='recovery':
    phrase=p.get('phrase','').encode('ascii');check(len(phrase.split())==12,'Generated phrase length differs')
    if expected_phrase is not None:check(phrase==expected_phrase,'Pending setup changed the wallet root')
    self.phrase=phrase
    if phrase not in canaries:canaries.append(phrase)
   elif kind not in ['confirm','notice']:raise RuntimeError('Unexpected prompt kind in recovery test')
   # This narrowly typed, one-use control is the only credential transport.
   # Ordinary requests and snapshots remain secret-free. No control ack exists.
   self.write({**request,'method':'promptReply','body':reply})
 def stop(self):
  if self.process.poll() is None:
   self.process.stdin.close()
   try:self.process.wait(timeout=10)
   except subprocess.TimeoutExpired:os.killpg(self.process.pid,signal.SIGKILL);self.process.wait()
def descriptors(snapshot):
 return sorted([{k:a[k] for k in ['id','family','profile','index','network','publicKey','address','parameters'] if k in a} for a in snapshot['accounts']],key=lambda a:a['id'])
try:
 if args.portable:
  metadata=json.loads((root/'bin/portable-build-manifest.json').read_text())
  check(metadata.get('schemaVersion')==1 and metadata.get('kind')=='portable-go-wallet' and metadata.get('file')=='cryptowallet' and metadata.get('sandbox')=='none','Portable build metadata does not describe the expected wallet')
  manifest={'cryptowallet':metadata.get('sha256')};entries=list(manifest);version_binary='cryptowallet'
 else:
  manifest=json.loads((root/'bin/manifest.json').read_text());entries=[*manifest,'manifest.json'];version_binary='wallet-core'
 for name,digest in manifest.items():check(hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest()==digest,'Component differs from build manifest')
 version=subprocess.check_output([str(root/'bin'/version_binary),'--version'],text=True,timeout=15).strip()
 if args.portable:check(metadata.get('version')==version,'Portable executable and build version differ')
 report['artifact']={'version':version,'binaryManifest':manifest}
 with tempfile.TemporaryDirectory(prefix='cryptowallet-portable-recovery-' if args.portable else 'localwallet-electron-phrase-recovery-') as temporary:
  folder=pathlib.Path(temporary);pinned=folder/'pinned-build';pinned.mkdir()
  for name in entries:shutil.copy2(root/'bin'/name,pinned/name)
  if not args.portable:check(json.loads((pinned/'manifest.json').read_text())==manifest,'Build changed during test startup')
  for name,digest in manifest.items():check(hashlib.sha256((pinned/name).read_bytes()).hexdigest()==digest,'Build changed during test startup')
  def machine(name):
   parent=folder/name;install=parent/'app';install.mkdir(parents=True)
   for component in entries:shutil.copy2(pinned/component,install/component)
   if args.portable:check(sorted(path.name for path in install.iterdir())==['cryptowallet'],'Portable installation includes helper or manifest files')
   home=parent/'home';home.mkdir(mode=0o700);config=home/'config';config.mkdir(mode=0o700)
   return install,home,config
  password='synthetic-local-password-426917';different='different-local-password-861327';canaries.extend([password.encode(),different.encode()])
  first=machine('machine-a');a=Client(*first,password);initial=a.call('accounts');check(initial['vaultState']=='missing' and initial['accounts'] is None,'Initial installation is not empty')
  pending=a.call('create',{'words':12},inputs=[password,password],wrong=True);phrase=a.phrase
  check(phrase is not None and not pending['locked'] and not pending['recoveryConfirmed'],'Incorrect words enabled the wallet')
  check((first[2]/'localwallet/mainnet/vault.lwv').exists(),'Generated root was not encrypted durably')
  passed('Headless Go backend persists a random root and rejects wrong recorded words through typed signer prompts')
  ready=a.call('confirmRecovery',expected_phrase=phrase);check(ready['recoveryConfirmed'] and len(ready['accounts'])==8,'Phrase confirmation did not enable all networks')
  for index in [1,127]:a.call('addAccount',{'index':index})
  expected=descriptors(a.call('accounts'));check(len(expected)==24,'Canonical account groups missing');a.stop()
  passed('Password reauthentication and correct words enable all eight networks at indices0,1,127')
  second=machine('machine-b');b=Client(*second,password);check(b.call('accounts')['vaultState']=='missing','Destination inherited a vault')
  check(b.call('import',{'words':12},inputs=[password,password,phrase.decode(),''])['recoveryConfirmed'],'Imported phrase remains pending')
  for index in [1,127]:b.call('addAccount',{'index':index})
  check(descriptors(b.call('accounts'))==expected,'Fresh installation restored different wallet descriptors');b.stop()
  passed('Fresh independent installation restores all24 descriptors using only specified password and recorded phrase')
  b=Client(*second,password);check(b.call('accounts')['locked'],'Restored app started unlocked');b.call('unlock')
  check(descriptors(b.call('accounts'))==expected,'Restart changed wallet identities');b.stop()
  passed('Recovered installation starts locked and unlocks to the same wallets')
  third=machine('machine-c');c=Client(*third,different);check(c.call('accounts')['vaultState']=='missing','Third installation inherited a vault')
  c.call('import',{'words':12},inputs=[different,different,phrase.decode(),''])
  for index in [1,127]:c.call('addAccount',{'index':index})
  check(descriptors(c.call('accounts'))==expected,'Storage password affected key derivation');c.stop()
  passed('Different local app password preserves every recovered wallet identity')
  needles=[value for secret in canaries for value in [secret,secret.hex().encode(),base64.b64encode(secret)]]
  for data in public_frames:check(not any(v in data for v in needles),'Secret crossed ordinary public-data IPC')
  for file in folder.rglob('*'):
   if file.is_file() and file.stat().st_size<32<<20:check(not any(v in file.read_bytes() for v in needles),'Secret persisted outside encrypted storage')
  for log in logs:
   log.seek(0);data=log.read();check(not any(v in data for v in needles),'Secret appeared in process logs')
  passed('Ordinary public-data IPC, persisted files and process logs contain no tested secret canaries')
  report['walletsCompared']=len(expected);report['walletDescriptorsSHA256']=hashlib.sha256(json.dumps(expected,sort_keys=True).encode()).hexdigest();report['promptTransportException']='Transient recovery phrase and input-only passwords/phrase/challenge replies through the private typed prompt channel. Never ordinary snapshots/logs/files/network. This headless driver does not exercise graphical rendering.'
 report['status']='PASS'
except Exception as error:
 report.update(status='FAIL',reason=str(error) if isinstance(error,RuntimeError) else 'Recovery protocol integration failed; no secret diagnostic retained')
finally:
 for process in children:
  if process.poll() is None:
   os.killpg(process.pid,signal.SIGTERM)
   try:process.wait(timeout=5)
   except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
 for log in logs:log.close()
 (reports/('portable-phrase-recovery.json' if args.portable else 'phrase-recovery.json')).write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(report['status']!='PASS')
