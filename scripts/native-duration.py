#!/usr/bin/env python3
"""Actual production five-minute keepalive then idle lock, isolated X11 only.
This takes about eleven minutes. No confirmation bypass or runtime timeout knob.
"""
import importlib.util,json,os,pathlib,signal,subprocess,tempfile,time,hashlib
root=pathlib.Path(__file__).resolve().parents[1];reports=root/'reports';reports.mkdir(exist_ok=True)
spec=importlib.util.spec_from_file_location('x',root/'tests/e2e/x11.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
report={'status':'NOT_RUN','scope':'Actual native key events through production five-minute inactivity boundary on private X11; not OS suspend/Wayland acceptance','seconds':{}}
xv=app=None;log=None;start=time.monotonic()
def control(d,word):
 shot=reports/'native-duration-current.png';d.screenshot(shot)
 tsv=subprocess.check_output(['tesseract',str(shot),'stdout','tsv'],text=True,stderr=subprocess.DEVNULL)
 for line in tsv.splitlines()[1:]:
  a=line.split('\t')
  if len(a)==12 and a[11].lower().startswith(word.lower()) and int(a[6])<320:
   d.focus(d.wait('LocalWallet')[0]);d.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2);return
 if word=='Change':
  d.focus(d.wait('LocalWallet')[0]);d.click(180,615);return
 raise RuntimeError('Public control not found: '+word)
try:
 display=next(f':{n}' for n in range(180,190) if not pathlib.Path(f'/tmp/.X11-unix/X{n}').exists() and not pathlib.Path(f'/tmp/.X{n}-lock').exists())
 with tempfile.TemporaryDirectory(prefix='wallet-native-duration-') as temp:
  folder=pathlib.Path(temp);(folder/'home').mkdir(mode=0o700)
  env={**os.environ,'DISPLAY':display,'HOME':str(folder/'home'),'XDG_CONFIG_HOME':str(folder/'config')}
  for key in ['WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:env.pop(key,None)
  server=root/'.tools/xvfb/root/usr/bin/Xvfb';env['LD_LIBRARY_PATH']=str(root/'.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
  log=tempfile.TemporaryFile(mode='w+');xv=subprocess.Popen([str(server),display,'-screen','0','1280x1024x24','-nolisten','tcp'],env=env,stdout=log,stderr=log)
  for _ in range(100):
   if pathlib.Path('/tmp/.X11-unix/X'+display[1:]).exists():break
   time.sleep(.05)
  d=m.Desktop(display.encode())
  report['launcherSHA256']=hashlib.sha256((root/'cryptowallet').resolve().read_bytes()).hexdigest()
  app=subprocess.Popen([str(root/'cryptowallet'),'--ozone-platform=x11'],env=env,stdout=log,stderr=log,start_new_session=True)
  d.wait('LocalWallet',20);time.sleep(1);control(d,'Import')
  w,_=d.wait('Create local wallet');d.focus(w);x,y,_,_=d.geometry(w)
  password='public-synthetic-duration-password-79326';phrase='abandon '*11+'about'
  d.inputs('Create local wallet',[password,password,phrase,''])
  d.accept('Create local wallet');d.wait('Imported account recovery limits',30);d.accept('Imported account recovery limits');time.sleep(2)
  control(d,'Change');w,_=d.wait('Change storage password',20);d.focus(w);x,y,_,_=d.geometry(w);d.inputs('Change storage password',['','',''])
  typing=time.monotonic();count=0
  while time.monotonic()-typing<315:
   if not any(t.startswith('Change storage password') for _,t in d.windows()):raise RuntimeError('Native prompt was killed despite real key activity')
   d.key('a');d.key('BackSpace');count+=1;lastInput=time.monotonic();time.sleep(10)
  report['seconds']['activeNativeInput']=round(time.monotonic()-typing,3);report['nativeInputPairs']=count
  print('PASS native prompt remains live through actual five-minute input boundary',flush=True)
  idle=lastInput
  while time.monotonic()-idle<340:
   if not any(t.startswith('Change storage password') for _,t in d.windows()):break
   time.sleep(1)
  else:raise RuntimeError('Native prompt remained open beyond the five-minute inactivity deadline')
  elapsed=time.monotonic()-idle
  if elapsed<275:raise RuntimeError('Idle lock occurred too early after genuine input')
  report['seconds']['idleLockAfterLastInput']=round(elapsed,3);d.screenshot(reports/'native-duration-locked.png')
  print('PASS production inactivity kills native session',flush=True)
  report.update(status='PASS')
except Exception as e:
 report.update(status='FAIL',reason=str(e));print(str(e),flush=True)
 if 'd' in locals():d.screenshot(reports/'native-duration-failure.png')
finally:
 if app:
  try:os.killpg(app.pid,signal.SIGTERM);app.wait(timeout=10)
  except ProcessLookupError:pass
  except subprocess.TimeoutExpired:os.killpg(app.pid,signal.SIGKILL);app.wait()
 if xv:xv.terminate();xv.wait(timeout=5)
 if log:log.close()
 report['seconds']['total']=round(time.monotonic()-start,3)
 (reports/'native-duration.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(report['status']!='PASS')
