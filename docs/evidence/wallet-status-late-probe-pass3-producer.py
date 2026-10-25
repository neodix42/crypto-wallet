#!/usr/bin/env python3
"""Exercise the uninstrumented packaged app on an owned private X11 display.
Synthetic public test credentials stay in this test process and dialog key events.
No clipboard secret transport, debug port, or bypass is used. The overview and
opened wallets query configured public RPCs with disposable fixture addresses.
This is not the clean Ubuntu/Wayland acceptance matrix.
"""
import argparse, base64, ctypes as C, hashlib, importlib.util, json, os, pathlib, re, shutil, signal, subprocess, tempfile, time
from PIL import Image, ImageOps
import sys
sys.path.insert(0,'/home/neodix/gitProjects/crypto-wallet/scripts')
from install import verify_binaries
root=pathlib.Path('/home/neodix/gitProjects/crypto-wallet')
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--profile',choices=['mainnet','testnet'],default='mainnet')
selected_profile=parser.parse_args().profile
vault_relative=pathlib.Path('localwallet')/selected_profile/'vault.lwv'
spec=importlib.util.spec_from_file_location('test_x11',root/'tests/e2e/x11.py');mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
reports=root/'reports';reports.mkdir(exist_ok=True)
phrase_spec=importlib.util.spec_from_file_location('test_phrase',root/'tests/e2e/phrase.py');phrase_ui=importlib.util.module_from_spec(phrase_spec);phrase_spec.loader.exec_module(phrase_ui)
report={'status':'NOT_RUN','scope':'Uninstrumented portable X11 password-to-dialog-phrase setup, recorded-word confirmation, isolated fresh-profile phrase restore, main-page Settings Wallet/Exchange/Journal navigation, fixed-height bottom status feedback without moving its public page anchor, numeric native balances and hover-only observation metadata, unclipped table addresses, persistent wallet address/copy, asset tabs and whole-asset Amount wording, unconfigured Decred table/wallet connection guidance, receive QR/copy, tab closing, disconnected Exchange controls and isolated Connect cancellation; disposable generated and public fixture roots only','steps':[], 'notRun':['clean Ubuntu24.04 desktop','Wayland full wallet workflows','OS suspend/session lock','send from installed package','upgrade/uninstall recovery matrix','live Bybit authentication, deposits, spot fills and withdrawals','authenticated live Decred mainnet balance/discovery']}
report['environment']=selected_profile
report['producerSHA256']=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
report['phraseHelperSHA256']=hashlib.sha256((root/'tests/e2e/phrase.py').read_bytes()).hexdigest()
report.update(status='RUNNING',sourceStable=False)
(reports/'wallet-status-late-probe.json').write_text(json.dumps(report,indent=2)+'\n')
# Keep the pinned test decoder's existing build cache when the wallet itself
# receives a fresh HOME below. These paths are never passed to the wallet.
decoder_environment=json.loads(subprocess.check_output([str(root/'scripts/with-go.sh'),'go','env','-json','GOROOT','GOPATH','GOMODCACHE','GOCACHE'],cwd=root,text=True))
password='public-synthetic-password-79326';phrase='abandon '*11+'about'
owned=[];app=None;display=None;desktop=None;logs=[];live_log_checks=[];x11_error_handler_refs=None
def passed(name): report['steps'].append({'name':name,'status':'PASS'});print('PASS '+name,flush=True)
def install_owned_window_query_handler(desktop,report,report_path):
 # Window destruction can race XQueryTree/GetWindowAttributes on this owned
 # private display. Tolerate only server-confirmed absence for the exact
 # resource while its one synchronous metadata lookup is active.
 class XError(C.Structure):
  _fields_=[('type',C.c_int),('display',C.c_void_p),('resourceid',C.c_ulong),('serial',C.c_ulong),('error_code',C.c_ubyte),('request_code',C.c_ubyte),('minor_code',C.c_ubyte)]
 handler_type=C.CFUNCTYPE(C.c_int,C.c_void_p,C.POINTER(XError))
 previous=None;active_query=None
 original_attributes=mod.X.XGetWindowAttributes
 mod.X.XSync.argtypes=[C.c_void_p,C.c_int]
 @handler_type
 def handler(connection,event):
  error=event.contents
  if active_query is not None and connection==desktop.d==active_query['connection'] and error.resourceid==active_query['resource'] and error.error_code==3 and error.request_code==3:
   active_query['absent']=True
   report['transientClosedWindowQueries']=report.get('transientClosedWindowQueries',0)+1
   return 0
  # Every other error retains native failure behavior; record FAIL before the
  # prior/default handler can terminate Python without running finally.
  report.update(status='FAIL',sourceStable=False,reason='Unexpected native X11 driver error',x11Error={'code':error.error_code,'request':error.request_code,'minor':error.minor_code})
  report_path.write_text(json.dumps(report,indent=2)+'\n')
  if previous is not None:return previous(connection,event)
  os._exit(1)
 def attributes(connection,resource,value):
  nonlocal active_query
  # Drain unrelated requests before establishing the narrow exception scope.
  mod.X.XSync(connection,0)
  if active_query is not None:raise RuntimeError('Nested X11 metadata query')
  active_query={'connection':connection,'resource':resource,'absent':False}
  try:
   result=original_attributes(connection,resource,value);mod.X.XSync(connection,0)
   return 0 if active_query['absent'] else result
  finally:active_query=None
 mod.X.XSetErrorHandler.argtypes=[handler_type];mod.X.XSetErrorHandler.restype=C.c_void_p
 address=mod.X.XSetErrorHandler(handler)
 if address:previous=handler_type(address)
 mod.X.XGetWindowAttributes=attributes
 def restore():
  mod.X.XSync(desktop.d,0);mod.X.XGetWindowAttributes=original_attributes
  if previous is not None:mod.X.XSetErrorHandler(previous)
 return handler,previous,restore
def until(fn,seconds=20):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  if fn():return
  if app is not None and app.poll() is not None:raise RuntimeError('Packaged application stopped')
  time.sleep(.1)
 raise RuntimeError('Timed out waiting for packaged workflow')
def capture():
 mod.X.XSync(desktop.d,0)
 if phrase_ui.secret_window(desktop):raise RuntimeError('Refusing to capture a dialog secret window')
 p=reports/'packaged-current.png';desktop.screenshot(p);return p
