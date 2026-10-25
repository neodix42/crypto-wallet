#!/usr/bin/env python3
"""Exercise the uninstrumented packaged app on an owned private X11 display.
Synthetic public test credentials stay in this test process and dialog key events.
No clipboard secret transport, debug port, or bypass is used. The overview and
opened wallets query configured public RPCs with disposable fixture addresses.
This is not the clean Ubuntu/Wayland acceptance matrix.
"""
import argparse, base64, ctypes as C, hashlib, importlib.util, json, os, pathlib, re, shutil, signal, subprocess, tempfile, time
from PIL import Image, ImageOps
from install import verify_binaries
root=pathlib.Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--profile',choices=['mainnet','testnet'],default='mainnet')
selected_profile=parser.parse_args().profile
vault_relative=pathlib.Path('localwallet')/selected_profile/'vault.lwv'
spec=importlib.util.spec_from_file_location('test_x11',root/'tests/e2e/x11.py');mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
reports=root/'reports';reports.mkdir(exist_ok=True)
phrase_spec=importlib.util.spec_from_file_location('test_phrase',root/'tests/e2e/phrase.py');phrase_ui=importlib.util.module_from_spec(phrase_spec);phrase_spec.loader.exec_module(phrase_ui)
report={'status':'NOT_RUN','scope':'Uninstrumented portable X11 password-to-dialog-phrase setup, recorded-word confirmation, isolated fresh-profile phrase restore, main-page Settings Wallet/Exchange/Journal navigation, floating feedback without moving its public page anchor, native balances, receive QR/copy, tab closing, disconnected Exchange controls and isolated Connect cancellation; disposable generated and public fixture roots only','steps':[], 'notRun':['clean Ubuntu24.04 desktop','Wayland full wallet workflows','OS suspend/session lock','send from installed package','upgrade/uninstall recovery matrix','live Bybit authentication, deposits, spot fills and withdrawals']}
report['environment']=selected_profile
report['producerSHA256']=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
report.update(status='RUNNING',sourceStable=False)
(reports/'packaged-recovery.json').write_text(json.dumps(report,indent=2)+'\n')
# Keep the pinned test decoder's existing build cache when the wallet itself
# receives a fresh HOME below. These paths are never passed to the wallet.
decoder_environment=json.loads(subprocess.check_output([str(root/'scripts/with-go.sh'),'go','env','-json','GOROOT','GOPATH','GOMODCACHE','GOCACHE'],cwd=root,text=True))
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
def clickphrase(phrase,button=1,region=None):
 words=phrase.lower().split()
 for attempt in range(16):
  rows=textboxes(region)
  for i,a in enumerate(rows):
   segment=rows[i:i+len(words)]
   if len(segment)==len(words) and [b[11].lower().strip('.,:') for b in segment]==words and all(b[1:5]==a[1:5] for b in segment):
    desktop.focus(desktop.wait('LocalWallet')[0]);desktop.click(int(a[6])+int(a[8])//2,int(a[7])+int(a[9])//2,button=button);return
  desktop.key('Home',ctrl=True) if attempt in (0,8) else desktop.key('Page_Down');time.sleep(.2)
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
def dismissnotification():
 if 'dismiss notification' in ' '.join(a[11] for a in textboxes()).lower():
  clickphrase('Dismiss notification');time.sleep(.2)
def notificationlayout(anchor,before):
 # A visible error previously inserted a block above the page. Require the
 # same public heading rectangle both while feedback is shown and after it is
 # dismissed. This measures the installed layout rather than reading CSS.
 if 'dismiss notification' not in ' '.join(a[11] for a in textboxes()).lower():raise RuntimeError('Floating feedback has no visible dismiss control')
 during=publicphrasebounds(anchor)
 if during!=before:raise RuntimeError('Feedback moved the public page anchor')
 dismissnotification();after=publicphrasebounds(anchor)
 if after!=before:raise RuntimeError('Dismissing feedback moved the public page anchor')
 report.setdefault('notificationLayoutObservations',[]).append({'anchor':anchor,'before':before,'whileFeedbackVisible':during,'afterDismissal':after})
def waitprompt(prefix):
 # One in-flight overview query can finish before the serial core handles a
 # dialog action. Its production deadline is 30 seconds; no wallet retry occurs.
 w,_=desktop.wait(prefix,45);desktop.focus(w);return desktop.geometry(w)
def expectfailure(dialog_fragment,public_fragment,name,persist=False):
 x,y,w,h=waitprompt('Wallet request failed')
 # This notice contains only fixed guidance; no credential input window remains.
 if phrase_ui.secret_window(desktop):
  raise RuntimeError('A credential window remained behind failure guidance')
 picture=ImageOps.invert(Image.open(capture()).crop((max(0,x),max(0,y),min(1280,x+w),min(1024,y+h))).convert('L'))
 picture=picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS)
 path=reports/'packaged-dialog-guidance.png';picture.save(path)
 text=subprocess.check_output(['tesseract',str(path),'stdout'],stderr=subprocess.DEVNULL,text=True)
 if dialog_fragment.lower() not in ' '.join(text.split()).lower():raise RuntimeError('Electron failure guidance is missing: '+name)
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
 desktop.focus(desktop.wait('LocalWallet')[0]);desktop.key('Home',ctrl=True);time.sleep(.2)
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
 if button==1:desktop.key('Home',ctrl=True);time.sleep(.2)
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
def copyfirstrow():
 overview()
 # Click the first table row's far-right copy control using its network label
 # and the content area's right edge. No DOM/debug injection is used.
 for attempt in range(12):
  rows=textboxes()
  headers=[int(a[7]) for a in rows if a[11].lower()=='network']
  for a in rows:
   if a[11].lower()=='ethereum' and int(a[6])<400 and headers and int(a[7])>min(headers):
    x,y,w,h=desktop.geometry(desktop.wait('LocalWallet')[0])
    desktop.click(x+w-70,int(a[7])+int(a[9])//2)
    waitprompt('Verify receive address');desktop.accept('Verify receive address');time.sleep(.3);return clip()
  desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('First public wallet table row not available')
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
 words=' '.join(a[11] for a in rows if int(native[0][6])-5<=int(a[6])<int(receive[0][6])-5 and hy+20<int(a[7])<bottom)
 if re.search(r'\b[0-9]+(?:\.[0-9]+)? ETH\b',words):report['nativeBalanceObservation']={'status':'PASS','scope':'Installed Ethereum table native-currency amount and observation metadata visible','publicText':words}
 elif any(word in words.lower() for word in ['unknown','unavailable','loading']):report['nativeBalanceObservation']={'status':'BLOCKED','scope':'Live Ethereum amount not available during this observation; UI retains an explicit unknown state','publicText':words}
 else:raise RuntimeError('First wallet row lacks native-currency amount or explicit unavailable state: '+words)
 desktop.screenshot(reports/'packaged-dialog-balance-table.png')
 passed('Overview table displays its native balance column and an exact ETH quantity or explicit unknown state')
def settingsjournal():
 openfirstwallet()
 # The journal must not remain on any scroll position of the wallet page.
 for attempt in range(4):
  if 'Local payment journal' in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Payment journal remained in the wallet page')
  desktop.key('Page_Down');time.sleep(.15)
 settingssection('Wallet')
 if 'Local payment journal' in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Payment journal remained in Settings Wallet')
 settingssection('Journal')
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
 desktop.focus(desktop.wait('LocalWallet')[0]);desktop.key('Home',ctrl=True);time.sleep(.2)
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
   desktop.key('End',ctrl=True);time.sleep(.3)
   if 'No exchange action requested' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Disconnected Exchange controls changed operation state')
   return
  desktop.key('Page_Down');time.sleep(.2)
 raise RuntimeError('Disabled Deposit, Swap and Withdraw controls could not be verified from public pixels')
def exchangeassetidentity(expected):
 if selected_profile!='mainnet':return
 desktop.key('Home',ctrl=True);time.sleep(.2)
 clickphrase('Wallet asset')
 # Ethereum's actual registry lists ETH then its pinned USDT token. Native
 # keyboard selection exercises the real select control without a provider
 # query, credentials, DOM inspection or synthetic renderer event injection.
 desktop.key('Home');desktop.key('Down');desktop.key('Escape');time.sleep(.3)
 dismissnotification()
 rows=textboxes();text=' '.join(a[11] for a in rows)
 if 'your wallet address' not in text.lower() or 'token contract' not in text.lower():raise RuntimeError('Exchange does not distinguish the owned receive address from token contract metadata')
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
 exchangetab()
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
 desktop.key('Home',ctrl=True);time.sleep(.2)
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
  desktop.focus(desktop.wait('LocalWallet')[0]);desktop.key('Home',ctrl=True);time.sleep(.2)
  desktop.screenshot(reports/'packaged-exchange-top.png')
  clickword('Connect');waitprompt('Reauthenticate Bybit connection')
  if not phrase_ui.secret_window(desktop):raise RuntimeError('Bybit password dialog is outside the protected credential-window classification')
  if enter_password:
   desktop.inputs('Reauthenticate Bybit connection',[password]);desktop.accept('Reauthenticate Bybit connection');waitprompt('Connect Bybit')
   if not phrase_ui.secret_window(desktop):raise RuntimeError('Bybit API dialog is outside the protected credential-window classification')
  desktop.key('Escape')
  until(lambda:not phrase_ui.secret_window(desktop),20)
  if any(title.startswith(('Wallet request failed','LocalWallet — Wallet request failed')) for _,title in desktop.windows()):desktop.accept('Wallet request failed')
  until(lambda:'bybit is disconnected' in ' '.join(a[11] for a in textboxes()).lower(),20)
  until(lambda:'request cancelled' in ' '.join(a[11] for a in textboxes()).lower(),20)
  notificationlayout('Bybit connection',connection_anchor)
 if hashlib.sha256((profile/vault_relative).read_bytes()).hexdigest()!=before:raise RuntimeError('Cancelled Bybit connection changed encrypted wallet state')
 report['exchangeConnectionDrill']={'status':'PASS','scope':'Real isolated app-password and empty masked API-input dialogs cancelled through XTest; no API credentials entered, custody approved or live exchange acceptance claimed'}
 passed('Installed Settings Exchange owns Bybit region and Connect; isolated credential cancellation preserves the disconnected vault')
 passed('Installed cancellation feedback floats without moving the Exchange settings heading and can be dismissed')
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
 desktop=mod.Desktop(display.encode());os.environ.update(DISPLAY=display,GDK_BACKEND='x11')
 with tempfile.TemporaryDirectory(prefix='localwallet-package-fixture-') as temporary:
  folder=pathlib.Path(temporary);profile=folder/'original'
  home=folder/'home';home.mkdir(mode=0o700);env['HOME']=str(home);os.environ['HOME']=str(home)
  for key in ['WAYLAND_DISPLAY','XDG_RUNTIME_DIR','DBUS_SESSION_BUS_ADDRESS']:os.environ.pop(key,None)
  generated=folder/'generated';launch(generated,env)
  if any(t.startswith(('Create local wallet','Unlock local wallet')) for _,t in desktop.windows()):raise RuntimeError('First launch opened a credential window without a user action')
  desktop.screenshot(reports/'packaged-first-run.png')
  onboarding_anchor=publicphrasebounds('Create your app password')
  clickline('Create app password');waitprompt('Create local wallet')
  desktop.inputs('Create local wallet',['bad','bad']);desktop.accept('Create local wallet')
  expectfailure('UTF-8 bytes','Use an app password','short-password-feedback',persist=True)
  notificationlayout('Create your app password',onboarding_anchor)
  passed('Installed error feedback remains readable across polling without moving onboarding and dismisses independently')
  if (generated/vault_relative).exists():raise RuntimeError('Short password created a vault')
  clickline('Create app password');waitprompt('Create local wallet')
  desktop.inputs('Create local wallet',[password,'different-public-password-79326']);desktop.accept('Create local wallet')
  expectfailure('do not match','passwords did not match','password-mismatch-feedback')
  clickline('Create app password');waitprompt('Create local wallet')
  desktop.inputs('Create local wallet',[password,password]);desktop.accept('Create local wallet')
  generated_phrase=phrase_ui.record(desktop,wrong=True)
  until(lambda:(generated/vault_relative).exists());time.sleep(1)
  passed('Password setup generates and encrypts a random phrase; incorrect recorded words leave setup pending')
  openfirstwallet();clickprimary('Send');time.sleep(.3)
  if 'Full recipient' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Pending Send did not open its form')
  passed('Pending recovery offers an actionable Send form without enabling payment preparation')
  clickphrase('Close send form');clickprimary('Receive');time.sleep(.3);clickphrase('Cancel');time.sleep(.3)
  if any(t.startswith('Verify receive address') for _,t in desktop.windows()):raise RuntimeError('Pending recovery opened a receive QR')
  passed('Pending Receive guide cancels without opening an unconfirmed address workflow')
  clickprimary('Receive');time.sleep(.3);clickprimary('Finish recovery phrase setup',last=True)
  waitprompt('Complete recovery setup');desktop.inputs('Complete recovery setup',[password]);desktop.accept('Complete recovery setup')
  phrase_ui.record(desktop,expected=generated_phrase);waitprompt('Verify receive address')
  generated_address=promptcopy();decodereceive(generated_address);desktop.accept('Verify receive address');time.sleep(.3)
  passed('Electron password reauthentication and correct recorded words enable the original wallet receive QR without a file backup')
  if generated_address=='0x9858EfFD232B4033E47d90003D41EC34EcaEda94':raise RuntimeError('Generated wallet reused a fixed import fixture')
  stopapp();launch(folder/'generated-restored',env);clickphrase('Restore from recovery phrase');waitprompt('Create local wallet')
  desktop.inputs('Create local wallet',[password,password,generated_phrase.decode('ascii'),'']);desktop.accept('Create local wallet');waitprompt('Imported account recovery limits');desktop.accept('Imported account recovery limits');time.sleep(2)
  if publicaddress()!=generated_address:raise RuntimeError('Fresh profile restored a different generated wallet')
  passed('Actual packaged app restores its generated wallet in an empty profile using only app password and recorded phrase')
  stopapp();launch(profile,env);clickphrase('Restore from recovery phrase');waitprompt('Create local wallet')
  desktop.inputs('Create local wallet',[password,password,'bad','']);desktop.accept('Create local wallet')
  expectfailure('valid English BIP39','valid English BIP39','invalid-recovery-phrase-feedback')
  if (profile/vault_relative).exists():raise RuntimeError('Invalid phrase created a vault')
  clickphrase('Restore from recovery phrase');waitprompt('Create local wallet');desktop.inputs('Create local wallet',[password,password,phrase,'']);desktop.accept('Create local wallet');waitprompt('Imported account recovery limits');desktop.accept('Imported account recovery limits');time.sleep(2)
  expected='0x9858EfFD232B4033E47d90003D41EC34EcaEda94'
  if publicaddress()!=expected:raise RuntimeError('Restored independent fixture address differs')
  passed('Known phrase restoration reproduces the independently generated public address without backup files')
  if copyfirstrow()!=expected:raise RuntimeError('Wallet row copied a different public address')
  passed('Wallet table copy icon invokes dialog verification and copies the full independent public address')
  overviewbalance();settingsjournal();exchangedisconnected(profile);openfirstwallet()
  desktop.screenshot(reports/'packaged-wallet-tab.png');clickprimary('Receive');waitprompt('Verify receive address');decodereceive(expected)
  if promptcopy()!=expected:raise RuntimeError('Electron QR popup copied a different address')
  desktop.accept('Verify receive address');time.sleep(.3)
  passed('Electron Receive displays independently decoded QR pixels and copies the exact address')
  clickprimary('Send');time.sleep(.3)
  if 'Full recipient' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Confirmed Send button failed')
  passed('Confirmed recovery enables Send form without any encrypted-backup prompt')
  desktop.key('Home',ctrl=True);clickphrase('Ethereum',button=2);time.sleep(.4)
  if 'Your wallets' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Middle click did not close wallet tab')
  passed('Middle-click closes wallet tab and returns to account table')
  stopapp();launch(profile,env);clickphrase('Unlock wallet');waitprompt('Unlock local wallet');desktop.inputs('Unlock local wallet',[password]);desktop.accept('Unlock local wallet');time.sleep(2)
  if publicaddress()!=expected:raise RuntimeError('Restart changed wallet identities')
  passed('Restart requires dialog unlock and preserves confirmed recovery and wallet address')
  stopapp()
  if selected_profile=='mainnet':
   saved=hashlib.sha256((profile/vault_relative).read_bytes()).hexdigest();launch(profile,env,environment='testnet')
   words=' '.join(a[11] for a in textboxes())
   if 'Testnet profile' not in words or 'Create your app password' not in words or (profile/'localwallet/testnet/vault.lwv').exists():raise RuntimeError('Explicit testnet reused mainnet state')
   if hashlib.sha256((profile/vault_relative).read_bytes()).hexdigest()!=saved:raise RuntimeError('Explicit testnet modified mainnet ciphertext')
   passed('Explicit testnet remains isolated from the restored mainnet profile')
   stopapp();launch(profile,env);clickphrase('Unlock wallet');waitprompt('Unlock local wallet');desktop.inputs('Unlock local wallet',[password]);desktop.accept('Unlock local wallet');time.sleep(2)
   walletmenu('Add deterministic account');desktop.key('Home',ctrl=True);until(lambda:'Account 2' in ' '.join(a[11] for a in textboxes()),45);clickphrase('Account 2');time.sleep(.5)
   if 'Derivation index 1' not in ' '.join(a[11] for a in textboxes()):raise RuntimeError('Additional account index not preserved')
   passed('Adding deterministic account preserves phrase confirmation and creates its own account tab')
   stopapp()
  canaries=[password.encode(),phrase.encode(),generated_phrase]
  # Entropy all-zero bytes are not scanned: binary formats legitimately contain
  # zero padding. Distinct password/phrase and their common encodings are checked.
  needles=[]
  for b in canaries:needles.extend([b,b.hex().encode(),base64.b64encode(b)])
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
  passed('Electron validation and phrase setup produce guidance without IPC handler stack traces or Mesa loader errors')
  if any(row['knownX11AtomDiagnostic'] for row in live_log_checks):raise RuntimeError('Live packaged modal or clipboard workflow emitted a known Chromium atom-cache diagnostic')
  passed('Installed modal and public clipboard workflows emit none of the three known Chromium X11 atom-cache diagnostics')
  report['recoveryMethod']='Electron recorded-word confirmation and phrase import; no encrypted backup API or file needed'
 if hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()!=report['producerSHA256']:raise RuntimeError('Packaged workflow producer changed during execution')
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
 for p in owned:
  p.terminate()
  try:p.wait(timeout=5)
  except subprocess.TimeoutExpired:p.kill();p.wait()
 for l in logs:l.close()
 (reports/'packaged-recovery.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(report['status']!='PASS')
