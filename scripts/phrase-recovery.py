#!/usr/bin/env python3
"""Restore a disposable generated phrase in isolated copies of shipped Go apps.

Three installation trees, HOME/config directories, core/signer processes and vaults
share only a phrase entered through native GUI input. No wallet file, keyring or
machine-specific state is copied. This simulates a new machine; actual separate
hardware and the full supported-OS matrix retain their own NOT_RUN status.
"""
import base64,hashlib,importlib.util,json,os,pathlib,queue,secrets,shutil,signal,struct,subprocess,tempfile,threading,time
root=pathlib.Path(__file__).resolve().parents[1]
reports=root/'reports';reports.mkdir(exist_ok=True)
def module(name,path):
 spec=importlib.util.spec_from_file_location(name,path);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
x11=module('recovery_x11',root/'tests/e2e/x11.py');phrases=module('recovery_phrase',root/'tests/e2e/phrase.py')
report={'status':'NOT_RUN','scope':__doc__,'steps':[],'notRun':['Separate physical machines','Full clean Ubuntu X11/Wayland lifecycle matrix','Funded transactions']}
children=[];server=None;logs=[];canaries=[];public_frames=[]
def passed(name):report['steps'].append({'name':name,'status':'PASS'});print('PASS '+name,flush=True)
def check(condition,message):
 if not condition:raise RuntimeError(message)
class Client:
 def __init__(self,install,home,config,env):
  self.log=tempfile.TemporaryFile();logs.append(self.log)
  self.process=subprocess.Popen([str(install/'wallet-core'),'--environment=mainnet'],env={**env,'HOME':str(home),'XDG_CONFIG_HOME':str(config)},stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=self.log,start_new_session=True)
  children.append(self.process);self.session=secrets.token_hex(16);self.counter=0;self.responses=queue.Queue()
  threading.Thread(target=self.reader,daemon=True).start()
 def reader(self):
  try:
   while True:
    header=self.process.stdout.read(4)
    if len(header)!=4:raise RuntimeError('Application IPC ended unexpectedly')
    length=struct.unpack('>I',header)[0];check(0<length<=1<<20,'Invalid public response size')
    data=self.process.stdout.read(length);check(len(data)==length,'Truncated public response')
    public_frames.append(data)
    for secret in canaries:check(secret not in data and secret.hex().encode() not in data and base64.b64encode(secret) not in data,'Synthetic secret crossed public IPC')
    message=json.loads(data)
    if message.get('method')=='activity':check(message.get('body')=={},'Native activity was not value-free');continue
    self.responses.put(message)
  except Exception:self.responses.put({'failure':True})
 def start(self,method,body=None):
  self.counter+=1;self.request={'version':1,'id':str(self.counter),'session':self.session,'method':method,'body':body or {}}
  data=json.dumps(self.request).encode();self.process.stdin.write(struct.pack('>I',len(data))+data);self.process.stdin.flush()
 def finish(self):
  response=self.responses.get(timeout=60);check(not response.get('failure'),'Application IPC failed')
  check(all(response.get(k)==self.request[k] for k in ['version','id','session','method']),'Mismatched public response')
  check(not response.get('error'),'Application rejected a recovery test request')
  return response['body']
 def call(self,method,body=None):self.start(method,body);return self.finish()
 def stop(self):
  if self.process.poll() is None:
   self.process.stdin.close()
   try:self.process.wait(timeout=10)
   except subprocess.TimeoutExpired:os.killpg(self.process.pid,signal.SIGKILL);self.process.wait()
def descriptors(snapshot):
 return sorted([{k:a[k] for k in ['id','family','profile','index','network','publicKey','address','parameters'] if k in a} for a in snapshot['accounts']],key=lambda a:a['id'])