def textboxes(region=None):
 # The public desktop uses light text on dark controls. Normalize its screenshot
 # for OCR; dialog credential windows are never parsed by this helper.
 if phrase_ui.secret_window(desktop):
  raise RuntimeError('Refusing to OCR a dialog credential window')
 picture=ImageOps.invert(Image.open(capture()).convert('L'))
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).point(lambda value:0 if value<140 else 255)
 ocr=reports/'packaged-public-ocr.png';picture.save(ocr)
 raw=subprocess.check_output(['tesseract',str(ocr),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
 rows=[]
 for line in raw.splitlines()[1:]:
  a=line.split('\t')
  if len(a)==12 and a[11].strip():
   for column in range(6,10):a[column]=str(int(a[column])//2)
   if region is None or (region[0]<=int(a[6])<region[2] and region[1]<=int(a[7])<region[3]):rows.append(a)
 return rows
def scrollpage(button,ticks=100):
 # Ctrl+Home/End is tab-list navigation when a navigation tab owns focus.
 # Real pointer-wheel events scroll the public page without activating a tab,
 # editing a field, inspecting the DOM or injecting renderer events.
 if phrase_ui.secret_window(desktop):raise RuntimeError('Refusing page scroll while a credential window is open')
 x,y,w,h=desktop.geometry(desktop.wait('LocalWallet')[0])
 mod.T.XTestFakeMotionEvent(desktop.d,-1,x+5,y+h//2,0)
 for _ in range(ticks):
  mod.T.XTestFakeButtonEvent(desktop.d,button,1,0);mod.T.XTestFakeButtonEvent(desktop.d,button,0,0)
 mod.X.XFlush(desktop.d);time.sleep(.25)
def scrollhome():scrollpage(4)
def scrollend():scrollpage(5)
def clickword(word,maxx=2000):
 for attempt in range(20):
  for a in textboxes():
   if a[11].lower().strip('.,:')==word.lower() and int(a[6])<maxx:
    desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2);return
  desktop.focus(desktop.wait('LocalWallet')[0])
  scrollhome() if attempt in (8,16) else desktop.key('Page_Down')
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
def clickphrase(phrase,button=1,region=None):
 words=phrase.lower().split()
 for attempt in range(16):
  rows=textboxes(region)
  for i,a in enumerate(rows):
   segment=rows[i:i+len(words)]
   if len(segment)==len(words) and [b[11].lower().strip('.,:') for b in segment]==words and all(b[1:5]==a[1:5] for b in segment):
    desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2,button=button);return
  scrollhome() if attempt in (0,8) else desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('Public UI phrase unavailable: '+phrase)
def clickprimary(word,last=False):
 # The bright primary-button border confuses document-layout OCR at the
 # ordinary threshold. Remove that border while preserving its label contrast.
 textboxes() # Enforce the credential-window exclusion before image processing.
 original=Image.open(capture()).convert('RGB')
 picture=ImageOps.invert(original.convert('L'))
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).point(lambda value:0 if value<90 else 255)
 path=reports/'packaged-primary-controls.png';picture.save(path)
 raw=subprocess.check_output(['tesseract',str(path),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
 rows=[line.split('\t') for line in raw.splitlines()[1:] if len(line.split('\t'))==12 and line.split('\t')[11].strip()]
 words=word.lower().split()
 matches=[]
 for i,a in enumerate(rows):
  segment=rows[i:i+len(words)]
  if len(segment)==len(words) and [b[11].lower() for b in segment]==words and all(b[1:5]==a[1:5] for b in segment):
   # A paragraph can repeat the button label. Require its blue control fill,
   # sampled in the left padding, so prose never becomes a click target.
   px=max(0,int(a[6])//2-8);py=(int(a[7])+int(a[9])//2)//2
   red,green,blue=original.getpixel((px,py))
   if 40<=red<=80 and 65<=green<=120 and 120<=blue<=180:matches.append(a)
 if matches:
  a=matches[-1] if last else matches[0];desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click((int(a[6])+int(a[8])//2)//2,(int(a[7])+int(a[9])//2)//2);return
 raise RuntimeError('Public primary control unavailable: '+word)
def publicphrasebounds(phrase):
 # Public pixels are the oracle: no DOM, accessibility internals, debugging
 # transport or production renderer instrumentation is used by this drill.
 words=phrase.lower().split();rows=textboxes();matches=[]
 for i,a in enumerate(rows):
  segment=rows[i:i+len(words)]
  if len(segment)==len(words) and [b[11].lower().strip('.,:') for b in segment]==words and all(b[1:5]==a[1:5] for b in segment):
   left=min(int(b[6]) for b in segment);top=min(int(b[7]) for b in segment)
   right=max(int(b[6])+int(b[8]) for b in segment);bottom=max(int(b[7])+int(b[9]) for b in segment)
   matches.append((left,top,right,bottom))
 if not matches:raise RuntimeError('Public layout anchor unavailable: '+phrase)
 return min(matches,key=lambda value:(value[1],value[0]))
def statusbarbounds():
 # Observe the full-width border and background in the actual public pixels.
 # This detects layout displacement, an auto-growing bar, or a floating notice
 # without relying on CSS/DOM inspection or a test-only renderer bridge.
 x,y,w,h=desktop.geometry(desktop.wait('LocalWallet')[0]);picture=Image.open(capture()).convert('RGB')
 samples=[x+3,x+w//2,x+w-25]
 candidates=[yy for yy in range(max(y,y+h-200),y+h-1) if all(picture.getpixel((xx,yy))==(64,81,107) for xx in samples) and all(picture.getpixel((xx,yy+1))==(20,30,43) for xx in samples)]
 if len(candidates)!=1:raise RuntimeError('Full-width bottom status bar boundary is missing or ambiguous')
 top=candidates[0]
 if y+h-top!=88:raise RuntimeError('Bottom status bar is not the required fixed 88-pixel height')
 return (x,top,x+w,y+h)
def dismissnotification():
 bar=statusbarbounds()
 if any(a[11].lower()=='dismiss' for a in textboxes(bar)):
  clickphrase('Dismiss',region=bar);time.sleep(.2)
def notificationlayout(anchor,before):
 # A visible error previously inserted a block above the page. Require the
 # same public heading rectangle while feedback is shown and after dismissal,
 # and prove that its dismiss control is inside the fixed bottom status bar.
 bar=statusbarbounds()
 if not any(a[11].lower()=='dismiss' for a in textboxes(bar)):raise RuntimeError('Bottom status feedback has no visible dismiss control')
 during=publicphrasebounds(anchor)
 if during!=before:raise RuntimeError('Feedback moved the public page anchor')
 dismissnotification();after=publicphrasebounds(anchor)
 if after!=before:raise RuntimeError('Dismissing feedback moved the public page anchor')
 if statusbarbounds()!=bar:raise RuntimeError('Dismissing feedback changed the bottom status bar geometry')
 report.setdefault('notificationLayoutObservations',[]).append({'anchor':anchor,'before':before,'whileFeedbackVisible':during,'afterDismissal':after,'fixedStatusbarBounds':bar})
def headingwithoutoutline(phrase):
 rows=textboxes();words=phrase.lower().split();matches=[]
 navigation=[int(a[7]) for a in rows if a[11].lower()=='settings']
 minimum_top=min(navigation)+45 if navigation else 0
 minimum_height=12 if phrase=='Send payment' else 17
 for i,a in enumerate(rows):
  segment=rows[i:i+len(words)]
  if len(segment)==len(words) and [b[11].lower().strip('.,:') for b in segment]==words and all(b[1:5]==a[1:5] for b in segment) and max(int(b[9]) for b in segment)>=minimum_height and int(a[7])>minimum_top:
   matches.append((min(int(b[6]) for b in segment),min(int(b[7]) for b in segment),max(int(b[6])+int(b[8]) for b in segment),max(int(b[7])+int(b[9]) for b in segment)))
 if not matches:raise RuntimeError('Focused page heading is unavailable: '+phrase)
 left,top,right,bottom=min(matches,key=lambda value:(value[1],value[0]));picture=Image.open(capture()).convert('RGB')
 # The former programmatic-focus rectangle used this solid green. Restrict
 # observation to the heading perimeter, excluding the navigation tab strip.
 box=(max(0,left-10),max(0,top-12),min(picture.width,right+25),min(picture.height,bottom+14))
 colors=picture.crop(box).getdata()
 if sum(color==(136,205,188) for color in colors)>20:raise RuntimeError('Page heading retained the green focus rectangle: '+phrase)
 report.setdefault('headingOutlineObservations',[]).append({'heading':phrase,'publicBounds':(left,top,right,bottom),'greenOutlinePixels':sum(color==(136,205,188) for color in colors)})

def waitprompt(prefix):
 # One in-flight overview query can finish before the serial core handles a
 # dialog action. Its production deadline is 30 seconds; no wallet retry occurs.
 w,_=desktop.wait(prefix,45);desktop.focus(w);return desktop.geometry(w)
def expectfailure(public_fragment,name,persist=False):
 # Expected failures now arrive as fixed reviewed public codes in the bottom
 # status bar. The Go notice transport is not permission to create or approve
 # a confirmation window, and no raw error/prompt summary is shown here.
 until(lambda:not phrase_ui.secret_window(desktop),20)
 def visible():return public_fragment.lower() in ' '.join(a[11] for a in textboxes(statusbarbounds())).lower()
 until(visible,20)
 if any(title!='LocalWallet' for _,title in desktop.windows()):raise RuntimeError('Information-only failure feedback opened a floating application dialog')
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
  live_log_checks.append({'mesaLoaderError':'MESA-LOADER:' in live,'ordinaryIPCStackTrace':"Error occurred in handler for 'wallet:request'" in live,'knownX11AtomDiagnostic':bool(re.search(r'\] Add (?:_NET_WM_STATE_MODAL|application/vnd\.portal\.filetransfer|application/vnd\.portal\.files) to kAtomsToCache(?:\n|$)',live))})
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
def finishimport():
 # Imported recovery limits are an actual Go-authored confirmation, rather
 # than an information-only notice. Preserve its explicit human-consent step.
 waitprompt('Imported account recovery limits');desktop.accept('Imported account recovery limits')
 until(lambda:not phrase_ui.secret_window(desktop),20)
 until(lambda:'Your wallets' in ' '.join(a[11] for a in textboxes()),45)
 if any(title!='LocalWallet' for _,title in desktop.windows()):raise RuntimeError('Phrase import left an application dialog after explicit confirmation')
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
 clickword('Copy');waitprompt('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3);return clip()
def overview():
 desktop.focus(desktop.wait('LocalWallet')[0]);scrollhome();time.sleep(.2)
 clickphrase('Main account');time.sleep(.3)
def openfirstwallet():
 overview();clickphrase('Ethereum');time.sleep(.5)
 until(lambda:'Balance' in ' '.join(a[11] for a in textboxes()))
def walletmenu(phrase):
 settingssection('Wallet');clickphrase(phrase)
def setupmenu(phrase):
 settingssection('Wallet')
 if phrase.lower() not in ' '.join(a[11] for a in textboxes()).lower():clickphrase('Other setup options')
 clickphrase(phrase)
def settingstab(button=1):
 desktop.focus(desktop.wait('LocalWallet')[0])
 # The middle-click persistence check is already at the top. Do not send
 # Home while the Journal tab has focus: tab-list keyboard navigation would
 # intentionally select its first section before this persistence assertion.
 if button==1:scrollhome();time.sleep(.2)
 rows=textboxes()
 settings=[a for a in rows if a[11].lower().strip('.,:')=='settings']
 exchange=[a for a in rows if a[11].lower().strip('.,:')=='exchange']
 if not settings or not exchange:raise RuntimeError('Permanent Settings/Exchange navigation is unavailable')
 tab=min(settings,key=lambda a:(int(a[7]),int(a[6])));previous=min(exchange,key=lambda a:(int(a[7]),int(a[6])))
 if int(tab[6])<=int(previous[6]) or abs(int(tab[7])-int(previous[7]))>12:raise RuntimeError('Settings is not immediately to the right of Exchange in the top tab row')
 # Only the public tab row must contain these labels between the two tabs.
 between=[a[11].lower().strip('.,:') for a in rows if int(previous[6])+int(previous[8])<int(a[6])<int(tab[6]) and abs(int(a[7])-int(tab[7]))<12]
 if between:raise RuntimeError('Another top-level tab appears between Exchange and Settings')
 desktop.click(int(tab[6])+int(tab[8])//2,int(tab[7])+int(tab[9])//2,button=button);time.sleep(.3)
def settingssection(section):
 settingstab();rows=textboxes()
 # The settings section tabs follow the permanent navigation. Select the first
 # exact caption below that top row, never an asset/journal field or heading.
 top=min(int(a[7]) for a in rows if a[11].lower().strip('.,:')=='settings')
 matches=[a for a in rows if a[11].lower().strip('.,:')==section.lower() and int(a[7])>top+25]
 if not matches:raise RuntimeError('Settings section unavailable: '+section)
 tab=min(matches,key=lambda a:(int(a[7]),int(a[6])))
 desktop.click(int(tab[6])+int(tab[8])//2,int(tab[7])+int(tab[9])//2);time.sleep(.3)
def publichexline(box,name):
 picture=ImageOps.invert(Image.open(capture()).crop(box).convert('L'))
 picture=picture.resize((picture.width*4,picture.height*4),Image.Resampling.LANCZOS)
 path=reports/('packaged-'+name+'-public-hex.png');picture.save(path)
 value=subprocess.check_output(['tesseract',str(path),'stdout','--psm','7','-c','tessedit_char_whitelist=0123456789abcdefABCDEFx'],stderr=subprocess.DEVNULL,text=True)
 return ''.join(value.split())
def hexlinebounds(rows,anchor,left):
 # Page OCR can split one monospace address into several adjacent words. Use
 # every public text fragment on its exact baseline, then independently decode
 # that complete pixel rectangle with a hexadecimal alphabet. No characters are
 # substituted and the expected vector never determines the observed bytes.
 baseline=int(anchor[7])+int(anchor[9])//2
 fragments=sorted([a for a in rows if int(a[6])>=left and int(a[9])<=max(14,int(anchor[9]))+2 and abs(int(a[7])+int(a[9])//2-baseline)<=3],key=lambda a:int(a[6]))
 parts=[]
 for fragment in fragments:
  if parts and int(fragment[6])-(int(parts[-1][6])+int(parts[-1][8]))>10:break
  parts.append(fragment)
 if not parts:raise RuntimeError('Public hexadecimal address line is unavailable')
 return (min(int(a[6]) for a in parts)-3,min(int(a[7]) for a in parts)-4,max(int(a[6])+int(a[8]) for a in parts)+3,max(int(a[7])+int(a[9]) for a in parts)+4)
def tableaddress(network,expected):
 # Locate a complete address from its actual public line. An ellipsis, clipped
 # suffix, wrapped line or renderer substitution cannot satisfy exact equality
 # against the independent reference vector.
 receive_left=None
 for attempt in range(10):
  rows=textboxes();labels=[a for a in rows if a[11].lower()==network.lower() and int(a[6])<400]
  headers=[int(a[6]) for a in rows if a[11].lower()=='receive']
  if headers:receive_left=min(headers)
  if receive_left is None:raise RuntimeError('Table receive-address column header is unavailable')
  for label in labels:
   middle=int(label[7])+int(label[9])//2
   addresses=[a for a in rows if len(a[11])>=20 and int(a[6])>400 and abs(int(a[7])+int(a[9])//2-middle)<22]
   for address in addresses:
    box=hexlinebounds(rows,address,receive_left-5)
    observed=publichexline(box,network.lower()+'-table-address')
    if observed.lower()==expected.lower():
     report.setdefault('tableAddressObservations',[]).append({'network':network,'completeSingleLineAddress':observed,'publicBounds':box})
     return box
  desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('Complete single-line '+network+' table address is clipped or differs from the independent vector')
def copyfirstrow():
 overview();box=tableaddress('Ethereum','0x9858EfFD232B4033E47d90003D41EC34EcaEda94')
 # The copy icon follows the address's intrinsic width; it is deliberately no
 # longer positioned at the outer window edge. Click its actual adjacent box.
 desktop.click(box[2]+22,(box[1]+box[3])//2)
 waitprompt('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3);return clip()
def overviewbalance():
 overview();rows=textboxes()
 headers=[a for a in rows if a[11].lower()=='network']
 if not headers:raise RuntimeError('Wallet table network header missing')
 header=max(headers,key=lambda a:int(a[7]));hy=int(header[7])
 native=[a for a in rows if a[11].lower()=='native' and abs(int(a[7])-hy)<25]
 receive=[a for a in rows if a[11].lower()=='receive' and abs(int(a[7])-hy)<25]
 if not native or not receive:raise RuntimeError('Wallet overview does not expose its native balance column')
 first=[a for a in rows if a[11].lower()=='ethereum' and int(a[7])>hy]
 following=[a for a in rows if a[11].lower()=='hyperevm' and int(a[7])>hy]
 if not first or not following:raise RuntimeError('First two network wallet rows missing')
 ey=int(first[0][7]);ny=int(following[0][7]);bottom=(ey+ny)//2
 cells=[a for a in rows if int(native[0][6])-5<=int(a[6])<int(receive[0][6])-5 and hy+20<int(a[7])<bottom]
 words=' '.join(a[11] for a in cells)
 # A standalone zero can be misread by document OCR as punctuation. Decode
 # the complete actual cell at a larger scale, without a character whitelist,
 # so units, provenance prose or other nonnumeric text would still fail.
 middle=ey+int(first[0][9])//2
 picture=ImageOps.invert(Image.open(capture()).crop((int(native[0][6])-5,middle-18,int(receive[0][6])-8,middle+18)).convert('L'))
 picture=picture.resize((picture.width*4,picture.height*4),Image.Resampling.LANCZOS)
 number_path=reports/'packaged-native-balance-public-cell.png';picture.save(number_path)
 words=' '.join(subprocess.check_output(['tesseract',str(number_path),'stdout','--psm','7'],stderr=subprocess.DEVNULL,text=True).split())
 if re.fullmatch(r'[0-9]+(?:\.[0-9]+)?',words):
  report['nativeBalanceObservation']={'status':'PASS','scope':'Installed Ethereum native balance is only an exact whole-asset decimal value; provider observation is hidden until hover','publicText':words}
  a=cells[0];mod.T.XTestFakeMotionEvent(desktop.d,-1,int(a[6])+max(2,int(a[8])//2),int(a[7])+int(a[9])//2,0);mod.X.XFlush(desktop.d)
  until(lambda:'provider-observed' in ' '.join(a[11] for a in textboxes()).lower(),5)
  report['nativeBalanceObservation']['hoverMetadata']='PASS: provider-observed appears only after a real pointer hover'
  mod.T.XTestFakeMotionEvent(desktop.d,-1,10,10,0);mod.X.XFlush(desktop.d);time.sleep(.3)
  if 'provider-observed' in ' '.join(a[11] for a in textboxes()).lower():raise RuntimeError('Provider observation remained in ordinary overview text after pointer exit')
 elif any(word in words.lower() for word in ['unknown','unavailable','loading']):report['nativeBalanceObservation']={'status':'BLOCKED','scope':'Live Ethereum amount not available during this observation; UI retains an explicit unknown state','publicText':words,'hoverMetadata':'NOT_RUN: no verified live amount'}
 else:raise RuntimeError('First wallet row lacks a numeric-only native amount or explicit unavailable state: '+words)
 desktop.screenshot(reports/'packaged-dialog-balance-table.png')
 passed('Overview table displays numeric-only native balances or explicit unknown; live observation metadata appears only on hover when available')
 overview();tableaddress('NEAR','5510e2b44cae6eb807e3e0e45d579dda058c274abcba15e5cb84636f5d1ee412')
 passed('Long NEAR table address remains complete on one line and matches the independent public reference vector')
def decredconnection():
 overview();dismissnotification();rows=textboxes()
 native=[a for a in rows if a[11].lower()=='native']
 receive=[a for a in rows if a[11].lower()=='receive']
 if not native or not receive:raise RuntimeError('Balance-column coordinates unavailable for Decred guidance')
 left=min(int(a[6]) for a in native)-5;right=min(int(a[6]) for a in receive)-5
 # Locate the actual table row by its network label after scrolling. The
 # observation rectangle covers only its native-balance column, using the
 # independently visible column headings rather than renderer internals.
 for attempt in range(10):
  rows=textboxes();matches=[a for a in rows if a[11].lower()=='decred' and int(a[6])<400]
  if matches:
   label=matches[0];middle=int(label[7])+int(label[9])//2
   cell=' '.join(a[11] for a in rows if left<=int(a[6])<right and middle-65<=int(a[7])<middle+65).lower()
   if 'connection required' in cell:break
  desktop.key('Page_Down');time.sleep(.25)
 else:raise RuntimeError('Unconfigured Decred row lacks its connection-required guidance')
 if 'rpc query failed' in cell or re.search(r'\b0(?:\.0+)? dcr\b',cell):raise RuntimeError('Unconfigured Decred row reports an RPC failure or verified zero')
 connection=[a for a in rows if a[11].lower()=='connection' and left<=int(a[6])<right and middle-25<=int(a[7])<middle+25]
 if not connection:raise RuntimeError('Compact Decred connection guidance has no visible hover target')
 target=connection[0];mod.T.XTestFakeMotionEvent(desktop.d,-1,int(target[6])+int(target[8])//2,int(target[7])+int(target[9])//2,0);mod.X.XFlush(desktop.d)
 until(lambda:all(fragment in ' '.join(a[11] for a in textboxes()).lower() for fragment in ('authenticated tls','dcrd node','balance unknown')),5)
 report['decredConnectionObservations']={'overview':{'status':'PASS','publicBalanceText':cell,'hoverMetadata':'Authenticated TLS dcrd node and unknown balance guidance independently observed on actual hover'},'configuredNodeBalance':'NOT_RUN: no authenticated trusted-TLS Decred mainnet node supplied'}
 mod.T.XTestFakeMotionEvent(desktop.d,-1,10,10,0);mod.X.XFlush(desktop.d);time.sleep(.3)
 desktop.screenshot(reports/'packaged-decred-table-guidance.png')
 passed('Unconfigured Decred table row shows connection required and explains authenticated TLS node setup on hover, leaving balance unknown')
 desktop.click(int(label[6])+int(label[8])//2,int(label[7])+int(label[9])//2);time.sleep(.5)
 scrollhome();time.sleep(.2);words=' '.join(a[11] for a in textboxes()).lower()
 if 'connection required' not in words:raise RuntimeError('Opened unconfigured Decred wallet lost its compact balance/connection guidance')
 if 'rpc query failed' in words or 'no balance has been verified' in words or re.search(r'\b0(?:\.0+)? dcr\b',words):raise RuntimeError('Opened unconfigured Decred wallet reports an RPC failure or verified zero')
 if 'configure trusted rpc' not in words:raise RuntimeError('Decred wallet has no actionable RPC configuration control')
 for attempt in range(4):
  if all(fragment in words for fragment in ('authenticated tls rpc','certificate trusted','rpc username/password','block scan budget')):break
  desktop.key('Page_Down');time.sleep(.2);words=' '.join(a[11] for a in textboxes()).lower()
 else:raise RuntimeError('Decred wallet lacks certificate, credentials and incomplete-scan guidance')
 report['decredConnectionObservations']['wallet']={'status':'PASS','scope':'Compact balance connection guidance visible; Configure trusted RPC and certificate/credential/discovery requirements verified from installed public pixels'}
 desktop.screenshot(reports/'packaged-decred-wallet-guidance.png')
 passed('Opened Decred wallet retains connection-required guidance and explains certificate, credentials and scan requirements')
 overview()
def publicfillrectangles(picture,region,colors):
 left,top,right,bottom=region;columns={}
 for xx in range(left,right):
  ys=[yy for yy in range(top,bottom) if picture.getpixel((xx,yy)) in colors]
  if len(ys)>=15:columns[xx]=ys
 groups=[]
 for xx in columns:
  if not groups or xx!=groups[-1][-1]+1:groups.append([])
  groups[-1].append(xx)
 return [(group[0],min(min(columns[xx]) for xx in group),group[-1]+1,max(max(columns[xx]) for xx in group)+1) for group in groups if len(group)>=20]
def publiccontroltext(picture,box,name):
 left,top,right,bottom=box
 crop=ImageOps.invert(picture.crop((left+4,top+4,right-4,bottom-4)).convert('L'))
 crop=crop.resize((crop.width*4,crop.height*4),Image.Resampling.LANCZOS)
 path=reports/('packaged-'+name+'-control-text.png');crop.save(path)
 return ' '.join(subprocess.check_output(['tesseract',str(path),'stdout','--psm','7'],stderr=subprocess.DEVNULL,text=True).split())
def selectwalletasset(symbol):
 scrollhome();rows=textboxes();_,_,_,panel_bottom=publicphrasebounds('Balance')
 # Locate the actual public button fills inside the independently labelled
 # asset strip. Cropping within their border avoids document OCR discarding
 # their labels, while both exact labels are independently decoded first.
 labels=[]
 for i,a in enumerate(rows[:-1]):
  b=rows[i+1]
  if a[11].lower()=='receive' and b[11].lower().strip('.,:')=='address' and a[1:5]==b[1:5]:labels.append(int(a[7]))
 if not labels:raise RuntimeError('Wallet asset strip receive-field boundary is unavailable')
 field_top=max(labels);picture=Image.open(capture()).convert('RGB')
 x,y,w,h=desktop.geometry(desktop.wait('LocalWallet')[0])
 buttons=publicfillrectangles(picture,(x,panel_bottom+3,x+w,field_top-4),{(54,87,131),(36,54,80)})
 if len(buttons)!=2:raise RuntimeError('The labelled Ethereum asset strip does not expose exactly two public buttons')
 observed=[publiccontroltext(picture,box,'wallet-asset-'+str(index)) for index,box in enumerate(buttons)]
 if observed!=['ETH','USDT']:raise RuntimeError('Public wallet asset buttons do not independently read ETH and USDT')
 left,top,right,bottom=buttons[0];desktop.click((left+right)//2,(top+bottom)//2)
 if symbol=='USDT':desktop.key('Right')
 elif symbol!='ETH':raise RuntimeError('Unsupported asset selection in public fixture drill')
 time.sleep(.3)
def walletpresentation(expected):
 openfirstwallet();dismissnotification();headingwithoutoutline('Ethereum')
 rows=textboxes();text=' '.join(a[11] for a in rows)
 if 'provider-observed' in text.lower():raise RuntimeError('Provider observation metadata is visible in the wallet page')
 if re.search(r'\bAsset\b',text):raise RuntimeError('Wallet page still presents a redundant asset dropdown label')
 labels=[]
 for i,a in enumerate(rows[:-1]):
  b=rows[i+1]
  if a[11].lower()=='receive' and b[11].lower().strip('.,:')=='address' and a[1:5]==b[1:5]:labels.append((int(a[6]),max(int(a[7])+int(a[9]),int(b[7])+int(b[9]))))
 if not labels:raise RuntimeError('Persistent wallet Receive address label is unavailable')
 label_left,label_bottom=max(labels,key=lambda value:value[1])
 matches=[a for a in rows if len(a[11])>=20 and int(a[6])>=label_left-5 and label_bottom<int(a[7])<label_bottom+60]
 if not matches:raise RuntimeError('Wallet page does not display a persistent receive address below its label')
 a=min(matches,key=lambda value:(int(value[7]),int(value[6])));box=hexlinebounds(rows,a,label_left-5)
 if publichexline(box,'wallet-receive-address').lower()!=expected.lower():raise RuntimeError('Wallet page receive address is clipped or differs from the verified independent address')
 desktop.click(box[2]+22,(box[1]+box[3])//2);waitprompt('Verify receive address')
 if promptcopy()!=expected:raise RuntimeError('Persistent wallet address copy icon did not copy the Go-verified public address')
 desktop.accept('Verify receive address');time.sleep(.3)
 passed('Wallet page always shows the complete receive address and working copy icon without visible provider-observed metadata or an asset dropdown')
 if selected_profile=='mainnet':
  # Ethereum has two independently identified assets. Real public buttons retain
  # access to USDT without a dropdown or changing the receive account.
  selectwalletasset('USDT')
  rows=textboxes();tabtop=min(int(a[7]) for a in rows if a[11].lower()=='settings')
  x,y,w,h=desktop.geometry(desktop.wait('LocalWallet')[0]);picture=Image.open(capture()).convert('RGB')
  navigation=publicfillrectangles(picture,(x,tabtop-18,x+w,tabtop+28),{(54,87,131)})
  if len(navigation)!=1:raise RuntimeError('Selected wallet navigation tab cannot be isolated from its public fill')
  tabwords=publiccontroltext(picture,navigation[0],'wallet-asset-navigation')
  if 'Ethereum' not in tabwords or 'USDT' not in tabwords:raise RuntimeError('Wallet tab name did not follow its selected USDT asset')
  clickprimary('Send');time.sleep(.3)
  for attempt in range(5):
   if re.search(r'\b1\s*=\s*1\s*USDT\b',' '.join(a[11] for a in textboxes())):break
   scrollpage(5,3)
  else:raise RuntimeError('Send form lacks visible 1 = 1 USDT whole-asset guidance')
  clickphrase('Close send form');selectwalletasset('ETH')
  passed('Real asset buttons select USDT and restore ETH, update the wallet tab and explain whole-asset amount units')
 scrollhome();shutil.copyfile(capture(),reports/'wallet-status-wallet.png');desktop.screenshot(reports/'packaged-wallet-persistent-address.png')
def sendpresentation():
 headingwithoutoutline('Send payment')
 # Read the whole public form across scroll positions; no request is prepared,
 # signed or submitted merely to establish labels and amount semantics.
 seen=set()
 for attempt in range(4):
  rows=textboxes();seen.update(a[11].lower().strip('.,:') for a in rows)
  words=' '.join(a[11] for a in rows)
  if re.search(r'\bAmount\b',words) and re.search(r'\b1\s*=\s*1\s*ETH\b',words):break
  desktop.key('Page_Down');time.sleep(.2)
 else:raise RuntimeError('Send form does not label Amount with explicit 1 = 1 ETH whole-asset guidance')
 shutil.copyfile(capture(),reports/'wallet-status-send.png')
 for attempt in range(3):
  rows=textboxes();words=' '.join(a[11] for a in rows).lower()
  if 'scan local qr image' in words or 'exact decimal amount' in words:raise RuntimeError('Send retains the removed QR scan control or old amount label')
  desktop.key('Page_Down');time.sleep(.15)
 passed('Send uses Amount and explicit 1 = 1 ETH guidance, with no Scan local QR image control')
def settingsjournal():
 openfirstwallet()
 # The journal must not remain on any scroll position of the wallet page.
 for attempt in range(4):
  if 'Local payment journal' in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Payment journal remained in the wallet page')
  desktop.key('Page_Down');time.sleep(.15)
 settingssection('Wallet')
 if 'Local payment journal' in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Payment journal remained in Settings Wallet')
 settingssection('Journal')
 headingwithoutoutline('Settings')
 words=' '.join(a[11] for a in textboxes())
 if 'Local payment journal' not in words or 'Refresh journal' not in words or 'Ethereum' not in words or 'Wallet' not in words:raise RuntimeError('Settings Journal tab lacks its explicit wallet context or controls')
 if any(title!='LocalWallet' for _,title in desktop.windows()):raise RuntimeError('Settings navigation opened another application window')
 desktop.screenshot(reports/'packaged-settings-journal.png')
 desktop.key('Escape');time.sleep(.3)
 if 'Local payment journal' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Escape dismissed the main Settings page')
 settingstab(button=2)
 if 'Local payment journal' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Middle click removed the permanent Settings tab')
 overview()
 if 'Local payment journal' in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Settings Journal remained visible in the account page')
 passed('Settings is a permanent main-window tab immediately after Exchange; Journal owns payment history and survives Escape/middle-click')
def exchangetab(button=1):
 # Select the topmost public Exchange label, which is the permanent tab rather
 # than the page heading. No DOM, accessibility internals or debugger is used.
 desktop.focus(desktop.wait('LocalWallet')[0]);scrollhome();time.sleep(.2)
 rows=[a for a in textboxes() if a[11].lower().strip('.,:')=='exchange']
 if not rows:raise RuntimeError('Installed Exchange tab is unavailable')
 a=min(rows,key=lambda value:(int(value[7]),int(value[6])))
 desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2,button=button);time.sleep(.3)
def exchangedisabledcontrols():
 # Disabled primary controls have the production CSS opacity of .4. Inspect
 # their public label pixels and blue fill independently, then click each.
 # This assertion would fail for the normal enabled primary-button fill.
 for attempt in range(12):
  if phrase_ui.secret_window(desktop):raise RuntimeError('Unexpected credential window while checking disconnected Exchange')
  original=Image.open(capture()).convert('RGB')
  picture=ImageOps.invert(original.convert('L'))
  picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).point(lambda value:0 if value<180 else 255)
  path=reports/'packaged-exchange-disabled-ocr.png';picture.save(path)
  raw=subprocess.check_output(['tesseract',str(path),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
  controls={}
  for line in raw.splitlines()[1:]:
   a=line.split('\t')
   if len(a)!=12 or a[11].lower() not in ('deposit','swap','withdraw'):continue
   x=int(a[6])//2;y=(int(a[7])+int(a[9])//2)//2
   color=original.getpixel((max(0,x-8),y))
   if 30<=color[0]<=46 and 43<=color[1]<=65 and 70<=color[2]<=105:
    controls[a[11].lower()]=(x+int(a[8])//4,y)
  if len(controls)==3:
   desktop.screenshot(reports/'packaged-exchange-disconnected.png')
   for name,(x,y) in controls.items():
    desktop.click(x,y);time.sleep(.2)
    if phrase_ui.secret_window(desktop):raise RuntimeError('Disconnected Exchange '+name+' opened an authorization dialog')
   scrollend();time.sleep(.3)
   if 'No exchange action requested' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Disconnected Exchange controls changed operation state')
   return
  desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('Disabled Deposit, Swap and Withdraw controls could not be verified from public pixels')
def exchangeassetidentity(expected):
 if selected_profile!='mainnet':return
 scrollhome();time.sleep(.2)
 clickphrase('Wallet asset')
 # Ethereum's actual registry lists ETH then its pinned USDT token. Native
 # keyboard selection exercises the real select control without a provider
 # query, credentials, DOM inspection or synthetic renderer event injection.
 desktop.key('Home');desktop.key('Down');desktop.key('Escape');time.sleep(.3)
 dismissnotification()
 for attempt in range(5):
  rows=textboxes();text=' '.join(a[11] for a in rows)
  if 'your wallet address' in text.lower() and 'token contract' in text.lower():break
  scrollpage(5,3)
 else:raise RuntimeError('Exchange does not distinguish the owned receive address from token contract metadata')
 def publichex(label,below):
  # Whole-document OCR can read hexadecimal 0 as O at this public 12px size.
  # Locate its line independently, then decode only its actual pixels using
  # a hex alphabet; never substitute characters to force the expected value.
  left,top,right,bottom=publicphrasebounds(label)
  if below:
   matches=[a for a in rows if bottom<int(a[7])<bottom+40 and left-5<=int(a[6])<left+700]
   if not matches:raise RuntimeError('Public address line is unavailable')
   first=min(int(a[7]) for a in matches);matches=[a for a in matches if abs(int(a[7])-first)<4]
  else:
   matches=[a for a in rows if re.match(r'[0o]x',a[11].lower()) and len(a[11])>=20 and abs(int(a[7])-top)<12 and int(a[6])>=right]
   if len(matches)!=1:raise RuntimeError('Public contract line is ambiguous')
  box=(min(int(a[6]) for a in matches)-5,min(int(a[7]) for a in matches)-6,max(int(a[6])+int(a[8]) for a in matches)+5,max(int(a[7])+int(a[9]) for a in matches)+6)
  picture=ImageOps.invert(Image.open(capture()).crop(box).convert('L'))
  picture=picture.resize((picture.width*4,picture.height*4),Image.Resampling.LANCZOS)
  path=reports/('packaged-exchange-'+('address' if below else 'contract')+'-ocr.png');picture.save(path)
  value=subprocess.check_output(['tesseract',str(path),'stdout','--psm','7','-c','tessedit_char_whitelist=0123456789abcdefABCDEFx'],stderr=subprocess.DEVNULL,text=True)
  return ''.join(value.split())
 address=publichex('Your wallet address',True)
 contract='0xdAC17F958D2ee523a2206206994597C13D831ec7'
 if address.lower()!=expected.lower():raise RuntimeError('Public-pixel OCR could not verify the exact independently known Exchange receive address')
 if publichex('Token contract',False).lower()!=contract.lower():raise RuntimeError('Public-pixel OCR could not verify the exact pinned USDT token contract')
 # The selected option itself must describe the asset, while contract metadata
 # appears separately. A raw internal asset ID is not a receiving address.
 if 'ethereum-mainnet:erc20:' in text.lower():raise RuntimeError('Exchange exposes its raw token asset ID as an asset name')
 desktop.screenshot(reports/'packaged-exchange-usdt-identity.png')
 report['exchangeAssetIdentity']={'status':'PASS','scope':'Public screenshot OCR contains the complete independently verified owned Ethereum receive address and separate exact canonical USDT contract after native keyboard selection; no live Bybit query','walletAddress':expected,'tokenContract':contract}
 passed('Installed Exchange USDT selection separates its exact token contract from the independently verified owned Ethereum receive address')
def exchangedisconnected(profile):
 exchangetab();headingwithoutoutline('Exchange')
 until(lambda:'bybit is disconnected' in ' '.join(a[11] for a in textboxes()).lower(),45)
 exchangeassetidentity('0x9858EfFD232B4033E47d90003D41EC34EcaEda94')
 overview();exchangetab();exchangetab(button=2)
 if 'bybit is disconnected' not in ' '.join(a[11] for a in textboxes()).lower():raise RuntimeError('Middle click removed the permanent Exchange tab')
 passed('Installed Exchange tab remains available after account navigation and middle-click')
 exchangedisabledcontrols()
 passed('Disconnected installed Exchange has visibly disabled Deposit, Swap and Withdraw buttons that perform no action')
 if selected_profile!='mainnet':return
 words=' '.join(a[11] for a in textboxes()).lower()
 if 'connect bybit api key' in words or 'bybit api region' in words:raise RuntimeError('Bybit connection setup remains on the trading page')
 scrollhome();time.sleep(.2)
 x,y,w,h=desktop.geometry(desktop.wait('LocalWallet')[0])
 navtop=min(int(a[7]) for a in textboxes() if a[11].lower().strip('.,:')=='exchange')
 # The adjacent permanent tabs themselves read "Exchange Settings". Scope the
 # shortcut to page content so it cannot be mistaken for those two controls.
 clickphrase('Exchange settings',region=(x,navtop+40,x+w,y+h));time.sleep(.3)
 until(lambda:'bybit api region' in ' '.join(a[11] for a in textboxes()).lower(),20)
 if 'bybit connection' not in ' '.join(a[11] for a in textboxes()).lower():raise RuntimeError('Settings Exchange tab did not display Bybit setup')
 if 'local payment journal' in ' '.join(a[11] for a in textboxes()).lower():raise RuntimeError('Journal remained on the Exchange settings page')
 connection_anchor=publicphrasebounds('Bybit connection')
 before=hashlib.sha256((profile/vault_relative).read_bytes()).hexdigest()
 # Cancel the password step first; then authenticate using only the existing
 # synthetic app password and cancel empty API input. Never enter an API key,
 # capture private dialog pixels, approve custody or call a private Bybit API.
 for enter_password in (False,True):
  desktop.focus(desktop.wait('LocalWallet')[0]);scrollhome();time.sleep(.2)
  desktop.screenshot(reports/'packaged-exchange-top.png')
  clickword('Connect');waitprompt('Reauthenticate Bybit connection')
  if not phrase_ui.secret_window(desktop):raise RuntimeError('Bybit password dialog is outside the protected credential-window classification')
  if enter_password:
   desktop.inputs('Reauthenticate Bybit connection',[password]);desktop.accept('Reauthenticate Bybit connection');waitprompt('Connect Bybit')
   if not phrase_ui.secret_window(desktop):raise RuntimeError('Bybit API dialog is outside the protected credential-window classification')
  desktop.key('Escape')
  until(lambda:not phrase_ui.secret_window(desktop),20)
  if any(title!='LocalWallet' for _,title in desktop.windows()):raise RuntimeError('Cancelled Bybit connection left a floating information dialog')
  until(lambda:'bybit is disconnected' in ' '.join(a[11] for a in textboxes()).lower(),20)
  until(lambda:'request cancelled' in ' '.join(a[11] for a in textboxes()).lower(),20)
  notificationlayout('Bybit connection',connection_anchor)
 if hashlib.sha256((profile/vault_relative).read_bytes()).hexdigest()!=before:raise RuntimeError('Cancelled Bybit connection changed encrypted wallet state')
 report['exchangeConnectionDrill']={'status':'PASS','scope':'Real isolated app-password and empty masked API-input dialogs cancelled through XTest; no API credentials entered, custody approved or live exchange acceptance claimed'}
 passed('Installed Settings Exchange owns Bybit region and Connect; isolated credential cancellation preserves the disconnected vault')
 passed('Installed cancellation feedback stays in the fixed bottom status bar without moving the Exchange settings heading and can be dismissed')
 desktop.key('Escape');time.sleep(.3)
 if 'bybit connection' not in ' '.join(a[11] for a in textboxes()).lower():raise RuntimeError('Escape dismissed main-page Exchange settings')
 overview()
def filechoice(prefix,path):
 waitprompt(prefix);desktop.fit(desktop.wait(prefix)[0]);desktop.key('l',ctrl=True);desktop.key('a',ctrl=True);desktop.type(str(path))
 # Electron resolves a typed absolute path asynchronously. Wait between accepting
 # the location and the final file selection instead of racing that resolution.
 desktop.key('Return');time.sleep(1.5)
 for attempt in range(3):
  if not any(t.startswith(prefix) for _,t in desktop.windows()):return
  desktop.focus(desktop.wait(prefix)[0]);desktop.accept(prefix);time.sleep(1.5)
 if any(t.startswith(prefix) for _,t in desktop.windows()):
  desktop.screenshot(reports/'packaged-file-selection.png')
  raise RuntimeError('Electron file selection did not complete: '+prefix)
def promptcopy():
 x,y,w,h=waitprompt('Verify receive address')
 # Only this public QR/address dialog is cropped for OCR. Secret input dialogs
 # are excluded; button coordinates come from the uninstrumented dialog UI.
 picture=ImageOps.invert(Image.open(capture()).crop((x,y,x+w,y+h)).convert('L'))
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS)
 path=reports/'packaged-dialog-receive-controls.png';picture.save(path)
 raw=subprocess.check_output(['tesseract',str(path),'stdout','tsv'],stderr=subprocess.DEVNULL,text=True)
 rows=[line.split('\t') for line in raw.splitlines()[1:] if len(line.split('\t'))==12 and line.split('\t')[11].strip()]
 for i,a in enumerate(rows[:-1]):
  if a[11]=='Copy' and rows[i+1][11].lower()=='address':
   desktop.click(x+(int(a[6])+int(a[8])//2)//2,y+(int(a[7])+int(a[9])//2)//2);time.sleep(.3);return clip()
 raise RuntimeError('Electron Receive popup Copy address button unavailable')
def decodereceive(expected):
 # Decode the pixels actually displayed by Electron, independently of the original
 # generated PNG and clipboard. This helper has no access to app internals.
 x,y,w,h=waitprompt('Verify receive address')
 path=reports/'packaged-dialog-receive-qr.png'
 Image.open(capture()).crop((x,y,x+w,y+h)).save(path)
 oracle='''package main
import("image";_ "image/png";"os";"fmt";"github.com/makiuchi-d/gozxing";"github.com/makiuchi-d/gozxing/qrcode")
func main(){f,e:=os.Open(os.Args[1]);if e!=nil{panic(e)};defer f.Close();im,_,e:=image.Decode(f);if e!=nil{panic(e)};b,e:=gozxing.NewBinaryBitmapFromImage(im);if e!=nil{panic(e)};r,e:=qrcode.NewQRCodeReader().Decode(b,nil);if e!=nil{panic(e)};fmt.Print(r.GetText())}
'''
 with tempfile.TemporaryDirectory(prefix='localwallet-public-qr-oracle-') as temporary:
  source=pathlib.Path(temporary)/'main.go';source.write_text(oracle)
  decoded=subprocess.check_output([str(pathlib.Path(decoder_environment['GOROOT'])/'bin/go'),'run',str(source),str(path)],cwd=root,env={**os.environ,**decoder_environment,'GOTOOLCHAIN':'local'},text=True,timeout=60)
 if decoded!=expected:raise RuntimeError('Displayed Receive QR pixels differ from the independent expected address')
try:
 server=shutil.which('Xvfb') or str(root/'.tools/xvfb/root/usr/bin/Xvfb')
 display=next((f':{n}' for n in range(180,190) if not pathlib.Path(f'/tmp/.X11-unix/X{n}').exists() and not pathlib.Path(f'/tmp/.X{n}-lock').exists()),None)
 if not display:raise RuntimeError('No unused private display; refusing user display')
 env={**os.environ,'DISPLAY':display};env.pop('WAYLAND_DISPLAY',None);env.pop('XDG_RUNTIME_DIR',None);env.pop('DBUS_SESSION_BUS_ADDRESS',None)
 if '.tools/' in server:env['LD_LIBRARY_PATH']=str(root/'.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
 xvlog=tempfile.TemporaryFile();xv=subprocess.Popen([server,display,'-screen','0','1280x1024x24','-nolisten','tcp'],env=env,stdout=xvlog,stderr=xvlog);owned.append(xv)
 until(lambda:pathlib.Path('/tmp/.X11-unix/X'+display[1:]).exists(),10)
 desktop=mod.Desktop(display.encode());x11_error_handler_refs=install_owned_window_query_handler(desktop,report,reports/'wallet-status-late-probe.json');os.environ.update(DISPLAY=display,GDK_BACKEND='x11')
 with tempfile.TemporaryDirectory(prefix='localwallet-package-fixture-') as temporary:
  folder=pathlib.Path(temporary);profile=folder/'original'
  home=folder/'home';home.mkdir(mode=0o700);env['HOME']=str(home);os.environ['HOME']=str(home)
  for key in ['WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:os.environ.pop(key,None)
  report['scope']='Targeted public late-UI helper diagnostic only: fresh public fixture import, persistent receive copy, ETH/USDT buttons/navigation, whole-asset amounts and Send form. This is not the complete packaged recovery campaign.'
  report['notRun'].append('Earlier onboarding/password errors, wrong-word confirmation, generated phrase recovery, DCR/settings/exchange cancellation, restart/profile/canary cases NOT_RUN in this targeted diagnostic')
  report['canonicalProducerSHA256']=hashlib.sha256((root/'scripts/packaged-recovery.py').read_bytes()).hexdigest()
  launch(profile,env);clickphrase('Restore from recovery phrase');waitprompt('Create local wallet');desktop.inputs('Create local wallet',[password,password,phrase,'']);desktop.accept('Create local wallet');finishimport()
  walletpresentation('0x9858EfFD232B4033E47d90003D41EC34EcaEda94');openfirstwallet();clickprimary('Send');time.sleep(.3);sendpresentation()
  if hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()!=report['producerSHA256']:raise RuntimeError('Targeted producer changed during execution')
  report['status']='PASS';report['sourceStable']=True

except Exception as error:
 reason=str(error) if isinstance(error,RuntimeError) else 'Packaged recovery or dialog automation failed; no secret diagnostic retained'
 report.update(status='FAIL',reason=reason);print(reason,flush=True)
 if app is not None:
  report['directChildProcesses']=subprocess.run(['ps','-o','pid,ppid,pgid,stat,comm','--ppid',str(app.pid)],text=True,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL).stdout
 if desktop and not phrase_ui.secret_window(desktop):
  desktop.screenshot(reports/'packaged-failure.png');print(desktop.windows(),flush=True)
finally:
 stopapp()
 if x11_error_handler_refs is not None:x11_error_handler_refs[2]()
 for p in owned:
  p.terminate()
  try:p.wait(timeout=5)
  except subprocess.TimeoutExpired:p.kill();p.wait()
 for l in logs:l.close()
 (reports/'wallet-status-late-probe.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(report['status']!='PASS')
