#!/usr/bin/env python3
"""Exercise the uninstrumented packaged app on an owned private X11 display.
Synthetic public test credentials stay in this test process and native key events.
No clipboard secret transport, debug port, or bypass is used. Opening a wallet
may query its configured public RPC with disposable fixture addresses only.
This is not the clean Ubuntu/Wayland acceptance matrix.
"""
import argparse, base64, ctypes as C, hashlib, importlib.util, json, os, pathlib, shutil, signal, subprocess, tempfile, time
from PIL import Image, ImageOps
from install import verify_binaries
root=pathlib.Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--profile',choices=['mainnet','testnet'],default='mainnet')
selected_profile=parser.parse_args().profile
vault_relative=pathlib.Path('localwallet')/selected_profile/'vault.lwv'
spec=importlib.util.spec_from_file_location('test_x11',root/'tests/e2e/x11.py');mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
reports=root/'reports';reports.mkdir(exist_ok=True)
report={'status':'NOT_RUN','scope':'Uninstrumented portable X11 validation feedback, password-only generation, import, ordinary-folder encrypted backup, restart and fresh-profile restore; disposable generated and public fixture roots only','steps':[], 'notRun':['clean Ubuntu24.04 desktop','Wayland full wallet workflows','OS suspend/session lock','send from installed package','upgrade/uninstall recovery matrix']}
report['environment']=selected_profile
password='public-synthetic-password-79326';phrase='abandon '*11+'about'
owned=[];app=None;display=None;desktop=None;logs=[];live_log_checks=[]
def passed(name): report['steps'].append({'name':name,'status':'PASS'});print('PASS '+name,flush=True)
def until(fn,seconds=20):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  if fn():return
  if app is not None and app.poll() is not None:raise RuntimeError('Packaged application stopped')
  time.sleep(.1)
 raise RuntimeError('Timed out waiting for packaged workflow')
def capture():
 p=reports/'packaged-current.png';desktop.screenshot(p);return p
def textboxes():
 # The public desktop uses light text on dark controls. Normalize its screenshot
 # for OCR; native credential windows are never parsed by this helper.
 if any(t.startswith(('Create local wallet','Unlock local wallet','Restore encrypted portable backup')) for _,t in desktop.windows()):
  raise RuntimeError('Refusing to OCR a native credential window')
 picture=ImageOps.invert(Image.open(capture()).convert('L'))
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).point(lambda value:0 if value<140 else 255)
 ocr=reports/'packaged-public-ocr.png';picture.save(ocr)
 raw=subprocess.check_output(['tesseract',str(ocr),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
 rows=[]
 for line in raw.splitlines()[1:]:
  a=line.split('\t')
  if len(a)==12 and a[11].strip():
   for column in range(6,10):a[column]=str(int(a[column])//2)
   rows.append(a)
 return rows
def clickword(word,maxx=2000):
 for attempt in range(20):
  for a in textboxes():
   if a[11].lower().strip('.,:')==word.lower() and int(a[6])<maxx:
    desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2);return
  desktop.focus(desktop.wait('LocalWallet')[0])
  desktop.key('Home',ctrl=True) if attempt in (8,16) else desktop.key('Page_Down')
  time.sleep(.2)
 raise RuntimeError('Public UI control unavailable: '+word)
def clickline(prefix):
 # Match the public button wording rather than clicking the similar heading.
 lines={}
 for a in textboxes():lines.setdefault(tuple(a[1:5]),[]).append(a)
 for words in lines.values():
  if ' '.join(a[11] for a in words).lower().startswith(prefix.lower()):
   a=words[0];desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2);return
 raise RuntimeError('Public UI button unavailable: '+prefix)
def clickphrase(phrase):
 words=phrase.lower().split()
 for attempt in range(16):
  rows=textboxes()
  for i,a in enumerate(rows):
   segment=rows[i:i+len(words)]
   if len(segment)==len(words) and [b[11].lower().strip('.,:') for b in segment]==words and all(b[1:5]==a[1:5] for b in segment):
    desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2);return
  desktop.key('Home',ctrl=True) if attempt in (0,8) else desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('Public UI phrase unavailable: '+phrase)
