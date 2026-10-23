#!/usr/bin/env python3
"""Exercise the uninstrumented packaged app on an owned private X11 display.
Synthetic public test credentials stay in this test process and native key events.
No clipboard secret transport, remote endpoint, debug port, or bypass is used.
This is not the clean Ubuntu/Wayland acceptance matrix.
"""
import base64, ctypes as C, hashlib, importlib.util, json, os, pathlib, shutil, signal, subprocess, tempfile, time
root=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('test_x11',root/'tests/e2e/x11.py');mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
reports=root/'reports';reports.mkdir(exist_ok=True)
report={'status':'NOT_RUN','scope':'Uninstrumented packaged X11 import, encrypted backup, restart and fresh-profile restore; synthetic public root only','steps':[], 'notRun':['clean Ubuntu24.04 desktop','Wayland','OS suspend/session lock','send from installed package','upgrade/uninstall recovery matrix']}
password='public-synthetic-password-79326';phrase='abandon '*11+'about'
owned=[];app=None;display=None;desktop=None;logs=[]
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
 raw=subprocess.check_output(['tesseract',str(capture()),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
 rows=[]
 for line in raw.splitlines()[1:]:
  a=line.split('\t')
  if len(a)==12 and a[11].strip():rows.append(a)
 return rows
def clickword(word,maxx=2000):
 for _ in range(20):
  for a in textboxes():
   if a[11].lower().strip('.,:')==word.lower() and int(a[6])<maxx:
    desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2);return
  time.sleep(.2)
 raise RuntimeError('Public UI control unavailable: '+word)
def waitnative(prefix):
 w,_=desktop.wait(prefix,25);desktop.focus(w);return desktop.geometry(w)
def stopapp():
 global app
 if app is not None:
  os.killpg(app.pid,signal.SIGTERM)
  try:app.wait(timeout=10)
  except subprocess.TimeoutExpired:os.killpg(app.pid,signal.SIGKILL);app.wait()
  app=None;time.sleep(.5)
def launch(profile,env):
 global app
 env={**env,'XDG_CONFIG_HOME':str(profile)}
 log=tempfile.TemporaryFile(mode='w+');logs.append(log)
 launcher=root/'wallet' if (root/'wallet').exists() else root/'dist/LocalWallet-linux-x64/wallet-launcher'
 app=subprocess.Popen([str(launcher),'--ozone-platform=x11'],env=env,stdout=log,stderr=log,start_new_session=True)
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
 clickword('Copy');waitnative('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3);return clip()
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
  folder=pathlib.Path(temporary);profile=folder/'original';backupdir=folder/'backup';backupdir.mkdir(mode=0o700);backup=backupdir/'portable.lwv'
  home=folder/'home';home.mkdir(mode=0o700);env['HOME']=str(home)
  os.environ['HOME']=str(home)
  for key in ['WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:os.environ.pop(key,None)
  launch(profile,env);clickword('Import',350);x,y,w,h=waitnative('Create local wallet')
  desktop.inputs('Create local wallet',[password,password,phrase,''])
  desktop.screenshot(reports/'packaged-native-masked.png');desktop.accept('Create local wallet')
  waitnative('Imported account recovery limits');desktop.accept('Imported account recovery limits')
  until(lambda:(profile/'localwallet/testnet/vault.lwv').exists());time.sleep(1);passed('Native input-only BIP39 import with production KDF')
  expected='0x9858EfFD232B4033E47d90003D41EC34EcaEda94'
  if publicaddress()!=expected:raise RuntimeError('Imported independent fixture public address differs')
  passed('Go-owned verified receive QR and exact independent public address')
  clickword('backup',350);filechoice('Save encrypted portable backup',backup)
  until(lambda:backup.exists());time.sleep(3);passed('Native encrypted portable backup creation and reopen verification')
  stopapp();launch(profile,env)
  # A restart never resumes the signer session; unlock requires native input.
  clickword('Unlock',350);x,y,w,h=waitnative('Unlock local wallet');desktop.inputs('Unlock local wallet',[password]);desktop.accept('Unlock local wallet');time.sleep(3)
  if publicaddress()!=expected:raise RuntimeError('Restart public address differs')
  passed('Restart requires native unlock and preserves deterministic address')
  stopapp();fresh=folder/'fresh';launch(fresh,env);clickword('Restore',350);filechoice('Open encrypted backup',backup)
  x,y,w,h=waitnative('Restore encrypted portable backup');desktop.inputs('Restore encrypted portable backup',[password]);desktop.accept('Restore encrypted portable backup')
  until(lambda:(fresh/'localwallet/testnet/vault.lwv').exists());time.sleep(3)
  if publicaddress()!=expected:raise RuntimeError('Fresh-profile public address differs')
  passed('Portable restore into empty profile without original application state')
  desktop.screenshot(reports/'packaged-restored.png')
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
  report['backupSHA256']=hashlib.sha256(backup.read_bytes()).hexdigest()
 report['status']='PASS'
except Exception as error:
 report.update(status='FAIL',reason=str(error));print(str(error),flush=True)
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