try:
 binary_manifest=json.loads((root/'bin/manifest.json').read_text())
 for name,digest in binary_manifest.items():check(hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest()==digest,'Built component differs from manifest')
 report['artifact']={'version':subprocess.check_output([str(root/'bin/wallet-core'),'--version'],text=True).strip(),'binaryManifest':binary_manifest}
 display=next((f':{n}' for n in range(180,190) if not pathlib.Path(f'/tmp/.X11-unix/X{n}').exists() and not pathlib.Path(f'/tmp/.X{n}-lock').exists()),None)
 check(display is not None,'No owned private display available')
 executable=shutil.which('Xvfb') or str(root/'.tools/xvfb/root/usr/bin/Xvfb')
 env={**os.environ,'DISPLAY':display,'GDK_BACKEND':'x11'}
 for key in ['WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:env.pop(key,None)
 if '.tools/' in executable:env['LD_LIBRARY_PATH']=str(root/'.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
 server=subprocess.Popen([executable,display,'-screen','0','1280x1024x24','-nolisten','tcp'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 for _ in range(100):
  if pathlib.Path('/tmp/.X11-unix/X'+display[1:]).exists():break
  time.sleep(.05)
 desktop=x11.Desktop(display.encode())
 with tempfile.TemporaryDirectory(prefix='localwallet-fresh-machine-recovery-') as temporary:
  folder=pathlib.Path(temporary)
  pinned=folder/'pinned-build';pinned.mkdir()
  for component in ['wallet-core','wallet-signer','wallet-launcher','manifest.json']:shutil.copy2(root/'bin'/component,pinned/component)
  check(json.loads((pinned/'manifest.json').read_text())==binary_manifest,'Build manifest changed during test startup')
  for name,digest in binary_manifest.items():check(hashlib.sha256((pinned/name).read_bytes()).hexdigest()==digest,'Build changed during test startup')
  def machine(name):
   machine=folder/name;install=machine/'app';install.mkdir(parents=True)
   for component in ['wallet-core','wallet-signer','wallet-launcher','manifest.json']:shutil.copy2(pinned/component,install/component)
   home=machine/'home';home.mkdir(mode=0o700);config=home/'fresh-config';config.mkdir(mode=0o700)
   return install,home,config
  first=machine('machine-a');second=machine('machine-b');password='synthetic-local-password-426917';different='different-local-password-861327';canaries.extend([password.encode(),different.encode()])
  a=Client(*first,env);initial=a.call('accounts');check(initial['vaultState']=='missing' and initial['accounts'] is None,'First installation was not fresh')
  a.start('create',{'words':12});desktop.inputs('Create local wallet',[password,password]);desktop.accept('Create local wallet')
  phrase=phrases.record(desktop,wrong=True);canaries.append(phrase)
  pending=a.finish();check(not pending['locked'] and not pending['recoveryConfirmed'],'Wrong recorded words enabled wallet')
  check((first[2]/'localwallet/mainnet/vault.lwv').exists(),'Pending generated root was not encrypted durably')
  passed('Fresh generated root persists encrypted before confirmation; incorrect recorded words leave recovery pending')
  a.start('confirmRecovery');desktop.inputs('Complete recovery setup',[password]);desktop.accept('Complete recovery setup');phrases.record(desktop,expected=phrase)
  ready=a.finish();check(ready['recoveryConfirmed'] and len(ready['accounts'])==9,'Recorded phrase did not enable every mainnet domain')
  for index in [1,127]:a.call('addAccount',{'index':index})
  source=a.call('accounts');expected=descriptors(source);check(len(expected)==27,'Additional deterministic account groups missing')
  passed('Native phrase confirmation enables all nine mainnet wallets and account indices0,1,127 without file backup')
  a.stop()
  b=Client(*second,env);check(b.call('accounts')['vaultState']=='missing','Second installation unexpectedly inherited a vault')
  b.start('import',{'words':12});desktop.inputs('Create local wallet',[password,password,phrase.decode('ascii'),'']);desktop.accept('Create local wallet');desktop.accept('Imported account recovery limits')
  restored=b.finish();check(restored['recoveryConfirmed'],'Native phrase restore remained pending')
  for index in [1,127]:b.call('addAccount',{'index':index})
  check(descriptors(b.call('accounts'))==expected,'Fresh independent installation recovered different wallet descriptors')
  passed('Independent fresh installation restores all27 exact public wallet descriptors using only the same app password and recorded phrase')
  b.stop();b=Client(*second,env);check(b.call('accounts')['locked'],'Restored installation resumed unlocked')
  b.start('unlock');desktop.inputs('Unlock local wallet',[password]);desktop.accept('Unlock local wallet');b.finish();check(descriptors(b.call('accounts'))==expected,'Restart changed restored wallet identities')
  passed('Fresh restored installation restarts locked and unlocks to identical wallets')
  b.stop()
  third=machine('machine-c');c=Client(*third,env);check(c.call('accounts')['vaultState']=='missing','Third installation unexpectedly inherited a vault');c.start('import',{'words':12});desktop.inputs('Create local wallet',[different,different,phrase.decode('ascii'),'']);desktop.accept('Create local wallet');desktop.accept('Imported account recovery limits');c.finish()
  for index in [1,127]:c.call('addAccount',{'index':index})
  check(descriptors(c.call('accounts'))==expected,'Local storage password incorrectly affected wallet derivation')
  passed('A different app storage password restores the same wallets; password is independent of BIP39 key derivation')
  c.stop()
  for data in public_frames:
   for secret in canaries:check(all(value not in data for value in [secret,secret.hex().encode(),base64.b64encode(secret)]),'Synthetic secret crossed public IPC before its canary was registered')
  for file in folder.rglob('*'):
   if file.is_file() and file.stat().st_size<32<<20:
    data=file.read_bytes()
    for secret in canaries:check(all(value not in data for value in [secret,secret.hex().encode(),base64.b64encode(secret)]),'Synthetic secret persisted outside encrypted storage')
  for log in logs:
   log.seek(0);data=log.read()
   for secret in canaries:check(secret not in data and secret.hex().encode() not in data and base64.b64encode(secret) not in data,'Synthetic secret appeared in process logs')
  passed('Public IPC, persisted files and process logs contain none of the tested plaintext/hex/base64 secret canaries')
  report['walletDescriptorsSHA256']=hashlib.sha256(json.dumps(expected,sort_keys=True).encode()).hexdigest();report['walletsCompared']=len(expected)
 report['status']='PASS'
except Exception as error:
 # A failed test must not stringify OCR buffers, native inputs or an arbitrary
 # exception object. Detailed secret-free component tests diagnose failures.
 report.update(status='FAIL',reason=str(error) if isinstance(error,RuntimeError) else 'Fresh-installation recovery or native automation failed; no secret diagnostic retained')
finally:
 for process in children:
  if process.poll() is None:
   os.killpg(process.pid,signal.SIGTERM)
   try:process.wait(timeout=5)
   except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
 if server:
  server.terminate();server.wait(timeout=5)
 for log in logs:log.close()
 (reports/'phrase-recovery.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(report['status']!='PASS')