def clickprimary(word):
 # The bright primary-button border confuses document-layout OCR at the
 # ordinary threshold. Remove that border while preserving its label contrast.
 textboxes() # Enforce the credential-window exclusion before image processing.
 picture=ImageOps.invert(Image.open(capture()).convert('L'))
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).point(lambda value:0 if value<90 else 255)
 path=reports/'packaged-primary-controls.png';picture.save(path)
 raw=subprocess.check_output(['tesseract',str(path),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
 for line in raw.splitlines()[1:]:
  a=line.split('\t')
  if len(a)==12 and a[11].lower()==word.lower():
   desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click((int(a[6])+int(a[8])//2)//2,(int(a[7])+int(a[9])//2)//2);return
 raise RuntimeError('Public primary control unavailable: '+word)
def waitnative(prefix):
 w,_=desktop.wait(prefix,25);desktop.focus(w);return desktop.geometry(w)
def expectfailure(native_fragment,public_fragment,name,persist=False):
 x,y,w,h=waitnative('Wallet request failed')
 # This notice contains only fixed guidance; no credential input window remains.
 if any(t.startswith(('Create local wallet','Unlock local wallet','Restore encrypted portable backup')) for _,t in desktop.windows()):
  raise RuntimeError('A credential window remained behind failure guidance')
 picture=Image.open(capture()).crop((max(0,x),max(0,y),min(1280,x+w),min(1024,y+h))).convert('L')
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS)
 path=reports/'packaged-native-guidance.png';picture.save(path)
 text=subprocess.check_output(['tesseract',str(path),'stdout'],stderr=subprocess.DEVNULL,text=True)
 if native_fragment.lower() not in ' '.join(text.split()).lower():raise RuntimeError('Native failure guidance is missing: '+name)
 desktop.accept('Wallet request failed')
 def visible():return public_fragment.lower() in ' '.join(a[11] for a in textboxes()).lower()
 until(visible)
 if persist:
  # The locked status poll runs every five seconds. Its next response must not
  # erase the error before the user can read it.
  end=time.monotonic()+6
  while time.monotonic()<end:time.sleep(.1)
  if not visible():raise RuntimeError('Locked polling erased public failure guidance')
 desktop.screenshot(reports/('packaged-'+name+'.png'));passed(name)
def stopapp():
 global app
 if app is not None:
  # Capture diagnostics while the app is live. Forced test teardown can itself
  # produce Chromium service/GPU shutdown noise and is outside this assertion.
  logs[-1].flush();logs[-1].seek(0);live=logs[-1].read()
  live_log_checks.append({'mesaLoaderError':'MESA-LOADER:' in live,'ordinaryIPCStackTrace':"Error occurred in handler for 'wallet:request'" in live})
  os.killpg(app.pid,signal.SIGTERM)
  try:app.wait(timeout=10)
  except subprocess.TimeoutExpired:os.killpg(app.pid,signal.SIGKILL);app.wait()
  app=None;time.sleep(.5)
def launch(profile,env,environment=None):
 global app
 env={**env,'XDG_CONFIG_HOME':str(profile)}
 log=tempfile.TemporaryFile(mode='w+');logs.append(log)
 launcher=root/'wallet' if (root/'wallet').exists() else root/'dist/LocalWallet-linux-x64/wallet-launcher'
 runtime=launcher.resolve().parent
 identity={'version':subprocess.check_output([str(launcher),'--version'],env=env,text=True,timeout=10).strip(),'binaryManifest':verify_binaries(runtime),'asarSHA256':hashlib.sha256((runtime/'resources/app.asar').read_bytes()).hexdigest()}
 if 'artifact' in report and report['artifact']!=identity:raise RuntimeError('Portable runtime changed between recovery steps')
 report['artifact']=identity
 arguments=[str(launcher),'--ozone-platform=x11']
 # Mainnet deliberately uses no profile override: this tests the default users
 # receive. Testnet is always an explicit isolated developer profile.
 environment=environment or selected_profile
 if environment=='testnet':arguments.append('--profile=testnet')
 app=subprocess.Popen(arguments,env=env,stdout=log,stderr=log,start_new_session=True)
 desktop.wait('LocalWallet',20);time.sleep(1)
def clip():
 # Only the public address is requested. No secret is ever written to a clipboard.
 gtk=C.CDLL('libgtk-3.so.0');gdk=C.CDLL('libgdk-3.so.0');glib=C.CDLL('libglib-2.0.so.0')
 gtk.gtk_init_check.argtypes=[C.c_void_p,C.c_void_p];gtk.gtk_init_check.restype=C.c_int
 gdk.gdk_atom_intern_static_string.argtypes=[C.c_char_p];gdk.gdk_atom_intern_static_string.restype=C.c_void_p
 gtk.gtk_clipboard_get.argtypes=[C.c_void_p];gtk.gtk_clipboard_get.restype=C.c_void_p
 gtk.gtk_clipboard_wait_for_text.argtypes=[C.c_void_p];gtk.gtk_clipboard_wait_for_text.restype=C.c_void_p
 glib.g_free.argtypes=[C.c_void_p]
 if not gtk.gtk_init_check(None,None):raise RuntimeError('Independent public clipboard reader unavailable')
 p=gtk.gtk_clipboard_wait_for_text(gtk.gtk_clipboard_get(gdk.gdk_atom_intern_static_string(b'CLIPBOARD')))
 if not p:raise RuntimeError('Public address clipboard empty')
 s=C.string_at(p).decode();glib.g_free(p);return s
def publicaddress():
 openfirstwallet()
 clickword('Copy');waitnative('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3);return clip()
def overview():
 desktop.focus(desktop.wait('LocalWallet')[0]);desktop.key('Home',ctrl=True);time.sleep(.2)
 clickphrase('Main account');time.sleep(.3)
def openfirstwallet():
 overview();clickword('Open');time.sleep(.5)
 until(lambda:'Balance' in ' '.join(a[11] for a in textboxes()))
def copyfirstcard():
 overview()
 # Locate the actual public first card by its Open wallet button. The copy
 # control is at the right edge of its single-line address row immediately
 # above. This uses native XTest pointer input, never DOM/debug injection.
 for attempt in range(12):
  rows=textboxes()
  for i,a in enumerate(rows[:-1]):
   if a[11].lower()=='open' and rows[i+1][11].lower()=='wallet' and a[1:5]==rows[i+1][1:5]:
    x=int(a[6]);y=int(a[7])
    # First-card width is determined by the responsive grid, not host DPI.
    # Find its two vertical border edges in the screenshot's own pixels.
    picture=Image.open(capture()).convert('RGB')
    border=(53,66,89)
    edges=[xx for xx in range(max(0,x-40),min(1280,x+500)) if sum(abs(picture.getpixel((xx,max(0,y-10)))[c]-border[c]) for c in range(3))<9]
    left=next((xx for xx in edges if xx<x),None)
    right=next((xx for xx in edges if xx>x+100),None)
    if left is None or right is None:raise RuntimeError('Public wallet card boundaries unavailable for native copy icon click')
    desktop.click(right-35,y-42)
    waitnative('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3);return clip()
  desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('First public wallet card not available')
def filechoice(prefix,path):
 waitnative(prefix);desktop.fit(desktop.wait(prefix)[0]);desktop.key('l',ctrl=True);desktop.type(str(path));desktop.key('Return');time.sleep(.5)
 if any(t.startswith(prefix) for _,t in desktop.windows()):desktop.accept(prefix)
try:
 server=shutil.which('Xvfb') or str(root/'.tools/xvfb/root/usr/bin/Xvfb')
 display=next((f':{n}' for n in range(180,190) if not pathlib.Path(f'/tmp/.X11-unix/X{n}').exists() and not pathlib.Path(f'/tmp/.X{n}-lock').exists()),None)
 if not display:raise RuntimeError('No unused private display; refusing user display')
 env={**os.environ,'DISPLAY':display};env.pop('WAYLAND_DISPLAY',None);env.pop('XDG_RUNTIME_DIR',None);env.pop('DBUS_SESSION_BUS_ADDRESS',None)
 if '.tools/' in server:env['LD_LIBRARY_PATH']=str(root/'.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
 xvlog=tempfile.TemporaryFile();xv=subprocess.Popen([server,display,'-screen','0','1280x1024x24','-nolisten','tcp'],env=env,stdout=xvlog,stderr=xvlog);owned.append(xv)
 until(lambda:pathlib.Path('/tmp/.X11-unix/X'+display[1:]).exists(),10)
 desktop=mod.Desktop(display.encode());os.environ.update(DISPLAY=display,GDK_BACKEND='x11')
 with tempfile.TemporaryDirectory(prefix='localwallet-package-fixture-') as temporary:
  folder=pathlib.Path(temporary);profile=folder/'original';backupdir=folder/'backup';backupdir.mkdir(mode=0o755);backupdir.chmod(0o755);backup=backupdir/'portable.lwv'
  home=folder/'home';home.mkdir(mode=0o700);env['HOME']=str(home)
  os.environ['HOME']=str(home)
  for key in ['WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:os.environ.pop(key,None)
  generated=folder/'generated';launch(generated,env)
  if any(t.startswith(('Create local wallet','Unlock local wallet')) for _,t in desktop.windows()):raise RuntimeError('First launch opened a credential window without a user action')
  desktop.screenshot(reports/'packaged-first-run.png')
  clickline('Create app password');waitnative('Create local wallet')
  desktop.inputs('Create local wallet',['bad','bad']);desktop.accept('Create local wallet')
  expectfailure('UTF-8 bytes','Use an app password','short-password-feedback',persist=True)
  if (generated/vault_relative).exists():raise RuntimeError('Short password created a vault')
  clickline('Create app password');waitnative('Create local wallet')
  desktop.inputs('Create local wallet',[password,'different-public-password-79326']);desktop.accept('Create local wallet')
  expectfailure('do not match','passwords did not match','password-mismatch-feedback')
  if (generated/vault_relative).exists():raise RuntimeError('Mismatched passwords created a vault')
  clickline('Create app password');waitnative('Create local wallet')
  desktop.inputs('Create local wallet',[password,password]);desktop.accept('Create local wallet')
  until(lambda:(generated/vault_relative).exists());time.sleep(2)
  words=' '.join(a[11] for a in textboxes())
  if 'Main account' not in words or selected_profile.title()+' profile' not in words:raise RuntimeError('Generated wallet did not show Main account and its actual profile identity')
  desktop.screenshot(reports/'packaged-generated-main-account.png')
  passed('Explicit first-run password-only generation and Main account overview without phrase reveal')
  generated_backup=backupdir/'generated.lwv'
  clickword('backup',350);filechoice('Save encrypted portable backup',generated_backup)
  until(lambda:generated_backup.exists());time.sleep(2)
  if generated_backup.stat().st_mode&0o777!=0o600 or backupdir.stat().st_mode&0o777!=0o755 or (backupdir/'writer.lock').exists():raise RuntimeError('Backup altered folder policy or did not retain owner-only file permissions')
  original_backup=generated_backup.read_bytes()
  passed('Verified owner-only encrypted backup in ordinary 0755 folder without folder mutation or lockfile')
  generated_address=publicaddress()
  if generated_address=='0x9858EfFD232B4033E47d90003D41EC34EcaEda94':raise RuntimeError('Generated wallet reused the fixed import fixture')
  clickword('backup',350);filechoice('Save encrypted portable backup',generated_backup)
  expectfailure('Choose a new backup filename','A file already exists','existing-backup-feedback')
  if generated_backup.read_bytes()!=original_backup:raise RuntimeError('Existing encrypted backup changed')
  unsafe=folder/'unsafe';unsafe.mkdir();unsafe.chmod(0o777)
  clickword('backup',350);filechoice('Save encrypted portable backup',unsafe/'rejected.lwv')
  expectfailure('other users cannot modify','destination is unavailable or unsafe','unsafe-backup-folder-feedback')
  if (unsafe/'rejected.lwv').exists():raise RuntimeError('Backup accepted writable-by-others folder')
  if publicaddress()!=generated_address:raise RuntimeError('Rejected backup damaged the active wallet')
  passed('Rejected backups preserve existing ciphertext and leave the active wallet usable')
  stopapp()
  launch(folder/'generated-restored',env);clickword('Other',350);clickword('Restore',350);filechoice('Open encrypted backup',generated_backup)
  waitnative('Restore encrypted portable backup');desktop.inputs('Restore encrypted portable backup',[password]);desktop.accept('Restore encrypted portable backup');time.sleep(3)
  if publicaddress()!=generated_address:raise RuntimeError('Generated root did not survive encrypted backup restore')
  passed('Password-only generated root restored from verified encrypted backup into an empty profile')
  stopapp()
  launch(profile,env);clickword('Other',350);clickword('Import',350);waitnative('Create local wallet')
  desktop.inputs('Create local wallet',[password,password,'bad','']);desktop.accept('Create local wallet')
  expectfailure('valid English BIP39','valid English BIP39','invalid-recovery-phrase-feedback')
  if (profile/vault_relative).exists():raise RuntimeError('Invalid phrase created a vault')
  clickword('Import',350);x,y,w,h=waitnative('Create local wallet')
  desktop.inputs('Create local wallet',[password,password,phrase,''])
  desktop.screenshot(reports/'packaged-native-masked.png');desktop.accept('Create local wallet')
  waitnative('Imported account recovery limits');desktop.accept('Imported account recovery limits')
  until(lambda:(profile/vault_relative).exists());time.sleep(1);passed('Native input-only BIP39 import with production KDF')
  expected='0x9858EfFD232B4033E47d90003D41EC34EcaEda94'
  clickword('backup',350);filechoice('Save encrypted portable backup',backup)
  until(lambda:backup.exists());time.sleep(3);passed('Native encrypted portable backup creation and reopen verification')
  if publicaddress()!=expected:raise RuntimeError('Imported independent fixture public address differs')
  passed('Go-owned verified receive QR and exact independent public address')
  if copyfirstcard()!=expected:raise RuntimeError('Wallet cell copy icon copied a different public address')
  passed('Wallet cell copy icon invokes native verification and copies the full independent public address')
  openfirstwallet()
  words=' '.join(a[11] for a in textboxes())
  if 'Balance' not in words or 'HyperEVM' in words:raise RuntimeError('Open wallet did not show an isolated Ethereum tab with its balance instead of the portfolio grid')
  desktop.screenshot(reports/'packaged-wallet-tab.png')
  clickprimary('Receive');waitnative('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3)
  clickprimary('Send');time.sleep(.3)
  if 'Full recipient' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Wallet Send button did not focus its own payment form')
  passed('Open wallet selects a wallet tab with scoped balance and send/receive controls instead of the portfolio grid')
  stopapp();launch(profile,env)
  # A restart never resumes the signer session; unlock requires native input.
  clickword('Unlock',350);x,y,w,h=waitnative('Unlock local wallet');desktop.inputs('Unlock local wallet',[password]);desktop.accept('Unlock local wallet');time.sleep(3)
  if publicaddress()!=expected:raise RuntimeError('Restart public address differs')
  passed('Restart requires native unlock and preserves deterministic address')
  stopapp();fresh=folder/'fresh';launch(fresh,env);clickword('Other',350);clickword('Restore',350);filechoice('Open encrypted backup',backup)
  x,y,w,h=waitnative('Restore encrypted portable backup');desktop.inputs('Restore encrypted portable backup',[password]);desktop.accept('Restore encrypted portable backup')
  until(lambda:(fresh/vault_relative).exists());time.sleep(3)
  if publicaddress()!=expected:raise RuntimeError('Fresh-profile public address differs')
  passed('Portable restore into empty profile without original application state')
  desktop.screenshot(reports/'packaged-restored.png')
  stopapp()
  if selected_profile=='mainnet':
   saved=hashlib.sha256((fresh/vault_relative).read_bytes()).hexdigest()
   launch(fresh,env,environment='testnet')
   words=' '.join(a[11] for a in textboxes())
   if 'Testnet profile' not in words or 'Create your app password' not in words or (fresh/'localwallet/testnet/vault.lwv').exists():raise RuntimeError('Explicit testnet launch reused or auto-created a mainnet root')
   if hashlib.sha256((fresh/vault_relative).read_bytes()).hexdigest()!=saved:raise RuntimeError('Explicit testnet launch modified the mainnet vault')
   passed('Explicit testnet profile starts empty and preserves existing mainnet ciphertext without conversion')
   stopapp();launch(fresh,env)
   words=' '.join(a[11] for a in textboxes())
   if 'Mainnet profile' not in words or 'Welcome back' not in words:raise RuntimeError('Default launch failed to return to the existing locked mainnet profile')
   clickword('Unlock',350);waitnative('Unlock local wallet');desktop.inputs('Unlock local wallet',[password]);desktop.accept('Unlock local wallet');time.sleep(2)
   clickword('Add',350);time.sleep(2)
   desktop.key('Home',ctrl=True);clickphrase('Account 2');time.sleep(.5)
   words=' '.join(a[11] for a in textboxes())
   if 'Derivation index 1' not in words:raise RuntimeError('Second deterministic account did not have its own top tab page')
   desktop.screenshot(reports/'packaged-account-tabs.png')
   passed('Additional deterministic account appears as its own top tab with index-one network wallets')
   stopapp()
  canaries=[password.encode(),phrase.encode(),bytes.fromhex('00'*16)]
  # Entropy all-zero bytes are not scanned: binary formats legitimately contain
  # zero padding. Distinct password/phrase and their common encodings are checked.
  needles=[]
  for b in canaries[:2]:needles.extend([b,b.hex().encode(),base64.b64encode(b)])
  for p in folder.rglob('*'):
   if p.is_file() and p.stat().st_size<32<<20:
    data=p.read_bytes()
    if any(n in data for n in needles):raise RuntimeError('Synthetic canary found in persisted application files')
  for log in logs:
   log.seek(0);data=log.read().encode()
   if any(n in data for n in needles):raise RuntimeError('Synthetic canary found in captured process logs')
  passed('Captured logs and persisted profile files contain no tested secret canaries')
  report['liveLogChecks']=live_log_checks
  if any(row['mesaLoaderError'] or row['ordinaryIPCStackTrace'] for row in live_log_checks):raise RuntimeError('Live packaged workflow emitted a Mesa loader error or ordinary IPC handler stack trace')
  passed('Invalid native inputs and backup failures produce guidance without ordinary IPC handler stack traces or Mesa loader errors')
  report['backupSHA256']=hashlib.sha256(backup.read_bytes()).hexdigest()
 report['status']='PASS'
except Exception as error:
 report.update(status='FAIL',reason=str(error));print(str(error),flush=True)
 if app is not None:
  report['directChildProcesses']=subprocess.run(['ps','-o','pid,ppid,pgid,stat,comm','--ppid',str(app.pid)],text=True,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL).stdout
 if desktop:
  desktop.screenshot(reports/'packaged-failure.png');print(desktop.windows(),flush=True)
finally:
 stopapp()
 for p in owned:
  p.terminate()
  try:p.wait(timeout=5)
  except subprocess.TimeoutExpired:p.kill();p.wait()
 for l in logs:l.close()
 (reports/'packaged-recovery.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(report['status']!='PASS')
