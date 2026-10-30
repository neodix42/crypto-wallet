#!/usr/bin/env python3
"""Uninstrumented Wails/WebKitGTK Go wallet acceptance on an isolated private X11 display.

Only newly generated disposable roots are used. Private pixels and OCR remain
in RAM; no phrase/password goes into logs, reports, files or process arguments.
The app runs without IP networking in its own test-only network namespace.
"""
import argparse
import ctypes as C
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import re
import shutil
import signal
import subprocess
import tempfile
import time
from collections import Counter
from PIL import Image, ImageOps

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORTS = ROOT / "reports"
REPORTS.mkdir(exist_ok=True)
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--binary", type=pathlib.Path, default=ROOT / "bin/cryptowallet")
parser.add_argument("--report-name", default="wails-wallet")
parser.add_argument("--instrumented", action="store_true")
mode = parser.add_mutually_exclusive_group()
mode.add_argument("--quick-import", action="store_true", help="Targeted public fixture import/restart check; omits generated-root address comparison")
mode.add_argument("--staking-only", action="store_true", help="Targeted actual NEAR Staking page check after disposable public-fixture import; no RPC server or transaction approval")
mode.add_argument("--tokens-only", action="store_true", help="Targeted actual Ethereum ETH/USDT presentation and selection after disposable public-fixture import; no RPC mock, import or payment")
mode.add_argument("--near-tokens-only", action="store_true", help="Actual offline NEAR native-asset table and token-contract import guards on a disposable public-fixture profile; no invented token balance or transaction")
mode.add_argument("--decred-copy-only", action="store_true", help="Targeted repeated Decred receive-copy diagnostics after disposable public-fixture import; no spending or public RPC")
mode.add_argument("--appearance-only", action="store_true", help="Targeted actual appearance persistence, Enter unlock and private Bybit credential layout; cancels custody confirmation without connecting")
mode.add_argument("--recovery-layout-only", action="store_true", help="Targeted actual 24-word authenticated backup, initial Copy phrase visibility and small-workarea layout after public-fixture import")
mode.add_argument("--main-row-copy-only", action="store_true", help="Targeted actual main-account Ethereum row icon and direct address copy using a public disposable phrase; no wallet opening, RPC service or payment")
mode.add_argument("--clipboard-only", action="store_true", help="Actual row/wallet/Receive copy buttons and selected-address keyboard shortcuts on an owned X11 display using a disposable public fixture")
args = parser.parse_args()
if not re.fullmatch(r"[a-z0-9-]{1,80}",args.report_name):
    raise SystemExit("Report name must be a simple owned test name")
report = {"status": "RUNNING", "scope": __doc__, "producerSHA256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(), "steps": [], "notRun": ["Separate physical machines", "Clean Ubuntu24/26 installed desktop matrix", "Wayland", "OS suspend/session lock", "Real funded sends", "Public RPC acceptance", "Live Bybit operations", "Independent security review"]}
report["instrumented"] = args.instrumented
if args.quick_import:
    report["scope"]="Targeted isolated WebKitGTK import/restart/Unlock check using the public abandon/about BIP39 fixture. This is not the full generated-root eight-wallet restore campaign."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (quick mode)")
if args.staking_only:
    report["scope"]="Actual uninstrumented NEAR Wallet/Staking navigation after importing a public disposable BIP39 fixture into a fresh isolated profile. Full AstroStakers contract, whole-NEAR units, unknown observed state, real offline RPC failure and disabled Withdraw are checked through X11 pixels/OCR/input. No RPC response is mocked and no transaction is prepared, approved or sent."
    report["notRun"].extend(["Generated-root setup, backup and exact eight-wallet address comparison (staking-only mode)", "Funded stake/unstake/withdraw execution and public testnet acceptance"])
if args.tokens_only:
    report["scope"]="Targeted actual Ethereum ETH/USDT wallet presentation on an isolated display after importing the public disposable BIP39 fixture. Public pixels verify separate unknown balances, the complete USDT contract, decimals, explicit selection, whole-USDT Send units, the unchanged owned receive address, encrypted address-book save/retry/validation feedback and recipient persistence after actual restart. No RPC response is mocked and no token is imported, transaction prepared, approved or sent. This partial mode is not full wallet/recovery acceptance."
    report["notRun"].extend(["Generated-root setup, backup, fresh-copy restore and all-eight-wallet comparison (tokens-only mode)", "Custom token metadata import and funded token execution (offline graphical mode)"])
if args.near_tokens_only:
    report["scope"]="Actual uninstrumented offline X11 NEAR wallet on a fresh public abandon/about fixture: native NEAR/24-decimal asset table, exact owned address/copy, NEP-141 contract input, URL rejection without approval, real offline metadata-query failure without installing a token, whole-NEAR Send units and unchanged native staking. No RPC response, token balance, import success, registration or transaction is fabricated."
    report["notRun"].extend(["Generated-root setup, backup and all-eight-wallet restore comparison (near-tokens-only mode)", "Actual custom NEP-141 token import, observed token balance, registration and token-send GUI workflows (offline graphical mode)"])
if args.decred_copy_only:
    report["scope"]="Targeted repeated actual Decred verified receive/copy on an isolated display after importing a public disposable BIP39 fixture; private windows remain excluded from public pixels and failure diagnostics contain only X11 window metadata. No payment, public RPC or complete wallet acceptance claim."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (Decred copy diagnostic mode)")
if args.appearance_only:
    report["scope"]="Targeted actual Light/18 px defaults, selected Settings tabs, Dark/24 px rendering and persistence after graceful restart, Enter-only unlock, and private two-field Bybit/footer layout. Disposable public fixture and API canaries only; custody confirmation is cancelled before encrypted credentials or any Bybit observation. No private pixels or values are retained."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (appearance-only mode)")
if args.main_row_copy_only:
    report["scope"]="Targeted actual Ethereum main-account copy icon, measured public button geometry and direct native copying of the displayed public address using the disposable abandon/about phrase. The icon must keep the account overview open, avoid wallet confirmation and copy the exact independent fixture address. No RPC response, private phrase or transaction is mocked."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (main-row-copy-only mode)")
if args.clipboard_only:
    report["scope"]="Actual uninstrumented X11 copy buttons and Ctrl+C/Ctrl+Shift+C/Ctrl+Insert for selected public addresses in the overview, wallet tab and verified Receive window. Every operation replaces a fresh public sentinel in an independent native clipboard client; no DOM/test bridge, private recovery words or transaction. Public disposable fixture only."
    report["notRun"].append("Generated-root setup, backup and all-eight-wallet restore comparison (clipboard-only mode)")
if args.recovery_layout_only:
    report["scope"]="Actual uninstrumented default 18px eight-wallet overview and authenticated 24-word backup on a disposable 1100px-high X11 display, including monitor clamping, initial Copy phrase visibility, wheel stability, static application icon and storage-location label. Public zero-entropy BIP39 fixture only; no RPC service or transaction."
    report["notRun"].append("Generated-root creation and independent fresh-copy restore (targeted 24-word layout mode)")
if args.instrumented:
    report["scope"] = "Instrumented Go statement-coverage run. " + report["scope"].replace("Uninstrumented", "Instrumented")
report_path = REPORTS / (args.report_name+".json")
app = xvfb = None
logs = []
password = "public-wails-wallet-password-79326"
different_password = "different-public-native-password-79326"
bybit_key = "PUBLIC_TEST_ONLY_APIKEY_92731"
bybit_secret = "PUBLIC_TEST_ONLY_SECRET_912735"
appearance = {"theme":"light", "fontSize":18}
recorded = None
wordlist_path=ROOT/"tests/oracles/bip39/english.txt"
wordlist_raw=wordlist_path.read_bytes()
if hashlib.sha256(wordlist_raw).hexdigest()!="2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda":
    raise SystemExit("Pinned public BIP39 English wordlist fixture changed")
wordlist=wordlist_raw.splitlines()
word_indices={word:index for index,word in enumerate(wordlist)}

def valid_mnemonic(phrase):
    words=phrase.split()
    if len(words) not in (12,15,18,21,24) or any(word not in word_indices for word in words):return False
    number=0
    for word in words:number=(number<<11)|word_indices[word]
    checksum_bits=len(words)//3
    entropy=(number>>checksum_bits).to_bytes((len(words)*11-checksum_bits)//8,"big")
    return number&((1<<checksum_bits)-1)==hashlib.sha256(entropy).digest()[0]>>(8-checksum_bits)

def source_identity():
    digest = hashlib.sha256()
    paths = [pathlib.Path(__file__), ROOT / "tests/e2e/x11.py", ROOT / "tests/e2e/phrase.py", wordlist_path, ROOT / "go.mod", ROOT / "go.sum"]
    for directory in (ROOT / "internal", ROOT / "cmd/cryptowallet", ROOT / "third_party/wails"):
        paths.extend(path for path in directory.rglob("*") if path.is_file() and "node_modules" not in path.parts and path.suffix in (".go", ".c", ".h", ".tsx", ".ts", ".css", ".html", ".js", ".json", ".png", ".svg"))
    for path in sorted(set(paths)):
        digest.update(str(path.relative_to(ROOT)).encode() + b"\0")
        digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()

report["sourceSHA256"] = source_identity()

def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

mod = load("native_wallet_x11", "tests/e2e/x11.py")
phrase_ui = load("native_wallet_phrase", "tests/e2e/phrase.py")
mod.X.XGrabServer.argtypes=[C.c_void_p]
mod.X.XUngrabServer.argtypes=[C.c_void_p]
mod.X.XSync.argtypes=[C.c_void_p,C.c_int]

class PrivateWindowVisible(RuntimeError):
    pass

def palette():
    if appearance["theme"] == "dark":
        return {"page":(16,19,25), "surface":(23,28,36), "chrome":(21,25,32), "input":(17,23,32), "primary":(185,229,208), "selected":(34,60,46), "border":(52,60,74), "button":(34,41,51), "disabled":(28,34,43)}
    return {"page":(245,247,250), "surface":(255,255,255), "chrome":(237,241,246), "input":(255,255,255), "primary":(32,99,67), "selected":(228,240,233), "border":(197,207,219), "button":(233,238,245), "disabled":(245,247,250)}

def masked_field_rectangles(picture, bounds, color):
    x,y,w,h=bounds
    borders=(palette()["border"],palette()["primary"])
    outlined=lambda xx,yy:any(max(abs(a-b) for a,b in zip(picture.getpixel((xx,yy)),wanted))<=3 for wanted in borders)
    # Dialog and input backgrounds can both be white. Require the full-width
    # entry's actual two borders; labels, blank gaps and footer surfaces cannot
    # masquerade as additional password inputs.
    rows=[yy for yy in range(y+10,y+h-75)
          if all(picture.getpixel((xx,yy))==color for xx in (x+60,x+w//2,x+w-60))
          and any(outlined(xx,yy) for xx in range(x+26,x+29))
          and any(outlined(xx,yy) for xx in range(x+w-29,x+w-26))]
    groups=[]
    for yy in rows:
        if not groups or yy!=groups[-1][-1]+1:groups.append([])
        groups[-1].append(yy)
    rectangles=[]
    for group in groups:
        if not 20<=len(group)<=80:continue
        center=(group[0]+group[-1])//2
        tops=[yy for yy in range(max(y,center-50),center-10) if outlined(x+w//2,yy)]
        bottoms=[yy for yy in range(center+10,min(y+h-75,center+51)) if outlined(x+w//2,yy)]
        if tops and bottoms and 20<=min(bottoms)-max(tops)+1<=80:
            rectangles.append((max(tops),min(bottoms)))
    return rectangles

def dialog_panel_bounds(picture,bounds):
    # Inspect only the owned application's pixels. The centered DOM dialog has
    # an opaque surface, a complete border and a dimmed backdrop. No pixels,
    # text or field values leave memory, including on an ambiguous layout.
    x,y,w,h=bounds
    if picture.mode!="RGB" or min(w,h)<150 or x<0 or y<0 or x+w>picture.width or y+h>picture.height:
        raise RuntimeError("Dialog capture exceeds the owned application bounds")
    colors=palette();center=x+w//2
    def near(xx,yy,color,tolerance=3):
        return max(abs(a-b) for a,b in zip(picture.getpixel((xx,yy)),color))<=tolerance
    dim=lambda color:tuple(round((a+b)/2) for a,b in zip(color,(8,16,24)))
    if not all(near(xx,y+8,dim(colors["chrome"])) for xx in (x+8,x+w-8)) or not near(x+8,y+h-8,dim(colors["surface"])):
        return None
    tops=[];bottoms=[]
    for yy in range(y+16,y+h-16):
        if not near(center,yy,colors["border"]):continue
        if near(center,yy+2,colors["surface"]) and not near(center,yy-2,colors["surface"]):tops.append(yy)
        if near(center,yy-2,colors["surface"]) and not near(center,yy+2,colors["surface"]):bottoms.append(yy)
    candidates=set()
    for top in tops:
        scan=top+16
        lefts=[xx for xx in range(x+16,center-150) if near(xx,scan,colors["border"]) and near(xx+2,scan,colors["surface"]) and not near(xx-2,scan,colors["surface"])]
        rights=[xx for xx in range(center+150,x+w-16) if near(xx,scan,colors["border"]) and near(xx-2,scan,colors["surface"]) and not near(xx+2,scan,colors["surface"])]
        for bottom in bottoms:
            if not 120<=bottom-top<h-32:continue
            for left in lefts:
                for right in rights:
                    if not 320<=right-left<min(w-32,1042) or abs(left+right-2*center)>2:continue
                    if all(near(xx,yy,colors["border"]) for xx in (left+16,center,right-16) for yy in (top,bottom)) and all(near(xx,yy,colors["border"]) for xx in (left,right) for yy in (top+16,(top+bottom)//2,bottom-16)):
                        candidates.add((left,top,right-left+1,bottom-top+1))
    if len(candidates)>1:raise RuntimeError("Application dialog frame was ambiguous")
    if not candidates:raise RuntimeError("Application dialog backdrop is visible without a complete frame")
    return next(iter(candidates))

def dialog_heading_matches(picture,bounds,prefix):
    # Heading OCR is exclusively in RAM. Match the requested complete title;
    # a different dialog cannot satisfy an action merely by sharing a word.
    x,y,w,h=bounds
    crop=picture.crop((x+18,y+10,x+w-18,y+min(185,h-50))).convert("L")
    if appearance["theme"]=="dark":crop=ImageOps.invert(crop)
    data=io.BytesIO();crop.resize((crop.width*2,crop.height*2),Image.Resampling.LANCZOS).save(data,format="PNG")
    raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","6"],input=data.getvalue(),stderr=subprocess.DEVNULL).decode("utf-8")
    normalize=lambda value:re.sub(r"[^a-z0-9]+"," ",value.lower()).strip()
    actual,wanted=normalize(raw),normalize(prefix)
    return actual==wanted or actual.startswith(wanted+" ")

def owned_application_processes():
    if app is None or app.poll() is not None:raise RuntimeError("Owned application exited while its dialog was active")
    pending=[app.pid];seen=set()
    while pending:
        pid=pending.pop()
        if pid in seen:continue
        seen.add(pid)
        root=pathlib.Path('/proc')/str(pid)
        try:
            arguments=(root/'cmdline').read_bytes().split(b'\0')
            if any(value.startswith(b'--internal-prompt') for value in arguments):
                raise RuntimeError("Application created a removed separate prompt process")
            for thread in (root/'task').iterdir():
                try:pending.extend(int(value) for value in (thread/'children').read_text().split())
                except (FileNotFoundError,ProcessLookupError):pass
        except (FileNotFoundError,ProcessLookupError):
            if pid==app.pid:raise RuntimeError("Owned application disappeared during dialog ownership check")
    return sorted(seen)

class Desktop(mod.Desktop):
    def grab_server(self):
        depth=getattr(self,"_server_grab_depth",0)
        if depth==0:mod.X.XGrabServer(self.d)
        self._server_grab_depth=depth+1

    def ungrab_server(self):
        depth=getattr(self,"_server_grab_depth",0)
        if depth<1:raise RuntimeError("Owned X11 server guard was not held")
        self._server_grab_depth=depth-1
        if depth==1:
            mod.X.XUngrabServer(self.d)
            mod.X.XFlush(self.d)

    def windows(self):
        # A window can retire between XQueryTree and XGetWindowAttributes.
        # Freeze only this disposable server across the whole enumeration.
        # Calls nested under public classification/capture must not release
        # the outer guard before its pixel read.
        self.grab_server()
        try:
            mod.X.XSync(self.d,0)
            return super().windows()
        finally:
            self.ungrab_server()

    def park_pointer(self):
        # The owned server is 1600x1100; its bottom-right corner lies outside
        # the monitor-fitted 1440px-wide wallet and centered private prompts. Move only, without
        # clicking, focusing a window or inspecting any private pixels. This
        # retires WebKit's native public-hover tooltip before a slow prompt
        # (notably a persisted Decred receive advance) finishes starting.
        mod.T.XTestFakeMotionEvent(self.d, -1, 1590, 1090, 0)
        mod.X.XFlush(self.d)
        time.sleep(.15)

    def window_metadata(self):
        # Failure-only metadata, never pixels, OCR, environment, arguments or
        # a process log. Freeze this owned X server while reading properties so
        # a retiring window cannot disappear between enumeration and lookup.
        def property(window, name):
            actual, format, count, remaining, data = C.c_ulong(), C.c_int(), C.c_ulong(), C.c_ulong(), C.c_void_p()
            atom = mod.X.XInternAtom(self.d, name.encode("ascii"), 0)
            status = mod.X.XGetWindowProperty(self.d, window, atom, 0, 64, 0, 0, C.byref(actual), C.byref(format), C.byref(count), C.byref(remaining), C.byref(data))
            try:
                if status != 0 or remaining.value or not data.value:
                    return None
                if format.value == 32 and count.value == 1:
                    return int(C.cast(data, C.POINTER(C.c_ulong))[0])
                if format.value == 8 and count.value <= 256:
                    return C.string_at(data, count.value).decode("ascii", errors="replace").split("\0")
                return None
            finally:
                if data.value:
                    mod.X.XFree(data)
        def window_type(window):
            atom = property(window, "_NET_WM_WINDOW_TYPE")
            if not isinstance(atom, int):
                return None
            mod.X.XGetAtomName.argtypes = [C.c_void_p, C.c_ulong]
            mod.X.XGetAtomName.restype = C.c_void_p
            name = mod.X.XGetAtomName(self.d, atom)
            try:
                return C.string_at(name).decode("ascii", errors="replace") if name else None
            finally:
                if name:
                    mod.X.XFree(name)
        def metadata(window, title):
            attributes = mod.WA()
            mod.X.XGetWindowAttributes(self.d, window, C.byref(attributes))
            return {"window": int(window), "title": title, "geometry": self.geometry(window),
                    "pid": property(window, "_NET_WM_PID"),
                    "transientFor": property(window, "WM_TRANSIENT_FOR"),
                    "class": property(window, "WM_CLASS"),
                    "windowType": window_type(window), "overrideRedirect": bool(attributes.override)}
        self.grab_server()
        try:
            mod.X.XSync(self.d, 0)
            return [metadata(window, title) for window, title in self.windows()]
        finally:
            self.ungrab_server()
    def wait(self, prefix, seconds=10):
        if prefix=="CryptoWallet" or prefix.startswith("CryptoWallet · "):
            found=super().wait(prefix,seconds)
            if not hasattr(self,"_application_window"):
                rows=self.window_metadata()
                row=next((row for row in rows if row['window']==found[0]),None)
                if row is None or not isinstance(row['pid'],int):raise RuntimeError("Application window lacks its numeric process identity")
                self._application_window,self._application_pid=found[0],row['pid']
            return found
        deadline=time.monotonic()+seconds
        while time.monotonic()<deadline:
            observed=self.dialog_capture()
            if observed:
                window,picture,bounds=observed
                if dialog_heading_matches(picture,bounds,prefix):
                    verify=getattr(self,"assert_dialog_owner",None)
                    identity=verify(window) if verify else {}
                    report.setdefault("inWindowDialogs",{})[prefix]={"window":int(window),"bounds":list(bounds),"sameApplicationWindow":True,**identity}
                    return window,prefix
            time.sleep(.1)
        raise RuntimeError("Expected in-window dialog heading did not appear: "+prefix)

    def assert_dialog_owner(self,window):
        rows=self.window_metadata()
        if len(rows)!=1 or rows[0]['window']!=window or window!=self._application_window or rows[0]['pid']!=self._application_pid:
            raise RuntimeError("Dialog did not remain inside the same application window and process")
        processes=owned_application_processes()
        if self._application_pid not in processes:raise RuntimeError("Dialog window is outside its owned application process tree")
        return {"pid":self._application_pid,"sameApplicationProcess":True,"topLevelWindows":1,"removedPromptProcessAbsent":True}

    def dialog_capture(self):
        self.grab_server()
        try:
            windows=self.windows()
            if len(windows)!=1 or not (windows[0][1]=="CryptoWallet" or windows[0][1].startswith("CryptoWallet · ")):
                return None
            window=windows[0][0]
            picture=self.image().convert("RGB")
            bounds=dialog_panel_bounds(picture,self.geometry(window))
            return (window,picture,bounds) if bounds else None
        finally:
            self.ungrab_server()

    def prompt_geometry(self,prefix):
        self.wait(prefix,30)
        observed=self.dialog_capture()
        if not observed or not dialog_heading_matches(observed[1],observed[2],prefix):
            raise RuntimeError("Application dialog changed before its bounded capture")
        return observed[2]

    def dialog_title(self,expected):
        observed=self.dialog_capture()
        return bool(observed and dialog_heading_matches(observed[1],observed[2],expected))
    def image(self):
        # Private image remains in memory, including during disposable phrase OCR.
        p = mod.X.XGetImage(self.d, self.root, 0, 0, 1600, 1100, 0xffffffff, 2)
        try:
            data = C.string_at(p.contents.data, p.contents.bytes_per_line * 1100)
            return Image.frombytes("RGB", (1600, 1100), data, "raw", "BGRX", p.contents.bytes_per_line, 1)
        finally:
            mod.X.XDestroyImage(p)
    def secret_window(self):
        # This is a test artifact guard, not an application restriction: never
        # retain ordinary screenshots while an in-window dialog may hold words.
        if any(title != "CryptoWallet" and not title.startswith("CryptoWallet · ") for _, title in self.windows()):return True
        return self.dialog_capture() is not None
    def accept(self, prefix):
        window,_=self.wait(prefix,30)
        self.focus(window)
        x,y=private_primary(prefix)
        report.setdefault("privateActionGeometry",[]).append({"title":prefix,"window":self.prompt_geometry(prefix),"point":[x,y]})
        self.click(x,y)
    def inputs(self, prefix, values):
        window,_=self.wait(prefix,30)
        self.focus(window)
        primary=private_primary(prefix)
        time.sleep(.2)
        x,y,w,h=self.prompt_geometry(prefix)
        picture=self.image().convert("RGB")
        rectangles=masked_field_rectangles(picture,(x,y,w,h),palette()["input"])
        centers=[(top+bottom)//2 for top,bottom in rectangles]
        if len(centers)!=len(values):
            colors=palette();probes=[]
            for offset in (26,27,28,w-29,w-28,w-27,w//2):
                matches=[yy-y for yy in range(y+10,y+h-75) if any(max(abs(a-b) for a,b in zip(picture.getpixel((x+offset,yy)),wanted))<=3 for wanted in (colors["border"],colors["primary"]))]
                groups=[]
                for yy in matches:
                    if not groups or yy!=groups[-1][-1]+1:groups.append([])
                    groups[-1].append(yy)
                probes.append({"offset":offset,"borderRuns":[[group[0],group[-1]] for group in groups]})
            report.setdefault("maskedInputFailure",[]).append({"title":prefix,"bounds":[x,y,w,h],"expected":len(values),"observed":len(centers),"borderProbes":probes})
            raise RuntimeError("Private masked input rectangles did not match expected field count: "+str(len(centers)))
        geometry={"title":prefix,"fieldCount":len(values),"window":[w,h],"centers":[yy-y for yy in centers],"heights":[bottom-top+1 for top,bottom in rectangles],"primary":[primary[0]-x,primary[1]-y],"typedBytes":[len(value.encode()) for value in values]}
        report.setdefault("privateInputGeometry",[]).append(geometry)
        # Click actual empty input rectangles, then type with XTest. Neither
        # clipboard nor a DOM/test bridge supplies any credential or phrase.
        for yy,value in zip(centers,values):
            self.click(x+140,yy)
            self.type(value)
            # WebKit acknowledges keyboard events across a process boundary.
            # Let the queued real key events drain before clicking another
            # input; GTK's old100ms pause could split a long phrase between
            # the mnemonic and optional-passphrase fields.
            time.sleep(.2+len(value)*.01)
        return geometry

def save():
    report_path.write_text(json.dumps(report, indent=2) + "\n")

def passed(name):
    report["steps"].append({"name": name, "status": "PASS"})
    print("PASS " + name, flush=True)
    save()

def stage(name):
    report["currentStep"]=name
    save()

def wait(predicate, seconds=45):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if app is not None and app.poll() is not None:
            raise RuntimeError("Owned native wallet exited during acceptance")
        value = predicate()
        if value:
            return value
        time.sleep(.1)
    report["ownedWindowTitles"]=[title for _,title in desktop.windows()]
    raise RuntimeError("Wails wallet workflow exceeded bounded deadline")

def private_primary(prefix):
    # A native window title can appear before React has loaded its private
    # prompt. Wait for the enabled, filled footer action before typing or
    # accepting, rather than racing a blank/disabled loading view.
    for _ in range(120):
        window,_=desktop.wait(prefix,30);x,y,w,h=desktop.prompt_geometry(prefix)
        picture=desktop.image().crop((x,y+h-70,x+w,y+h)).convert("RGB")
        pixels=[]
        for yy in range(70):
            points=[xx for xx in range(w//2,w) if picture.getpixel((xx,yy))==palette()["primary"]]
            if len(points)>=50:pixels.append((yy,points))
        if len(pixels)>=15 and pixels[-1][0]-pixels[0][0]>=20:
            points=max(pixels,key=lambda row:len(row[1]))[1]
            return x+(points[0]+points[-1])//2,y+h-70+(pixels[0][0]+pixels[-1][0])//2
        time.sleep(.1)
    # Numeric color/geometry metadata reveals neither prompt content nor
    # private pixels and distinguishes an unrendered/disabled footer from a
    # presentation mismatch without retaining a screenshot or OCR payload.
    counts=Counter(picture.getpixel((xx,yy)) for xx in range(w) for yy in range(70))
    report.setdefault("privateFooterFailures",[]).append({"title":prefix,"window":[w,h],"expectedPrimary":list(palette()["primary"]),"commonColors":[[list(color),count] for color,count in counts.most_common(6)]})
    raise RuntimeError("Private prompt did not finish rendering its enabled footer action")

def public_rows(contrast=False, with_picture=False, with_geometry=False, ocr_scale=2):
    if ocr_scale not in (1,2):raise RuntimeError("Unexpected public OCR scale")
    # Make classification and pixel capture one X11 operation. An async Go
    # prompt cannot become visible between the allowlist check and image read.
    # This is only the owned private :18x test server; release it before OCR.
    desktop.grab_server()
    try:
        mod.X.XSync(desktop.d,0)
        if desktop.secret_window():
            raise PrivateWindowVisible("Refusing public OCR while a private dialog is open")
        geometry=None
        if with_geometry:
            windows=[window for window,title in desktop.windows() if title=="CryptoWallet" or title.startswith("CryptoWallet · ")]
            if len(windows)!=1:raise RuntimeError("Public wallet window was not unique during capture")
            geometry=desktop.geometry(windows[0])
        picture=desktop.image()
    finally:
        desktop.ungrab_server()
    captured=picture
    # In the Light theme, disabled controls have low-contrast gray text on
    # near-white surfaces. Preserve the immutable, guard-checked public
    # capture while making those actual glyphs legible to the OCR engine.
    if contrast and appearance["theme"]=="light":
        picture=ImageOps.grayscale(picture).point(lambda value:0 if value<210 else 255)
    data = io.BytesIO()
    picture.resize((picture.width*ocr_scale,picture.height*ocr_scale),Image.Resampling.LANCZOS).save(data, format="PNG")
    raw = subprocess.check_output(["tesseract", "stdin", "stdout", "--psm", "11", "tsv"], input=data.getvalue(), stderr=subprocess.DEVNULL)
    rows = []
    for line in raw.decode().splitlines()[1:]:
        fields = line.split("\t")
        if len(fields) == 12 and fields[11].strip():
            for position in (6,7,8,9):fields[position]=str(int(fields[position])//ocr_scale)
            rows.append(fields)
    if with_geometry:return rows,captured,geometry
    return (rows,captured) if with_picture else rows

def private_rows(prefix):
    window,_=desktop.wait(prefix,30)
    desktop.focus(window)
    x,y,w,h=desktop.prompt_geometry(prefix)
    from PIL import ImageOps
    picture=desktop.image().crop((x,y,x+w,y+h)).convert("L")
    if appearance["theme"]=="dark":picture=ImageOps.invert(picture)
    data=io.BytesIO()
    picture.resize((w*3,h*3),Image.Resampling.LANCZOS).save(data,format="PNG")
    raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","6","tsv"],input=data.getvalue(),stderr=subprocess.DEVNULL)
    rows=[]
    for line in raw.decode().splitlines()[1:]:
        fields=line.split("\t")
        if len(fields)==12 and fields[11].strip():
            for position in (6,7,8,9):fields[position]=str(int(fields[position])//3)
            rows.append(fields)
    return rows

def private_click(prefix, words):
    if (prefix,words) not in (("Back up recovery phrase","Copy phrase"),("Record recovery phrase","Copy phrase")):
        raise RuntimeError("Unexpected private control request")
    wanted=words.lower().split()
    for _ in range(8):
        rows=private_rows(prefix)
        for i,row in enumerate(rows):
            segment=rows[i:i+len(wanted)]
            if [r[11].lower().strip("·.,:") for r in segment]==wanted and all(r[1:5]==row[1:5] for r in segment):
                window,_=desktop.wait(prefix,30);x,y,_,_=desktop.prompt_geometry(prefix)
                desktop.click(x+int(row[6])+int(row[8])//2,y+int(row[7])+int(row[9])//2)
                return
        # At larger fonts the authenticated backup's explanatory text and
        # explicit Copy button extend below its numbered words. Scroll only
        # this known private body's owned window; its footer stays pinned.
        window,_=desktop.wait(prefix,30);x,y,w,h=desktop.prompt_geometry(prefix)
        mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-18,y+h//2,0)
        for _ in range(3):
            mod.T.XTestFakeButtonEvent(desktop.d,5,1,0)
            mod.T.XTestFakeButtonEvent(desktop.d,5,0,0)
        mod.X.XFlush(desktop.d)
        time.sleep(.2)
    raise RuntimeError("Private control could not be identified; no private OCR diagnostic retained")

def private_phrase_grid(picture,count):
    # Private geometry only: no OCR payload, word glyph or image is retained.
    # Derive each CSS grid edge independently before checking its actual
    # border/interior pixels; floored cell widths cannot accumulate here.
    if count not in (12,24):raise RuntimeError("Unexpected private grid word count")
    w,h=picture.size
    if picture.mode!="RGB" or w<560 or h<250:return []
    columns=4 if w>=760 else 3
    cell=(w-52-(columns-1)*8)/columns
    predicted=[(int(26+i*(cell+8)+.5),int(26+(i+1)*cell+i*8+.5)-1) for i in range(columns)]
    colors=palette()
    def near(x,y,color):return max(abs(a-b) for a,b in zip(picture.getpixel((x,y)),color))<8
    scans=[]
    for yy in range(90,h-75):
        boxes=[]
        for left,right in predicted:
            ls=[xx for xx in range(left-1,left+2) if near(xx,yy,colors["border"]) and near(xx+2,yy,colors["button"])]
            rs=[xx for xx in range(right-1,right+2) if near(xx,yy,colors["border"]) and near(xx-2,yy,colors["button"])]
            if len(ls)!=1 or len(rs)!=1 or abs(rs[0]-ls[0]+1-cell)>2:break
            boxes.append((ls[0],rs[0]))
        if len(boxes)==columns and all(7<=boxes[i+1][0]-boxes[i][1]-1<=9 for i in range(columns-1)):
            scans.append((yy,tuple(boxes)))
    groups=[]
    for scan in scans:
        if not groups or scan[0]!=groups[-1][-1][0]+1 or scan[1]!=groups[-1][-1][1]:groups.append([])
        groups[-1].append(scan)
    groups=[g for g in groups if 15<=len(g)<=60]
    if len(groups)!=(count+columns-1)//columns:return []
    centers=[(g[0][0]+g[-1][0])//2 for g in groups]
    steps=[b-a for a,b in zip(centers,centers[1:])]
    if max(map(len,groups))-min(map(len,groups))>2 or max(steps)-min(steps)>2:return []
    return [(left,center-15,right+1,center+15) for center,group in zip(centers,groups) for left,right in group[len(group)//2][1]]

def numbered_phrase(prefix="Record recovery phrase", count=12):
    # Phrase pixels and OCR remain exclusively in RAM on the owned display.
    # Only ordinals and a word count may enter a failure report.
    if prefix not in ("Record recovery phrase","Back up recovery phrase") or count not in (12,24):
        raise RuntimeError("Unexpected private phrase view")
    for _ in range(8):
        rows=private_rows(prefix)
        raw=" ".join(r[11] for r in rows).encode()
        found={int(i):word for i,word in re.findall(rb"\b(\d{1,2})[.),:]?\s+([a-z]+)\b",raw)}
        if set(found)==set(range(1,count+1)):
            phrase=b" ".join(found[i] for i in range(1,count+1))
            if valid_mnemonic(phrase):return phrase
        time.sleep(.2)
    window,_=desktop.wait(prefix,30)
    x,y,w,h=desktop.prompt_geometry(prefix)
    picture=desktop.image().crop((x,y,x+w,y+h)).convert("RGB")
    from PIL import ImageOps
    cells=private_phrase_grid(picture,count)
    parsed={}
    for index,(left,top,right,bottom) in enumerate(cells,1):
        crop=picture.crop((left+34,top,right-7,bottom)).convert("L")
        if appearance["theme"]=="dark":crop=ImageOps.invert(crop)
        data=io.BytesIO();crop.resize((crop.width*4,crop.height*4),Image.Resampling.LANCZOS).save(data,format="PNG")
        value=subprocess.check_output(["tesseract","stdin","stdout","--psm","7","-c","tessedit_char_whitelist=abcdefghijklmnopqrstuvwxyz"],input=data.getvalue(),stderr=subprocess.DEVNULL).strip().lower()
        if re.fullmatch(rb"[a-z]+",value):parsed[index]=value
    if len(parsed)==count:
        phrase=b" ".join(parsed[i] for i in range(1,count+1))
        if valid_mnemonic(phrase):return phrase
    raise RuntimeError("Disposable private phrase OCR incomplete: "+str(len(found))+" ordinals; "+str(len(cells)//(4 if w>=760 else 3))+" grid rows; no words or pixels retained")

def record_phrase():
    deadline=time.monotonic()+30
    while time.monotonic()<deadline:
        if desktop.dialog_title("Record recovery phrase"):break
        if not desktop.secret_window():
            visible=text()
            for marker in ("Use an app password","The passwords did not match","Request cancelled","Request failed or is unavailable"):
                if marker in visible:
                    report["creationPublicResultCategory"]=marker
                    raise RuntimeError("Creation did not reach generated phrase; fixed public error category recorded")
        time.sleep(.1)
    phrase=numbered_phrase()
    private_click("Record recovery phrase","Copy phrase")
    wait(lambda:clipboard().encode()==phrase)
    desktop.accept("Record recovery phrase")
    desktop.wait("Confirm recovery phrase",30)
    words=phrase.split()
    desktop.inputs("Confirm recovery phrase",[words[i].decode("ascii") for i in (1,5,11)])
    desktop.accept("Confirm recovery phrase")
    return phrase

def selected_phrase_word_copy(expected,prefix="Back up recovery phrase"):
    # Exercise ordinary text selection on the disposable generated/public
    # fixture without retaining its text, image or clipboard contents.
    window,_=desktop.wait(prefix,30)
    x,y,w,h=desktop.prompt_geometry(prefix)
    picture=desktop.image().crop((x,y,x+w,y+h)).convert("RGB")
    cells=private_phrase_grid(picture,len(expected.split()))
    if len(cells)!=len(expected.split()):raise RuntimeError("Numbered phrase selection lacks its complete observed grid")
    left,top,_,bottom=cells[0]
    desktop.focus(window)
    desktop.click(x+left+45,y+(top+bottom)//2);desktop.click(x+left+45,y+(top+bottom)//2)
    del picture,cells
    for shortcut in ("Ctrl+C","Ctrl+Shift+C","Ctrl+Insert"):
        reset_public_clipboard()
        native_copy_shortcut(shortcut)
        wait(lambda:clipboard().encode()==expected.split()[0],10)
    report.setdefault("phraseSelection",[]).append({"dialog":prefix,"wordIndex":1,"shortcuts":3,"matchesDisplayedWord":True,"privatePixelsRetained":False})

def text():
    return " ".join(r[11] for r in public_rows())

def click_primary_public(heading):
    # Creation, Unlock and disconnected Bybit settings each have exactly one
    # filled primary button on their verified public page. These actions only
    # open a private input, never authorize or submit a funds operation.
    # Identify the rectangle independently of OCR contrast, then require the
    # exact next private prompt and workflow state.
    if heading not in text():
        raise RuntimeError("Expected locked public page missing before primary action")
    desktop.grab_server()
    try:
        mod.X.XSync(desktop.d,0)
        if desktop.secret_window():raise PrivateWindowVisible("Private dialog unexpectedly open during setup action")
        picture=desktop.image().convert("RGB")
    finally:
        desktop.ungrab_server()
    candidates=[]
    for yy in range(145,picture.height-50):
        points=[xx for xx in range(picture.width) if picture.getpixel((xx,yy))==palette()["primary"]]
        if len(points)>=45:candidates.append((yy,points))
    groups=[]
    for row in candidates:
        if not groups or row[0]-groups[-1][-1][0]>20:groups.append([])
        groups[-1].append(row)
    groups=[group for group in groups if len(group)>=20]
    if len(groups)!=1:
        picture.save(REPORTS/"wails-initial-public.png")
        raise RuntimeError("Locked public primary control was not uniquely identified: "+str(len(groups))+" candidate rectangles")
    group=groups[0];points=max(group,key=lambda row:len(row[1]))[1]
    desktop.click((points[0]+points[-1])//2,group[len(group)//2][0])

def click(words, top=False, last=False, button=1):
    wanted = [w.lower() for w in words.split()]
    for attempt in range(12):
        rows = public_rows()
        matches = []
        for i, row in enumerate(rows):
            segment = rows[i:i+len(wanted)]
            actual = [r[11].lower().strip("·.,:") for r in segment]
            if actual != wanted or not all(r[1:5] == row[1:5] for r in segment):
                continue
            if wanted==["receive"]:
                before=rows[i-1][11].lower().strip("·.,:") if i and rows[i-1][1:5]==row[1:5] else ""
                after=rows[i+1][11].lower().strip("·.,:") if i+1<len(rows) and rows[i+1][1:5]==row[1:5] else ""
                # Select the Receive button, excluding its address label and
                # the distinct Save receive QR button on the same public row.
                if before=="save" or after=="address":continue
            _, main_y, _, _ = desktop.geometry(desktop.wait("CryptoWallet")[0])
            if top and int(row[7]) > main_y + 145:
                continue
            matches.append(row)
        if matches:
            row = matches[-1] if last else matches[0]
            desktop.focus(desktop.wait("CryptoWallet")[0])
            clicked_at=time.monotonic()
            desktop.click(int(row[6])+int(row[8])//2, int(row[7])+int(row[9])//2, button=button)
            time.sleep(.25)
            return clicked_at
        if not top and words in ("Ethereum","Base","HyperEVM","HyperCore","NEAR","Aptos","Decred","TON") and attempt in (2,5,8):
            if desktop.secret_window():raise PrivateWindowVisible("Refusing public scrolling while a private prompt is visible")
            x,y,w,h=desktop.geometry(desktop.wait("CryptoWallet")[0])
            mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-22,y+h//2,0)
            for _ in range(4):
                mod.T.XTestFakeButtonEvent(desktop.d,5,1,0);mod.T.XTestFakeButtonEvent(desktop.d,5,0,0)
            mod.X.XFlush(desktop.d);time.sleep(.2)
        time.sleep(.15)
    raise RuntimeError("Public native control not found: " + words)

def clipboard(public_sentinel=None,selection_name="CLIPBOARD"):
    # Independent OS selection client on this owned display, not an app API.
    gtk = C.CDLL("libgtk-3.so.0")
    gdk = C.CDLL("libgdk-3.so.0")
    glib = C.CDLL("libglib-2.0.so.0")
    gtk.gtk_init_check.argtypes = [C.c_void_p, C.c_void_p]
    gtk.gtk_init_check.restype = C.c_int
    gdk.gdk_atom_intern_static_string.argtypes = [C.c_char_p]
    gdk.gdk_atom_intern_static_string.restype = C.c_void_p
    gtk.gtk_clipboard_get.argtypes = [C.c_void_p]
    gtk.gtk_clipboard_get.restype = C.c_void_p
    gtk.gtk_clipboard_wait_for_text.argtypes = [C.c_void_p]
    gtk.gtk_clipboard_wait_for_text.restype = C.c_void_p
    glib.g_free.argtypes = [C.c_void_p]
    if not gtk.gtk_init_check(None, None):
        raise RuntimeError("Private test clipboard client unavailable")
    if selection_name not in ("CLIPBOARD","PRIMARY"):
        raise RuntimeError("Unexpected owned test selection request")
    selection = gtk.gtk_clipboard_get(gdk.gdk_atom_intern_static_string(selection_name.encode("ascii")))
    if public_sentinel is not None:
        if not re.fullmatch(r"cryptowallet-public-test-sentinel-[0-9]+", public_sentinel) and public_sentinel not in (password,different_password):
            raise RuntimeError("Only a public sentinel or fixed disposable password fixture may enter the owned clipboard")
        gtk.gtk_clipboard_set_text.argtypes = [C.c_void_p, C.c_char_p, C.c_int]
        gtk.gtk_clipboard_set_text(selection, public_sentinel.encode("ascii"), -1)
    p = gtk.gtk_clipboard_wait_for_text(selection)
    if not p:
        return ""
    result = C.string_at(p).decode()
    glib.g_free(p)
    return result

def serve_owned_clipboard(seconds=1):
    # gtk_clipboard_set_text makes this independent test process the owner.
    # Serve its native selection requests while WebKit completes a real paste;
    # immediately pressing Enter would otherwise submit before data arrives.
    glib=C.CDLL("libglib-2.0.so.0")
    glib.g_main_context_iteration.argtypes=[C.c_void_p,C.c_int]
    glib.g_main_context_iteration.restype=C.c_int
    deadline=time.monotonic()+seconds
    while time.monotonic()<deadline:
        glib.g_main_context_iteration(None,0)
        time.sleep(.01)

def stop():
    global app
    if app is not None and app.poll() is None:
        os.killpg(app.pid, signal.SIGTERM)
        try:
            app.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(app.pid, signal.SIGKILL)
            app.wait(timeout=5)
    app = None

def close_gracefully():
    # Deliver the same WM_DELETE_WINDOW message as an ordinary window manager.
    # Killing a hung app is cleanup and must never count as a passing close.
    global app
    class Data(C.Union):
        _fields_=[("b",C.c_char*20),("s",C.c_short*10),("l",C.c_long*5)]
    class Client(C.Structure):
        _fields_=[("type",C.c_int),("serial",C.c_ulong),("send_event",C.c_int),("display",C.c_void_p),("window",C.c_ulong),("message_type",C.c_ulong),("format",C.c_int),("data",Data)]
    class XEvent(C.Union):
        _fields_=[("client",Client),("padding",C.c_long*24)]
    mod.X.XSendEvent.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_long,C.POINTER(XEvent)]
    window,_=desktop.wait("CryptoWallet",10)
    event=XEvent();event.client.type=33;event.client.send_event=1;event.client.display=desktop.d;event.client.window=window
    event.client.message_type=mod.X.XInternAtom(desktop.d,b"WM_PROTOCOLS",0);event.client.format=32
    event.client.data.l[0]=mod.X.XInternAtom(desktop.d,b"WM_DELETE_WINDOW",0)
    if not mod.X.XSendEvent(desktop.d,window,0,0,C.byref(event)):raise RuntimeError("Owned window close request could not be delivered")
    mod.X.XFlush(desktop.d)
    try:
        code=app.wait(timeout=10)
    except subprocess.TimeoutExpired:
        raise RuntimeError("Native window close failed to exit gracefully within ten seconds")
    if code!=0:raise RuntimeError("Native window close returned a failure status")
    app=None

def launch(installation, home):
    global app
    for name in ("_application_window","_application_pid"):
        if hasattr(desktop,name):delattr(desktop,name)
    env = dict(base_environment, HOME=str(home), XDG_CONFIG_HOME=str(home / "config"))
    home.mkdir(mode=0o700, exist_ok=True)
    log = tempfile.TemporaryFile()
    logs.append(log)
    command=["unshare","--user","--map-current-user","--keep-caps","--net","--mount","/bin/sh","-c",'mount --make-rprivate / && mount --bind "$1" /etc/hosts && exec setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all "$2"',"cryptowallet-owned-test",str(hosts_fixture),str(installation/"cryptowallet")]
    app = subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True)
    if args.instrumented:
        report.setdefault("processIDs", []).append(app.pid)
    report["currentStep"]="Wait for initial Wails public window"
    window,_=desktop.wait("CryptoWallet", 30)
    # GTK maps before its final monitor-fit allocation/centering callback.
    # Wait for contained numeric bounds before any public or dialog pixels.
    deadline=time.monotonic()+30
    while time.monotonic()<deadline:
        x,y,w,h=desktop.geometry(window)
        if x>=0 and y>=0 and x+w<=1600 and y+h<=1100:break
        time.sleep(.1)
    else:raise RuntimeError("Initial application geometry never fit the owned display")
    report["currentStep"]="Wait for initial Wails public wallet state"
    wait(lambda: "Create your app password" in text() or "Your encrypted wallet is on this device" in text())
    verify_window_icon()

def receive_dialog_state(windows):
    # Active dialogs live inside the main Wails window. Unexpected extra
    # native windows, including obsolete prompt titles, still block public
    # OCR and direct-copy acceptance rather than being treated as wallet UI.
    private = [title for _, title in windows if title != "CryptoWallet" and not title.startswith("CryptoWallet · ")]
    if not private:
        capture=getattr(desktop,"dialog_capture",None)
        observed=capture() if capture else None
        if observed:
            return "ready" if dialog_heading_matches(observed[1],observed[2],"Verify receive address") else "starting"
        return "absent"
    if len(private) != 1:
        raise RuntimeError("Unexpected native dialog during public address copy")
    if private[0] == "Verify receive address":
        return "ready"
    if private[0] == "cryptowallet":
        return "starting"
    raise RuntimeError("Unexpected native dialog during public address copy")

def reset_public_clipboard():
    sentinel="cryptowallet-public-test-sentinel-"+str(time.monotonic_ns())
    if clipboard(sentinel)!=sentinel:
        raise RuntimeError("Owned test clipboard did not acknowledge its fresh public sentinel")
    return sentinel

def public_copy_feedback(sentinel,timing=None):
    if desktop.secret_window():
        raise PrivateWindowVisible("Unexpected private window during direct address copy")
    value=clipboard()
    if not value or value==sentinel:return None
    observed_at=time.monotonic()
    if timing is not None and "nativeClipboardObservedSeconds" not in timing:
        # This upper bound ends at the independent native selection read,
        # before expensive status OCR. Existing input-settle and window-guard
        # overhead is included; there is deliberately no performance cutoff.
        timing["nativeClipboardObservedSeconds"]=round(observed_at-timing["clickedAt"],4)
    visible=text()
    if timing is not None:
        timing["feedbackReadSeconds"]=round(time.monotonic()-observed_at,4)
    if "waiting for wallet confirmation" in visible.lower():
        raise RuntimeError("Direct address copy incorrectly entered wallet confirmation")
    if "Address copied." not in visible:return None
    if timing is not None:timing["walletConfirmationAbsent"]=True
    return value

def request_receive_copy(seconds=30):
    # Copying the displayed public value has no wallet request or approval. A new nonaddress
    # sentinel prevents a stale address or status from satisfying this check.
    sentinel=reset_public_clipboard()
    deadline=time.monotonic()+seconds
    requested=False
    timing=None
    next_click=0
    while time.monotonic()<deadline:
        if receive_dialog_state(desktop.windows())!="absent":
            raise RuntimeError("Unexpected native dialog during direct address copy")
        if requested:
            value=public_copy_feedback(sentinel,timing)
            if value:
                report.setdefault("addressCopyTiming",[]).append({key:value for key,value in timing.items() if key!="clickedAt"})
                return value
        if time.monotonic()>=next_click:
            clicked_at=click("Copy address")
            timing={"context":"wallet-address-button","clickedAt":clicked_at,"measurement":"native clipboard observation upper bound before status OCR; input settling and window guard included"}
            requested=True
            desktop.park_pointer()
            next_click=time.monotonic()+.75
        time.sleep(.05)
    raise RuntimeError("Direct address copy did not acknowledge a fresh clipboard write within its bounded deadline")

def main_row_copy_target(rows,picture,geometry):
    # Bind the point to the actual Ethereum row and complete rendered button,
    # not a distance from the application window's unrelated outer edge.
    window,_=desktop.wait("CryptoWallet")
    if desktop.geometry(window)!=geometry:raise RuntimeError("Main-row wallet geometry changed after public capture")
    x,y,w,h=geometry
    if x<0 or y<0 or x+w>picture.width or y+h>picture.height:raise RuntimeError("Main-row public capture did not contain its complete wallet bounds")
    visible=" ".join(row[11] for row in rows)
    if "Account group" not in visible or public_words_position("Copy address",rows):
        raise RuntimeError("Main-row address copy left the account overview")
    matches=[row for row in rows if row[11]=="Ethereum" and y+145<int(row[7])<y+h-55]
    if len(matches)!=1:raise RuntimeError("Main-account Ethereum row was not uniquely visible")
    row=matches[0]
    row_y=int(row[7])+int(row[9])//2
    expected=palette()
    def color(point,wanted,tolerance=0):
        return max(abs(a-b) for a,b in zip(picture.getpixel(point),wanted))<=tolerance
    centers={"enabled":set(),"disabled":set()}
    disabled_border=tuple(round(.45*a+.55*b) for a,b in zip(expected["border"],expected["surface"]))
    states=(("enabled",expected["button"],expected["border"]),("disabled",expected["disabled"],disabled_border))
    clipped=False
    fill_runs=[]
    for state,button_color,border_color in states:
        for scan_y in range(row_y+9,row_y+14):
            runs=[]
            for xx in range(x+w//2,x+w-32):
                if color((xx,scan_y),button_color):
                    if not runs or xx!=runs[-1][-1]+1:runs.append([])
                    runs[-1].append(xx)
            for run in runs:
                if 8<=len(run)<=35 and run[-1]>=x+w-35:
                    clipped=True
                if not 26<=len(run)<=36:continue
                fill_runs.append([state,run[0],scan_y,len(run)])
                cx=(run[0]+run[-1])//2
                fill=[yy for yy in range(row_y-23,row_y+24)
                      if color((cx-11,yy),button_color) and color((cx+11,yy),button_color)]
                groups=[]
                for yy in fill:
                    if not groups or yy!=groups[-1][-1]+1:groups.append([])
                    groups[-1].append(yy)
                for group in groups:
                    cy=(group[0]+group[-1])//2
                    if not 22<=len(group)<=34 or abs(cy-row_y)>8:continue
                    # Require all four sides of the small rectangular control.
                    # The identically colored circular network avatar is outside
                    # this half of the row and cannot satisfy this border test.
                    vertical=lambda xx:all(color((xx,cy+dy),border_color,8) for dy in (-6,0,6))
                    horizontal=lambda yy:all(color((cx+dx,yy),border_color,8) for dx in (-6,0,6))
                    if (any(vertical(xx) for xx in range(cx-18,cx-14)) and any(vertical(xx) for xx in range(cx+15,cx+19))
                        and any(horizontal(yy) for yy in range(cy-18,cy-14)) and any(horizontal(yy) for yy in range(cy+15,cy+19))):
                        centers[state].add((cx,cy))
    observed=report.setdefault("mainRowCopy",{})
    observed.update(window=[x,y,w,h],ethereumRow=[int(row[6]),int(row[7]),int(row[8]),int(row[9])])
    if not centers["enabled"] and not centers["disabled"] and clipped:
        observed["controlState"]="clipped"
        return None
    if len(centers["disabled"])==1 and not centers["enabled"]:
        observed["controlState"]="disabled"
        return None
    if len(centers["enabled"])!=1 or centers["disabled"]:
        bounds=(x+w//2,row_y-25,x+w-32,row_y+26)
        path=REPORTS/(args.report_name+"-main-row-public-controls.png")
        picture.crop(bounds).save(path)
        report["mainRowCopyGeometryFailure"]={"window":list(geometry),"ethereumRow":[int(row[6]),int(row[7]),int(row[8]),int(row[9])],"fillRuns":fill_runs,"centers":{state:[list(point) for point in points] for state,points in centers.items()},"publicRegion":list(bounds),"artifact":path.name,"artifactSHA256":hashlib.sha256(path.read_bytes()).hexdigest()}
        raise RuntimeError("Ethereum row copy icon was not uniquely identified by its rendered rectangle")
    point=next(iter(centers["enabled"]))
    observed.update(controlState="enabled",buttonCenter=list(point))
    return point

def reveal_main_row_copy(geometry):
    # A normal horizontal wheel gesture reveals the table's clipped rightmost
    # column. The next capture must independently re-find the same Ethereum
    # row and whole button; no coordinates survive this scroll operation.
    if desktop.secret_window():raise PrivateWindowVisible("Refusing table scrolling while a private prompt is visible")
    window,_=desktop.wait("CryptoWallet")
    if desktop.geometry(window)!=geometry:raise RuntimeError("Main-row wallet geometry changed before horizontal reveal")
    x,y,w,h=geometry
    row=report["mainRowCopy"]["ethereumRow"]
    mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-50,row[1]+row[3]//2,0)
    mod.T.XTestFakeButtonEvent(desktop.d,7,1,0);mod.T.XTestFakeButtonEvent(desktop.d,7,0,0)
    mod.X.XFlush(desktop.d)
    time.sleep(.25)
    desktop.park_pointer()

def request_main_row_copy(seconds=30):
    sentinel=reset_public_clipboard()
    deadline=time.monotonic()+seconds
    requested=False
    timing=None
    next_click=0
    desktop.park_pointer()
    while time.monotonic()<deadline:
        if receive_dialog_state(desktop.windows())!="absent":
            raise RuntimeError("Unexpected native dialog during direct main-row address copy")
        if requested:
            value=public_copy_feedback(sentinel,timing)
            if value:
                rows,picture,geometry=public_rows(with_picture=True,with_geometry=True)
                if main_row_copy_target(rows,picture,geometry) is None:
                    raise RuntimeError("Main-row copy icon became unavailable after direct address copy")
                report.setdefault("addressCopyTiming",[]).append({key:value for key,value in timing.items() if key!="clickedAt"})
                return value
        if time.monotonic()>=next_click:
            rows,picture,geometry=public_rows(with_picture=True,with_geometry=True)
            point=main_row_copy_target(rows,picture,geometry)
            if point is None:
                if report["mainRowCopy"]["controlState"]=="clipped":
                    reveals=report["mainRowCopy"].get("horizontalReveals",0)
                    if reveals>=3:raise RuntimeError("Ethereum copy icon remained clipped after bounded horizontal reveal")
                    reveal_main_row_copy(geometry)
                    report["mainRowCopy"]["horizontalReveals"]=reveals+1
            elif desktop.secret_window():
                raise PrivateWindowVisible("Private prompt appeared before main-row address input")
            else:
                if desktop.geometry(desktop.wait("CryptoWallet")[0])!=geometry:
                    raise RuntimeError("Main-row wallet geometry changed before address input")
                desktop.focus(desktop.wait("CryptoWallet")[0])
                timing={"context":"main-account-row-icon","clickedAt":time.monotonic(),"measurement":"native clipboard observation upper bound before status OCR; input settling and window guard included"}
                desktop.click(*point)
                requested=True
                report.setdefault("mainRowCopy",{}).setdefault("attempts",0)
                report["mainRowCopy"]["attempts"]+=1
                desktop.park_pointer()
                next_click=time.monotonic()+.75
        time.sleep(.05)
    raise RuntimeError("Main-row address copy did not acknowledge a fresh clipboard write within its bounded deadline")

def main_row_copy(expected_address=None):
    stage("Main-account Ethereum row icon and direct validated address copy")
    copied=request_main_row_copy()
    stage("Main row copy preserves the main account")
    wait(lambda:"Account group" in text())
    if "Copy address" in text():raise RuntimeError("Main-row copy opened a wallet page")
    if not re.fullmatch(r"0x[0-9a-fA-F]{40}",copied):
        raise RuntimeError("Main-row copy did not return a complete Ethereum address")
    if expected_address is not None and copied!=expected_address:
        raise RuntimeError("Main-row copy differed from the independently derived Ethereum fixture address")
    report["mainRowCopy"]["clipboardSHA256"]=hashlib.sha256(copied.encode()).hexdigest()
    passed("Main-account row copy icon directly copies its validated address without opening a wallet tab or private dialog")
    return copied

def address_selection_target(context,rows,geometry):
    # OCR locates a visible public code token; only an independent exact OS
    # clipboard comparison determines whether the selected address is right.
    # No glyph is rewritten to the expected address and no DOM is inspected.
    if context not in ("overview","wallet","receive"):
        raise RuntimeError("Unknown public address-selection context")
    x,y,w,h=geometry
    if x<0 or y<0 or x+w>1600 or y+h>1100:
        raise RuntimeError("Public address-selection bounds exceed the owned display")
    visible=" ".join(row[11] for row in rows)
    if context=="overview":
        if "Account group" not in visible or public_words_position("Copy address",rows):
            raise RuntimeError("Address selection left the account overview")
        labels=[row for row in rows if row[11]=="Ethereum" and y+145<int(row[7])<y+h-55]
        if len(labels)!=1:raise RuntimeError("Ethereum selection row was not uniquely visible")
        center_y=int(labels[0][7])+int(labels[0][9])//2
    else:
        if context=="wallet" and not public_words_position("Copy address",rows):
            raise RuntimeError("Address selection left the wallet page")
        label=public_words_position("Receive address" if context=="wallet" else "Copy address",rows)
        if not label:raise RuntimeError("Public address label was not visible")
        center_y=label[1]
    candidates=[]
    for row in rows:
        left,top,width,height=(int(row[index]) for index in (6,7,8,9))
        # PSM 6 can split a long rendered address into adjacent OCR tokens.
        # A unique visible prefix locates the native double-click only; the
        # fresh OS clipboard must still equal the complete independent address.
        if not re.fullmatch(r"[0Oo][xX][0-9A-Za-z]{4,64}",row[11]):continue
        if not (x<left and left+width<x+w and y<top and top+height<y+h):continue
        if width<40 or not 8<=height<=36 or abs(top+height//2-center_y)>20:continue
        candidates.append((left+width//2,top+height//2))
    if len(candidates)!=1:raise RuntimeError("Public address selection token was not uniquely visible")
    return candidates[0]

def native_copy_shortcut(shortcut):
    keys={"Ctrl+C":("Control_L","c"),"Ctrl+Shift+C":("Control_L","Shift_L","c"),"Ctrl+Insert":("Control_L","Insert")}
    if shortcut not in keys:raise RuntimeError("Unsupported public copy shortcut")
    codes=[mod.X.XKeysymToKeycode(desktop.d,mod.X.XStringToKeysym(name.encode("ascii"))) for name in keys[shortcut]]
    if not all(codes):raise RuntimeError("Owned X11 keyboard map lacks a required copy key")
    pressed=[]
    try:
        for code in codes:
            mod.T.XTestFakeKeyEvent(desktop.d,code,1,0)
            pressed.append(code)
    finally:
        for code in reversed(pressed):mod.T.XTestFakeKeyEvent(desktop.d,code,0,0)
        mod.X.XFlush(desktop.d)
    time.sleep(.15)

def x11_selection_state():
    # Only fixed numeric metadata from this owned X11 server. No text,
    # process argument, environment or private screen is retained here.
    mod.X.XGetInputFocus.argtypes=[C.c_void_p,C.POINTER(C.c_ulong),C.POINTER(C.c_int)]
    mod.X.XGetSelectionOwner.argtypes=[C.c_void_p,C.c_ulong]
    mod.X.XGetSelectionOwner.restype=C.c_ulong
    focus,revert=C.c_ulong(),C.c_int()
    mod.X.XGetInputFocus(desktop.d,C.byref(focus),C.byref(revert))
    owners={name:int(mod.X.XGetSelectionOwner(desktop.d,mod.X.XInternAtom(desktop.d,name.encode("ascii"),0)))
            for name in ("CLIPBOARD","PRIMARY")}
    return {"focusWindow":focus.value,"selectionOwners":owners}

def selected_address_copy(context,expected_address):
    stage("Locate actual "+context+" public address before selecting it")
    sentinel=reset_public_clipboard()
    if clipboard(sentinel,selection_name="PRIMARY")!=sentinel:
        raise RuntimeError("Owned PRIMARY selection did not acknowledge its fresh public sentinel")
    if context=="receive":
        if receive_dialog_state(desktop.windows())!="ready":
            raise RuntimeError("Public receive selection lost its exact verified window")
        window,_=desktop.wait("Verify receive address",30)
        private_primary("Verify receive address")
        geometry=desktop.prompt_geometry("Verify receive address")
        x,y,_,_=geometry
        rows=private_rows("Verify receive address")
        rows=[list(row) for row in rows]
        for row in rows:
            row[6]=str(int(row[6])+x);row[7]=str(int(row[7])+y)
    else:
        rows,_,geometry=public_rows(with_picture=True,with_geometry=True)
        window,_=desktop.wait("CryptoWallet")
    point=address_selection_target(context,rows,geometry)
    report.setdefault("addressSelectionGeometry",[]).append({"view":context,"window":list(geometry),"point":list(point),
        "publicCodeBoxes":[[int(row[index]) for index in (6,7,8,9)] for row in rows if re.match(r"[0Oo][xX]",row[11])]})
    desktop.focus(window)
    current_geometry=lambda:desktop.prompt_geometry("Verify receive address") if context=="receive" else desktop.geometry(window)
    if current_geometry()!=geometry:raise RuntimeError("Address-selection window moved before input")
    box=(point[0]-160,point[1]-18,point[0]+160,point[1]+18)
    before=desktop.image().crop(box)
    desktop.click(*point);desktop.click(*point)
    after=desktop.image().crop(box)
    primary=clipboard(selection_name="PRIMARY")
    selection_color=(65,102,87) if appearance["theme"]=="dark" else (204,227,215)
    report.setdefault("addressSelectionGeometry",[])[-1].update(primaryLength=len(primary),primaryMatchesExpected=primary==expected_address,
        changedPixels=sum(a!=b for a,b in zip(before.getdata(),after.getdata())),
        selectionColorPixels=sum(pixel==selection_color for pixel in after.getdata()),nativeState=x11_selection_state())
    del before,after,primary
    # Native copying does not navigate or change this public view. Measure
    # before selecting once, then exercise the same real selection with all
    # three shortcuts. Highlighted text need not remain readable to OCR.
    for index,shortcut in enumerate(("Ctrl+C","Ctrl+Shift+C","Ctrl+Insert")):
        stage("Actual "+context+" selected-address "+shortcut)
        if index:sentinel=reset_public_clipboard()
        if current_geometry()!=geometry:raise RuntimeError("Address-selection window moved between shortcuts")
        if context=="receive":
            if receive_dialog_state(desktop.windows())!="ready":
                raise RuntimeError("Selected receive address lost its exact verified window")
        elif desktop.secret_window():
            raise PrivateWindowVisible("Private prompt appeared during public address selection")
        if context=="overview":
            visible=text()
            if "Account group" not in visible or "Copy address" in visible:
                raise RuntimeError("Selecting an overview address navigated to a wallet")
        if clipboard()!=sentinel:
            raise RuntimeError("Selecting an address changed CLIPBOARD without a copy shortcut")
        native_copy_shortcut(shortcut)
        observed={"value":sentinel}
        def exact_clipboard():
            observed["value"]=clipboard()
            return observed["value"]==expected_address
        try:wait(exact_clipboard,10)
        except RuntimeError:
            value=observed["value"]
            report.setdefault("keyboardCopyFailure",[]).append({"view":context,"shortcut":shortcut,
                "sentinelUnchanged":value==sentinel,"observedLength":len(value),"observedSHA256":hashlib.sha256(value.encode()).hexdigest(),
                "nativeState":x11_selection_state(),"primaryMatchesExpected":clipboard(selection_name="PRIMARY")==expected_address})
            raise RuntimeError("Selected public address did not reach the independent OS clipboard: "+context+" "+shortcut)
        report.setdefault("keyboardCopies",[]).append({"view":context,"shortcut":shortcut,"matchesIndependentAddress":True,"freshSentinelReplaced":True})
    passed("Actual "+context+" selected public address copies exactly with Ctrl+C, Ctrl+Shift+C and Ctrl+Insert")

def clipboard_campaign(expected_address,secret):
    selected_address_copy("overview",expected_address)
    click("Ethereum")
    wait(lambda:"Copy address" in text())
    if request_receive_copy()!=expected_address:
        raise RuntimeError("Wallet copy button changed its independently known address")
    selected_address_copy("wallet",expected_address)
    click("Receive")
    desktop.wait("Verify receive address",30)
    private_primary("Verify receive address")
    sentinel=reset_public_clipboard()
    if receive_dialog_state(desktop.windows())!="ready":
        raise RuntimeError("Receive copy lost its exact verified public window")
    window,_=desktop.wait("Verify receive address",30)
    x,y,_,_=desktop.prompt_geometry("Verify receive address")
    point=public_words_position("Copy address",private_rows("Verify receive address"))
    if not point:raise RuntimeError("Receive copy button label was not visible")
    desktop.click(x+point[0],y+point[1])
    wait(lambda:clipboard()==expected_address,10)
    if clipboard()==sentinel:raise RuntimeError("Receive copy did not replace its fresh clipboard sentinel")
    selected_address_copy("receive",expected_address)
    desktop.accept("Verify receive address")
    wait(lambda:not desktop.secret_window())
    passed("Receive copy button and native address selection use the exact verified public address")
    # Standard clipboard paste works in the same main-window password dialog.
    # This is the fixed public disposable fixture, never a user credential.
    click("Settings",top=True);click("Wallet")
    click("Back up recovery phrase")
    desktop.wait("Back up recovery phrase",30)
    desktop.accept("Back up recovery phrase")
    desktop.wait("Confirm app password",30)
    desktop.inputs("Confirm app password",[""])
    if clipboard(secret)!=secret:raise RuntimeError("Disposable clipboard password fixture was not acknowledged")
    desktop.key("v",ctrl=True)
    serve_owned_clipboard()
    desktop.key("Return")
    desktop.wait("Back up recovery phrase",30)
    if recorded is None or numbered_phrase("Back up recovery phrase",len(recorded.split()))!=recorded:
        raise RuntimeError("Password paste failed to authenticate the unchanged recovery phrase")
    desktop.accept("Back up recovery phrase")
    wait(lambda:not desktop.secret_window())
    click("Main account",top=True)
    passed("Ordinary Ctrl+V password entry authenticates the exact recovery phrase in a dialog inside the main application window")

def addresses(networks=("Ethereum", "Base", "HyperEVM", "HyperCore", "NEAR", "Aptos", "Decred", "TON")):
    result = {}
    # These are the display labels in the pinned mainnet registry.
    for network in networks:
        report["currentStep"] = "Select main account before " + network
        click("Main account", top=True)
        # Some network labels contain an ecosystem prefix; match the unique word.
        report["currentStep"] = "Select " + network + " wallet row"
        deadline=time.monotonic()+30
        while time.monotonic()<deadline:
            if "Copy address" in text():break
            click(network)
            time.sleep(.15)
        report["currentStep"] = "Wait for " + network + " wallet page"
        wait(lambda: "Copy address" in text())
        report["currentStep"] = "Request " + network + " address copy"
        value=request_receive_copy()
        pattern = {"NEAR":r"[0-9a-f]{64}","Aptos":r"0x[0-9a-fA-F]{64}","Decred":r"Ds[1-9A-HJ-NP-Za-km-z]{32,34}","TON":r"[UE]Q[A-Za-z0-9_-]{46}"}.get(network,r"0x[0-9a-fA-F]{40}")
        if not re.fullmatch(pattern,value):
            raise RuntimeError("Fresh copied address did not match the selected network: "+network)
        result[network] = value
        report.setdefault("walletsObserved",[]).append(network)
        save()
        if network == "Ethereum":
            ethereum_tokens(value)
        if network != "TON":
            report["currentStep"] = "Close " + network + " wallet tab by middle click"
            deadline=time.monotonic()+30
            while time.monotonic()<deadline:
                rows=public_rows();words=" ".join(r[11] for r in rows)
                _,main_y,_,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
                present=any(r[11].lower().strip("·.,:")==network.lower() and int(r[7])<=main_y+145 for r in rows)
                if "Account group" in words and "Copy address" not in words and not present:break
                click(network,top=True,button=2)
                time.sleep(.15)
            else:raise RuntimeError("Middle click did not close the selected wallet tab")
    return result

def scroll_public(button, count=5):
    if button not in (4,5):raise RuntimeError("Only owned workspace scrolling is permitted")
    if desktop.secret_window():raise PrivateWindowVisible("Refusing public scroll while a private prompt is visible")
    window,_=desktop.wait("CryptoWallet");x,y,w,h=desktop.geometry(window)
    mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-22,y+h//2,0)
    for _ in range(count):
        mod.T.XTestFakeButtonEvent(desktop.d,button,1,0)
        mod.T.XTestFakeButtonEvent(desktop.d,button,0,0)
    mod.X.XFlush(desktop.d)
    time.sleep(.25)

def verify_window_icon():
    # Read only this public window's static icon property, never private pixels.
    desktop.grab_server()
    try:
        if desktop.secret_window():raise PrivateWindowVisible("Unexpected private prompt during application icon check")
        window,_=desktop.wait("CryptoWallet")
        actual,format,count,remaining,data=C.c_ulong(),C.c_int(),C.c_ulong(),C.c_ulong(),C.c_void_p()
        atom=mod.X.XInternAtom(desktop.d,b"_NET_WM_ICON",0)
        status=mod.X.XGetWindowProperty(desktop.d,window,atom,0,16386,0,6,C.byref(actual),C.byref(format),C.byref(count),C.byref(remaining),C.byref(data))
        try:
            if status or actual.value!=6 or format.value!=32 or count.value!=16386 or remaining.value or not data.value:raise RuntimeError("Public application icon property is missing or malformed")
            values=C.cast(data,C.POINTER(C.c_ulong))
            expected=Image.open(ROOT/"internal/branding/window-icon.png").convert("RGBA")
            pixels=[(a<<24)|(r<<16)|(g<<8)|b for r,g,b,a in expected.getdata()]
            # Xlib stores 32-bit CARDINALs in native unsigned longs; some
            # backends sign-extend the upper 32 bits on 64-bit systems.
            observed=[value&0xffffffff for value in values[2:16386]]
            if (values[0],values[1])!=(128,128) or observed!=pixels:
                mismatches=[index for index,(a,b) in enumerate(zip(observed,pixels)) if a!=b]
                report["staticIconMismatch"]={"count":len(mismatches),"first":[[i,observed[i],pixels[i]] for i in mismatches[:8]]}
                raise RuntimeError("Public application icon differs from its embedded 128px PNG")
            report["mainWindowIcon"]={"dimensions":[128,128],"matchesEmbeddedPNG":True}
        finally:
            if data.value:mod.X.XFree(data)
    finally:
        desktop.ungrab_server()

def overview_layout():
    stage("Verify complete default 18px overview without vertical scrolling")
    expected={"ethereum","near","hyperevm","hypercore","aptos","base","decred","ton"}
    def positions():
        rows,_,bounds=public_rows(with_picture=True,with_geometry=True)
        x,y,w,h=bounds
        headers=[r for r in rows if r[11].lower()=="network" and x+20<int(r[6])<x+400 and int(r[7])>y+145]
        if len(headers)!=1:return None
        top=int(headers[0][7])+int(headers[0][9])
        found={r[11].lower():int(r[7]) for r in rows if r[11].lower() in expected and x+20<int(r[6])<x+400 and top<int(r[7])<y+h-45}
        return (found,bounds) if set(found)==expected else None
    before,bounds=wait(positions)
    scroll_public(5,8)
    after,after_bounds=wait(positions)
    if before!=after or bounds!=after_bounds:raise RuntimeError("Default eight-wallet overview moved after vertical wheel input")
    x,y,w,h=bounds
    if x<0 or y<0 or x+w>1600 or y+h>1100:raise RuntimeError("Fitted main window extends outside the owned logical monitor")
    report["overviewLayout"]={"window":[w,h],"origin":[x,y],"withinOwnedDisplay":True,"visibleNetworks":len(before),"wheelStable":True,"fontSize":appearance["fontSize"]}
    passed("Default 18px overview shows all eight network rows without vertical wheel movement")

def verify_storage_location(home):
    click("Wallet")
    visible=wait(lambda:(v if "Encrypted storage directory" in (v:=text()) else None))
    compact="".join(visible.split())
    if "/cryptowallet/mainnet" not in compact or "vault.lwv" not in compact:raise RuntimeError("Settings Wallet did not display its profile directory and vault filename")
    report["storageLocation"]={"directoryLabelVisible":True,"mainnetProfileSuffixVisible":True,"vaultFilenameVisible":True,"temporaryProfileFileExists":(home/"config/cryptowallet/mainnet/vault.lwv").is_file()}
    if not report["storageLocation"]["temporaryProfileFileExists"]:raise RuntimeError("Disposable profile vault was not found at its configured location")
    passed("Settings Wallet displays the encrypted profile location and vault filename")

def backup_phrase(expected,secret):
    click("Back up recovery phrase")
    desktop.wait("Back up recovery phrase",30)
    desktop.accept("Back up recovery phrase")
    desktop.wait("Confirm app password",30)
    desktop.inputs("Confirm app password",[secret])
    desktop.accept("Confirm app password")
    words=len(expected.split())
    if numbered_phrase("Back up recovery phrase",words)!=expected:raise RuntimeError("Authenticated backup changed the recovery phrase")
    rows=private_rows("Back up recovery phrase")
    if not any([r[11].lower() for r in rows[i:i+2]]==["copy","phrase"] and rows[i][1:5]==rows[i+1][1:5] for i in range(len(rows)-1)):raise RuntimeError("Initial backup view does not show Copy phrase without scrolling")
    window,_=desktop.wait("Back up recovery phrase");x,y,w,h=desktop.prompt_geometry("Back up recovery phrase")
    if x<0 or y<0 or x+w>1600 or y+h>1100:raise RuntimeError("Fitted recovery backup extends outside the owned logical monitor")
    desktop.park_pointer()
    before=desktop.image().crop((x,y+175,x+w,y+h-80)).tobytes()
    mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-15,y+h//2,0)
    for _ in range(8):
        mod.T.XTestFakeButtonEvent(desktop.d,5,1,0);mod.T.XTestFakeButtonEvent(desktop.d,5,0,0)
    mod.X.XFlush(desktop.d);desktop.park_pointer()
    stable=before==desktop.image().crop((x,y+175,x+w,y+h-80)).tobytes()
    del before,rows
    if not stable:raise RuntimeError("Default backup body scrolled instead of fitting its complete phrase and Copy control")
    report["backupLayout"]={"wordCount":words,"window":[w,h],"origin":[x,y],"withinOwnedDisplay":True,"copyInitiallyVisible":True,"wheelStable":True,"privatePixelsRetained":False}
    selected_phrase_word_copy(expected)
    private_click("Back up recovery phrase","Copy phrase")
    wait(lambda:clipboard().encode()==expected)
    desktop.accept("Back up recovery phrase")
    wait(lambda:not desktop.secret_window())
    if clipboard().encode()!=expected:raise RuntimeError("Explicit phrase copy was lost when its private view closed")
    passed("Authenticated numbered backup and Copy phrase fit initially without scrolling and copy only on explicit private click")

def evm_asset_button_target(symbol,rows,picture,bounds,heading_y,require_enabled=False):
    # An exact glyph alone is insufficient: the balance repeats the symbol.
    # Require an independently observed value-only Unknown balance and its bounded filled
    # button to the left. Never substitute balance text or repair OCR glyphs.
    if symbol not in ("ETH","USDT"):raise RuntimeError("Unexpected asset selector target")
    x,y,w,h=bounds
    if x<0 or y<0 or x+w>picture.width or y+h>picture.height:raise RuntimeError("Asset selector capture exceeds owned bounds")
    balances=[r for r in rows if r[11]=="Unknown" and int(r[7])>heading_y]
    found=set()
    for row in rows:
        if row[11]!=symbol:continue
        left,top,width,height=(int(row[index]) for index in (6,7,8,9))
        cx,cy=left+width//2,top+height//2
        if not (x<left and left+width<x+w and y<top-4 and top+height+4<y+h):continue
        if not any(left+width<int(balance[6])-7 and abs(cy-int(balance[7])-int(balance[9])//2)<16 for balance in balances):continue
        fill=picture.getpixel((cx,top-4))
        if fill!=palette()["button"] and (require_enabled or fill!=palette()["disabled"]):continue
        if any(picture.getpixel(point)!=fill for point in ((cx,top+height+4),(left-5,cy),(left+width+5,cy))):continue
        run_left=run_right=cx
        while run_left>x and picture.getpixel((run_left-1,top-4))==fill:run_left-=1
        while run_right<x+w-1 and picture.getpixel((run_right+1,top-4))==fill:run_right+=1
        edge_x=run_left+5
        run_top=run_bottom=top-4
        while run_top>y and picture.getpixel((edge_x,run_top-1))==fill:run_top-=1
        while run_bottom<y+h-1 and picture.getpixel((edge_x,run_bottom+1))==fill:run_bottom+=1
        if 36<=run_right-run_left+1<=180 and 30<=run_bottom-run_top+1<=70 and run_left<left and left+width<run_right and run_top<top and top+height<run_bottom:found.add((cx,cy))
    return next(iter(found)) if len(found)==1 else None

def exact_evm_contract_text(raw,expected):
    # Preserve every non-whitespace glyph. No hex whitelist, character repair
    # or expected-address substring can turn a confusable or longer identity
    # into the reviewed contract.
    candidates=[]
    for line in raw.decode("utf-8").splitlines():
        compact=re.sub(r"\s+","",line)
        candidates.extend(re.findall(r"(?<![0-9A-Za-z])0x[0-9a-fA-F]{40}(?![0-9A-Za-z])",compact))
    return [value.lower() for value in candidates]==[expected.lower()]

def evm_contract_tooltip_block(rows,picture,bounds,point):
    x,y,w,h=bounds
    if x<0 or y<0 or x+w>picture.width or y+h>picture.height:raise RuntimeError("Contract hover capture exceeds owned bounds")
    labels=[r for i,r in enumerate(rows[:-1]) if r[11]=="ERC-20" and rows[i+1][11]=="contract" and r[1:5]==rows[i+1][1:5] and point[0]-70<int(r[6])<point[0]+710 and max(y+145,point[1]-190)<int(r[7])<point[1]-20]
    if len(labels)!=1:return None
    label=labels[0]
    sample_x,sample_y=int(label[6])+10,int(label[7])-4
    fill=palette()["chrome"]
    if not (x<sample_x<x+w and y<sample_y<y+h) or picture.getpixel((sample_x,sample_y))!=fill:return None
    left=right=sample_x
    while left>x and picture.getpixel((left-1,sample_y))==fill:left-=1
    while right<x+w-1 and picture.getpixel((right+1,sample_y))==fill:right+=1
    edge_x=left+5
    top=bottom=sample_y
    while top>y and picture.getpixel((edge_x,top-1))==fill:top-=1
    while bottom<y+h-1 and picture.getpixel((edge_x,bottom+1))==fill:bottom+=1
    if not (100<=right-left+1<=710 and 35<=bottom-top+1<=140):return None
    box=(left,top,right+1,bottom+1)
    return picture.crop(box).convert("L"),box

def evm_contract_address_line(picture):
    # This receives only the immutable, label/fill-bounded public tooltip.
    # Separate its two real ink lines, without looking for expected glyphs or
    # trimming a prefix/suffix that might change the displayed identity.
    if picture.mode!="L" or not (100<=picture.width<=710 and 35<=picture.height<=140):return None
    if appearance["theme"] not in ("light","dark"):return None
    fill=Image.new("RGB",(1,1),palette()["chrome"]).convert("L").getpixel((0,0))
    if any(picture.getpixel(point)!=fill for point in ((picture.width//2,2),(picture.width//2,picture.height-3))):return None
    gray=ImageOps.invert(picture) if appearance["theme"]=="dark" else picture
    ink=[]
    for yy in range(1,gray.height-1):
        columns=[xx for xx in range(1,gray.width-1) if gray.getpixel((xx,yy))<160]
        if columns:ink.append((yy,min(columns),max(columns)))
    groups=[]
    for row in ink:
        if not groups or row[0]!=groups[-1][-1][0]+1:groups.append([])
        groups[-1].append(row)
    if len(groups)!=2:return None
    if any(not (8<=len(group)<=30) for group in groups):return None
    if not (4<=groups[1][0][0]-groups[0][-1][0]-1<=30):return None
    if any(left<8 or right>=gray.width-8 for _,left,right in ink):return None
    top,bottom=groups[1][0][0]-4,groups[1][-1][0]+5
    if top<=groups[0][-1][0] or bottom>=gray.height-1:return None
    box=(8,top,gray.width-8,bottom)
    return gray.crop(box),box

def evm_dotted_zero_structure(line):
    # DejaVu Sans Mono's zero has a detached central dot inside a complete
    # ring. Capital O does not. Independently require that observed structure
    # before the additional mono-font decoder: OCR alone can turn Ox into 0x.
    # Hollow/slashed/ambiguous zero fonts cannot use this fallback.
    if line.mode!="L" or not (84<=line.width<=694 and 16<=line.height<=40):return False,0
    ink={(xx,yy) for yy in range(line.height) for xx in range(line.width) if line.getpixel((xx,yy))<160}
    if not ink or len(ink)>12000:return False,0
    columns=sorted({xx for xx,_ in ink})
    spans=[]
    for xx in columns:
        if not spans or xx!=spans[-1][-1]+1:spans.append([])
        spans[-1].append(xx)
    if len(spans)>64:return False,0
    def dotted(span):
        left,right=span[0],span[-1]
        glyph={(xx,yy) for xx,yy in ink if left<=xx<=right}
        top=min(yy for _,yy in glyph);bottom=max(yy for _,yy in glyph)
        if not (2<=left and 5<=right-left+1<=24 and 8<=bottom-top+1<=30 and top>=2 and bottom<line.height-2):return False
        if right+2>=line.width:return False
        components=[]
        remaining=set(glyph)
        while remaining:
            if len(components)>=2:return False
            pending=[remaining.pop()];component=set(pending)
            while pending:
                xx,yy=pending.pop()
                for neighbour in ((xx-1,yy),(xx+1,yy),(xx,yy-1),(xx,yy+1)):
                    if neighbour in remaining:
                        remaining.remove(neighbour);component.add(neighbour);pending.append(neighbour)
            components.append(component)
        if len(components)!=2:return False
        ring,dot=sorted(components,key=len,reverse=True)
        if not (1<=len(dot)<=16 and len(ring)>=20):return False
        if any(not (left+2<=xx<=right-2 and top+2<=yy<=bottom-2) for xx,yy in dot):return False
        if max(xx for xx,_ in dot)-min(xx for xx,_ in dot)>3 or max(yy for _,yy in dot)-min(yy for _,yy in dot)>3:return False
        # The outside background must not reach the detached dot's neighbours.
        outside={(left-1,top-1)};pending=list(outside)
        while pending:
            xx,yy=pending.pop()
            for neighbour in ((xx-1,yy),(xx+1,yy),(xx,yy-1),(xx,yy+1)):
                nx,ny=neighbour
                if left-1<=nx<=right+1 and top-1<=ny<=bottom+1 and neighbour not in glyph and neighbour not in outside:
                    outside.add(neighbour);pending.append(neighbour)
        return not any((xx+dx,yy+dy) in outside for xx,yy in dot for dx,dy in ((-1,0),(1,0),(0,-1),(0,1)))
    observed=[dotted(span) for span in spans]
    return observed[0],sum(observed[1:])

def decode_evm_contract_tooltip(picture,expected,deadline):
    if picture.mode!="L" or not (100<=picture.width<=710 and 35<=picture.height<=140):return False
    line=evm_contract_address_line(picture)
    dotted,other_dots=evm_dotted_zero_structure(line[0]) if line else (False,0)
    report["ethereumContractLine"]={"twoCompleteInkLines":line is not None,"dottedZeroPrefix":dotted,"otherDottedZeroGlyphs":other_dots}
    # In a dotted-zero font an initial hollow O cannot be reinterpreted by
    # any of the baseline OCR modes either. Other observed zero glyphs supply
    # the font-shape evidence independently of the expected address text.
    if other_dots and not dotted:return False
    # Preserve the original bounded two-line observations. The one added path
    # uses a whole single line and an independently disambiguated zero glyph.
    observations=[("block",picture,scale,psm) for scale,psm in ((1,6),(3,6),(1,7),(3,7))]
    if dotted:observations.append(("line",line[0],4,7))
    for region,crop,scale,psm in observations:
        remaining=deadline-time.monotonic()
        if remaining<=0:return False
        data=io.BytesIO()
        ImageOps.expand(crop.resize((crop.width*scale,crop.height*scale),Image.Resampling.LANCZOS),border=12 if region=="block" else 12*scale,fill=255).save(data,format="PNG")
        raw=subprocess.check_output(["tesseract","stdin","stdout","--psm",str(psm)],input=data.getvalue(),stderr=subprocess.DEVNULL,timeout=min(10,remaining))
        compact="".join(raw.decode("utf-8",errors="replace").split())
        matched=compact.lower()==expected.lower() if region=="line" else exact_evm_contract_text(raw,expected)
        report.setdefault("ethereumContractOCR",[]).append({"region":region,"scale":scale,"psm":psm,"matched":matched,"recognizedBytes":len(compact.encode()),"recognizedSHA256":hashlib.sha256(compact.encode()).hexdigest()})
        if matched:return True
    return False

def verify_evm_public_contract(point,contract,deadline):
    last=None
    while time.monotonic()<deadline:
        rows,picture,bounds=public_rows(with_picture=True,with_geometry=True)
        captured=evm_contract_tooltip_block(rows,picture,bounds,point)
        if captured:
            crop,box=captured
            last=(crop,box,bounds)
            report["ethereumContractPublicRegion"]={"bounds":list(box),"size":list(crop.size)}
            if decode_evm_contract_tooltip(crop,contract,deadline):return last
        time.sleep(min(.15,max(0,deadline-time.monotonic())))
    if last:
        # Already classified/captured under the same public X11 guard and
        # bounded by the exact public label and complete tooltip fill.
        path=REPORTS/(args.report_name+"-evm-public-contract.png")
        last[0].save(path)
        report["ethereumContractPublicRegion"]["failureArtifact"]=path.name
        report["ethereumContractPublicRegion"]["failureArtifactSHA256"]=hashlib.sha256(path.read_bytes()).hexdigest()
    return None

def public_decimal_cell(rows,picture,bounds,row_y,expected):
    headers=[row for row in rows if row[11]=="Decimals" and int(row[7])<row_y]
    if len(headers)!=1:raise RuntimeError("Unique asset precision column was not observed")
    left=int(headers[0][6])-8
    x,y,w,h=bounds
    box=(left,row_y-23,min(left+145,x+w-1),row_y+23)
    if box[0]<x or box[1]<y or box[3]>y+h:raise RuntimeError("Asset precision cell exceeds owned public bounds")
    cell=picture.crop(box).convert("L")
    for scale in (1,3):
        data=io.BytesIO();ImageOps.expand(cell.resize((cell.width*scale,cell.height*scale),Image.Resampling.LANCZOS),border=12,fill=255).save(data,format="PNG")
        raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","7"],input=data.getvalue(),stderr=subprocess.DEVNULL,timeout=10)
        if raw.decode().strip()==expected:return True
    return False

def public_words_position(words, rows=None):
    wanted=[word.lower() for word in words.split()]
    rows=public_rows() if rows is None else rows
    for index,row in enumerate(rows):
        segment=rows[index:index+len(wanted)]
        if [r[11].lower().strip("·.,:") for r in segment]!=wanted or not all(r[1:5]==row[1:5] for r in segment):continue
        left=int(segment[0][6]);right=int(segment[-1][6])+int(segment[-1][8])
        return (left+right)//2,int(row[7])+int(row[9])//2
    return None

def near_native_token_row(rows,bounds):
    # Require the actual native row inside NEAR's three-column asset table.
    # A NEAR label in tabs/headings or an unrelated Unknown cannot pass. This
    # offline mode has installed no token policy, so additional rows also fail.
    x,y,w,h=bounds
    if x<0 or y<0 or x+w>1600 or y+h>1100 or w<300 or h<200:
        raise RuntimeError("NEAR token table exceeds the owned display")
    content=[row for row in rows if x<=int(row[6]) and int(row[6])+int(row[8])<=x+w and y+145<int(row[7]) and int(row[7])+int(row[9])<y+h-31]
    heading=public_words_position("Assets on NEAR mainnet",content)
    tracking=public_words_position("Track a NEAR token",content)
    if not heading or not tracking or heading[1]>=tracking[1]:return None
    columns={word:[row for row in content if row[11]==word and heading[1]<int(row[7])<tracking[1]] for word in ("Token","Balance","Decimals")}
    if any(len(matches)!=1 for matches in columns.values()):return None
    headers={word:matches[0] for word,matches in columns.items()}
    if max(int(row[7]) for row in headers.values())-min(int(row[7]) for row in headers.values())>8:return None
    if not int(headers["Token"][6])<int(headers["Balance"][6])<int(headers["Decimals"][6]):return None
    table_top=max(int(row[7])+int(row[9]) for row in headers.values())
    symbols=[row for row in content if table_top<int(row[7])<tracking[1]-15 and int(headers["Token"][6])-12<=int(row[6])<int(headers["Balance"][6])-20]
    if len(symbols)!=1 or symbols[0][11]!="NEAR":return None
    native=symbols[0]
    cy=int(native[7])+int(native[9])//2
    balances=[row for row in content if row[11]=="Unknown" and int(headers["Balance"][6])-12<=int(row[6])<int(headers["Decimals"][6])-15 and abs(int(row[7])+int(row[9])//2-cy)<=12]
    return cy if len(balances)==1 else None

def wait_near_token_import_failure(seconds=45):
    deadline=time.monotonic()+seconds
    while time.monotonic()<deadline:
        if app is not None and app.poll() is not None:
            raise RuntimeError("Owned wallet exited during offline NEAR token inspection")
        if desktop.secret_window():
            # Escape only in our owned main window; no unexpected approval is
            # accepted or authenticated, and no private pixels are persisted.
            desktop.focus(desktop.wait("CryptoWallet")[0])
            desktop.key("Escape")
            raise RuntimeError("Offline NEAR token inspection unexpectedly requested a dialog; cancelled")
        visible=text()
        if "Prepared payment" in visible or "Original prepared token registration" in visible:
            raise RuntimeError("Read-only NEAR token inspection prepared a payment or registration")
        if public_status_contains("Request failed or is unavailable."):return
        time.sleep(.1)
    raise RuntimeError("Offline NEAR token metadata failure was not shown within its bounded deadline")

def near_tokens():
    stage("Open NEAR native wallet before exact-contract token tracking")
    click("Main account",top=True)
    scroll_public(4,35)
    wait_near_navigation("wallet")
    expected_address="5510e2b44cae6eb807e3e0e45d579dda058c274abcba15e5cb84636f5d1ee412"
    if request_receive_copy()!=expected_address:
        raise RuntimeError("NEAR receive address differs from the independent public BIP39 fixture")
    stage("Verify native NEAR table, precision and exact-contract import controls")
    scroll_public(4,35)
    wait(lambda:public_words_position("Track a NEAR token"))
    rows,picture,bounds=public_rows(contrast=True,with_picture=True,with_geometry=True)
    row_y=near_native_token_row(rows,bounds)
    if row_y is None or not public_decimal_cell(rows,picture,bounds,row_y,"24"):
        raise RuntimeError("Offline NEAR assets did not show only native NEAR, Unknown balance and exact 24 decimals")
    if not all(public_words_position(label,rows) for label in ("Token contract account on NEAR mainnet","Verify and add token")):
        raise RuntimeError("NEAR exact-contract token input or import action is missing")
    passed("Actual NEAR wallet shows its exact owned address/copy, native NEAR asset row with Unknown balance and 24 decimals, and contract-only token tracking controls")
    def contract_field(value):
        position=scroll_to_public_words("Token contract account on NEAR mainnet")
        if desktop.secret_window():raise PrivateWindowVisible("Unexpected dialog before public NEAR contract entry")
        desktop.click(position[0],position[1]+40)
        desktop.key("a",ctrl=True)
        desktop.type(value)
        time.sleep(.25)
    stage("Reject a URL as a NEAR token contract before any private confirmation")
    contract_field("https://invalid.near")
    click("Verify and add token")
    wait(lambda:public_status_contains("Enter the exact NEAR token contract account for this network."))
    if desktop.secret_window():raise RuntimeError("Invalid NEAR token URL opened a private approval")
    passed("Actual NEAR token tracking rejects a URL with actionable contract-account feedback and no approval dialog")
    stage("Request real offline metadata for an exact NEAR contract without fabricating an import")
    contract_field("wrap.near")
    click("Verify and add token")
    wait_near_token_import_failure()
    scroll_public(4,35)
    rows,picture,bounds=public_rows(contrast=True,with_picture=True,with_geometry=True)
    row_y=near_native_token_row(rows,bounds)
    if row_y is None or not public_decimal_cell(rows,picture,bounds,row_y,"24"):
        raise RuntimeError("Failed offline token inspection installed an asset or corrupted native NEAR precision/balance")
    if request_receive_copy()!=expected_address:
        raise RuntimeError("Failed token import changed the owned NEAR receive address")
    report["nearTokenPresentation"]={"network":"near-mainnet","assets":["NEAR"],"decimals":[24],"receiveAddressSHA256":hashlib.sha256(expected_address.encode()).hexdigest(),"invalidSelectorRejected":True,"offlineContract":"wrap.near","offlineMetadataFailure":True,"tokenInstalled":False,"preparedOrSent":False}
    passed("A real offline NEAR token metadata request fails honestly without installing a token, inventing a balance, changing the owned address or requesting spending approval")
    stage("Verify ordinary native NEAR Send units before staking")
    click("Send")
    wait(lambda:"Amount (1 means 1 NEAR)" in text())
    if desktop.secret_window() or "Prepared payment" in text():
        raise RuntimeError("Opening ordinary NEAR Send requested approval or prepared a payment")
    click("Send")
    wait(lambda:"Recipient address" not in text())
    passed("Native NEAR Send retains whole-NEAR units and remains an unprepared ordinary form")
    staking_page()

def scroll_to_public_words(words, contrast=False):
    for _ in range(12):
        position=public_words_position(words,public_rows(contrast=contrast))
        if position:return position
        scroll_public(5,3)
    raise RuntimeError("Expected staking control or disclosure missing: "+words)

def near_navigation_state(rows):
    # Inspect only fixed public markers, never retain the OCR payload. A Copy
    # address button on another wallet is insufficient: require NEAR's own
    # heading and its Wallet/Staking pair on one content-navigation row.
    _,main_y,_,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
    content=[row for row in rows if int(row[7])>main_y+145]
    visible=" ".join(row[11] for row in content)
    lines={}
    for row in content:
        lines.setdefault(tuple(row[1:5]),[]).append(row[11])
    # The displayed middle dot is commonly recognized as a dash. Ignore
    # punctuation only, preserving all three exact words on one OCR line.
    heading=any(re.findall(r"[a-z]+"," ".join(line).lower())==["near","mainnet","near"]
                for line in lines.values())
    wallet=public_words_position("Wallet",content)
    staking=public_words_position("Staking",content)
    state={"mainAccount":"Account group" in visible,
           "nearHeading":heading,
           "copyAddress":public_words_position("Copy address",content) is not None,
           "nearSections":bool(wallet and staking and abs(wallet[1]-staking[1])<=8),
           "validator":public_words_position("AstroStakers",content) is not None,
           "contract":public_words_position("astro-stakers.poolv1.near",content) is not None}
    state["wallet"]=state["nearHeading"] and state["copyAddress"] and state["nearSections"]
    state["staking"]=state["wallet"] and state["validator"] and state["contract"]
    return state

def near_contract_crop(rows,picture):
    # Locate the read-only public address below its unique Exact contract
    # label, not the editable pool field or the owned receive address. Rows and
    # pixels come from one immutable, guard-checked public capture.
    window,_=desktop.wait("CryptoWallet")
    x,y,w,h=desktop.geometry(window)
    matches=[]
    for index,row in enumerate(rows[:-1]):
        second=rows[index+1]
        if row[11]=="Exact" and second[11]=="contract" and row[1:5]==second[1:5] and int(row[7])>y+145:
            matches.append((row,second))
    if len(matches)>1:raise RuntimeError("Public staking contract label was ambiguous")
    if not matches:return None
    first,last=matches[0]
    if int(first[6])<x+32 or int(last[6])+int(last[8])>x+w-32:return None
    left=max(x+32,int(first[6])-3)
    top=max(int(row[7])+int(row[9]) for row in (first,last))+2
    bounds=(left,top,min(left+523,x+w-32),top+52)
    if bounds[2]<=bounds[0] or bounds[3]>y+h-33 or bounds[2]>picture.width or bounds[3]>picture.height:return None
    return picture.crop(bounds),bounds

def decode_near_contract(picture,deadline):
    # A whole-display sparse-text pass can misread poolv1 as poolvl on the
    # Ubuntu24 Tesseract/font combination. Decode the isolated actual public
    # line, without a character whitelist, supplied dictionary or 1/l rewrite.
    # Only whitespace segmentation may change; every other byte must match.
    expected="astro-stakers.poolv1.near"
    gray=picture.convert("L")
    if appearance["theme"]=="dark":gray=ImageOps.invert(gray)
    for scale in (1,3):
        remaining=deadline-time.monotonic()
        if remaining<=0:return False
        prepared=gray.resize((gray.width*scale,gray.height*scale),Image.Resampling.LANCZOS)
        prepared=ImageOps.expand(prepared,border=12*scale,fill=255)
        data=io.BytesIO();prepared.save(data,format="PNG")
        raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","7"],input=data.getvalue(),stderr=subprocess.DEVNULL,timeout=min(10,remaining))
        candidate="".join(raw.decode("utf-8",errors="replace").split())
        matched=candidate==expected
        report.setdefault("nearContractOCR",[]).append({"scale":scale,"psm":7,"matched":matched,"recognizedBytes":len(candidate.encode()),"recognizedSHA256":hashlib.sha256(candidate.encode()).hexdigest()})
        if matched:return True
    return False

def verify_near_public_contract(deadline):
    # Navigation has already reached this exact NEAR/Staking page. Move only
    # its public scroll position if the read-only line is below the viewport;
    # do not re-click Staking or any transaction control while decoding it.
    for _ in range(4):
        if time.monotonic()>=deadline:return False
        rows,picture=public_rows(with_picture=True)
        captured=near_contract_crop(rows,picture)
        if captured:
            line,bounds=captured
            report["nearContractPublicRegion"]={"bounds":list(bounds),"size":list(line.size)}
            if decode_near_contract(line,deadline):return True
            # This immutable crop was classified/captured before OCR under the
            # same X11 guard. It contains only the anchored public contract
            # line, never a whole window, address table or private prompt.
            path=REPORTS/(args.report_name+"-near-public-contract.png")
            line.save(path)
            report["nearContractPublicRegion"]["failureArtifact"]=path.name
            report["nearContractPublicRegion"]["failureArtifactSHA256"]=hashlib.sha256(path.read_bytes()).hexdigest()
            return False
        scroll_public(5,2)
    return False

def wait_near_navigation(target, seconds=45):
    # Only these two idempotent public navigation actions may be repeated.
    # An asynchronous balance snapshot can supersede a pointer event; inspect
    # the actual resulting page before another attempt. This helper cannot
    # prepare, approve, refresh, sign, send, trade or withdraw anything.
    if target not in ("wallet","staking"):
        raise RuntimeError("Unexpected read-only NEAR navigation target")
    deadline=time.monotonic()+seconds
    next_click=0
    observed=report.setdefault("nearNavigation",{}).setdefault(target,{"attempts":0})
    while time.monotonic()<deadline:
        if app is not None and app.poll() is not None:
            raise RuntimeError("Owned native wallet exited during NEAR navigation")
        if desktop.secret_window():
            raise PrivateWindowVisible("Unexpected private prompt during read-only NEAR navigation")
        rows=public_rows()
        state=near_navigation_state(rows)
        observed["observed"]=state
        save()
        if target=="staking" and state["wallet"] and state["validator"]:
            stage("Verify exact read-only public NEAR staking contract")
            state["contract"]=verify_near_public_contract(deadline)
            state["staking"]=state["contract"]
            save()
            if not state["contract"]:
                raise RuntimeError("Complete public NEAR staking contract could not be verified exactly")
            return
        if state[target]:return
        # Never select Staking until the exact NEAR Wallet is observed. Never
        # select another wallet or act on an unexpected page with its address.
        allowed=state["mainAccount"] and not state["copyAddress"] if target=="wallet" else state["wallet"]
        if allowed and time.monotonic()>=next_click:
            _,main_y,_,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
            content=[row for row in rows if int(row[7])>main_y+145]
            point=public_words_position("NEAR" if target=="wallet" else "Staking",content)
            if point:
                # One pointer event from this guarded observation, not click()'s
                # independent multi-OCR retry loop inside another deadline.
                if desktop.secret_window():
                    raise PrivateWindowVisible("Unexpected private prompt before NEAR navigation input")
                desktop.focus(desktop.wait("CryptoWallet")[0])
                desktop.click(*point)
                observed["attempts"]+=1
                desktop.park_pointer()
                next_click=time.monotonic()+.75
        time.sleep(.1)
    raise RuntimeError("Read-only NEAR "+target+" navigation exceeded its bounded deadline")

def public_picture():
    # Apply the same atomic public-window allowlist before every appearance
    # sample. These images never leave RAM or become failure attachments.
    desktop.grab_server()
    try:
        mod.X.XSync(desktop.d,0)
        if desktop.secret_window():raise PrivateWindowVisible("Refusing public appearance pixels while a private prompt is visible")
        return desktop.image().convert("RGB")
    finally:
        desktop.ungrab_server()

def observe_appearance(name, private_prefix=None):
    if private_prefix:
        window,_=desktop.wait(private_prefix,30)
        private_primary(private_prefix)
        x,y,w,h=desktop.prompt_geometry(private_prefix)
        picture=desktop.image().convert("RGB")
        observed={"surface":picture.getpixel((x+w//2,y+8))}
    else:
        window,_=desktop.wait("CryptoWallet",30)
        x,y,w,h=desktop.geometry(window)
        picture=public_picture()
        observed={"page":picture.getpixel((x+8,y+h-65)),"chrome":picture.getpixel((x+w-8,y+15))}
    if any(color!=palette()[key] for key,color in observed.items()):
        raise RuntimeError("Actual wallet colors did not match selected presentation preferences")
    row={"name":name,"theme":appearance["theme"],"fontSize":appearance["fontSize"],"colors":{key:list(value) for key,value in observed.items()}}
    if not private_prefix:
        words=public_rows()
        brand=next((row for row in words if row[11]=="CryptoWallet" and int(row[7])<y+77),None)
        if not brand:raise RuntimeError("Actual public brand font could not be measured")
        row["brandGlyphHeight"]=int(brand[9])
    report.setdefault("appearanceObservations",[]).append(row)
    return row

def assert_selected_subtab(selected, other):
    rows=public_rows()
    # Settings tabs are below the top-level navigation. Exclude headings and
    # labels elsewhere by requiring the common tab row, independently located
    # from the complete selected/other texts in public OCR.
    _,main_y,_,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
    candidates={}
    for title in (selected,other):
        matching=[row for row in rows if row[11]==title and main_y+145<int(row[7])<main_y+240]
        if not matching:raise RuntimeError("Settings subtab was not visible: "+title)
        candidates[title]=min(matching,key=lambda row:int(row[7]))
    if abs(int(candidates[selected][7])-int(candidates[other][7]))>8:
        raise RuntimeError("Settings tab labels did not share a navigation row")
    picture=public_picture()
    colors={}
    for title,row in candidates.items():
        colors[title]=picture.getpixel((int(row[6])-9,int(row[7])+int(row[9])//2))
    report.setdefault("selectedSubtabs",[]).append({"selected":selected,"inactive":other,"theme":appearance["theme"],"colors":{key:list(value) for key,value in colors.items()}})
    if colors[selected]!=palette()["selected"] or colors[other]!=palette()["surface"]:
        raise RuntimeError("Selected Settings subtab was not visually distinct from an inactive subtab")

def choose_public_option(label, index):
    point=public_words_position(label)
    if not point:raise RuntimeError("Appearance selection label was not visible: "+label)
    desktop.click(point[0]+35,point[1]+40)
    desktop.key("Home")
    for _ in range(index):desktop.key("Down")
    desktop.key("Return")
    desktop.park_pointer()
    time.sleep(.3)

def unlock_with_enter(secret):
    stage("Unlock via Enter on public page and private password input")
    visible=text()
    if "Unlock local wallet" not in visible or "Retry storage check" in visible:
        raise RuntimeError("Locked wallet did not show the simplified Unlock page")
    desktop.focus(desktop.wait("CryptoWallet")[0])
    started=time.monotonic()
    desktop.key("Return")
    desktop.wait("Unlock local wallet",30)
    report.setdefault("unlockDialogTiming",[]).append({"visibleSeconds":round(time.monotonic()-started,3),"sameApplicationWindow":True,"sameApplicationProcess":True})
    observe_appearance("Private unlock uses persisted theme", "Unlock local wallet")
    geometry=desktop.inputs("Unlock local wallet",[secret])
    if geometry["fieldCount"]!=1 or geometry["centers"][0]+geometry["heights"][0]//2>=geometry["primary"][1]-20:
        raise RuntimeError("Private Unlock input overlapped or clipped its visible footer")
    # The input retains actual keyboard focus from Desktop.inputs. Do not click
    # the footer: this verifies the ordinary Enter form path end to end.
    desktop.key("Return")
    wait(lambda:not desktop.secret_window(),30)
    wait(lambda:"Account group" in text() and "Unlock local wallet" not in text(),30)

def appearance_campaign(installation, home, secret):
    global appearance
    stage("Verify default Light and 18 px public appearance")
    baseline=observe_appearance("Fresh default Light/18")
    click("Settings",top=True)
    click("Appearance")
    scroll_public(4,35)
    wait(lambda:"Font size" in text())
    if not re.search(r"\bLight\b",text()) or not re.search(r"\b18\s+px\b",text()):
        raise RuntimeError("Fresh Appearance controls did not default to Light and 18 px")
    assert_selected_subtab("Appearance","Wallet")
    stage("Change actual public font to 24 px and theme to Dark")
    choose_public_option("Font size",5)
    appearance={"theme":"light","fontSize":24}
    wait(lambda:bool(re.search(r"\b24\s+px\b",text())))
    choose_public_option("Theme",1)
    appearance={"theme":"dark","fontSize":24}
    wait(lambda:"Dark" in text())
    changed=observe_appearance("Selected Dark/24")
    if changed["brandGlyphHeight"]<baseline["brandGlyphHeight"]*1.15:
        raise RuntimeError("Increasing the selected font did not enlarge actual public text")
    preferences=home/"config/cryptowallet/ui/appearance.json"
    wait(lambda:preferences.is_file() and json.loads(preferences.read_text())==appearance)
    assert_selected_subtab("Appearance","Wallet")
    stage("Inspect actual private Bybit key/secret and visible footer at 24 px")
    click("Exchange",last=True)
    wait(lambda:"Bybit connection" in text())
    assert_selected_subtab("Exchange","Wallet")
    click_primary_public("Bybit connection")
    title="Connect Bybit — enter your API key and matching API secret"
    desktop.wait(title,30)
    observe_appearance("Private Bybit uses selected theme",title)
    geometry=desktop.inputs(title,[bybit_key,bybit_secret])
    if geometry["fieldCount"]!=2 or geometry["window"][1]>850 or min(geometry["heights"])<45 or max(geometry["heights"])-min(geometry["heights"])>1 or geometry["centers"][-1]+geometry["heights"][-1]//2>=geometry["primary"][1]-20:
        raise RuntimeError("Actual Bybit API secret field or action footer was clipped at 24 px")
    desktop.accept(title)
    desktop.wait("Approve Bybit custody and API access",30)
    observe_appearance("Private custody confirmation uses selected theme","Approve Bybit custody and API access")
    # Only reach the exact consent view. Cancelling avoids credential storage,
    # API authentication/observation and all funds-moving exchange operations.
    desktop.focus(desktop.wait("Approve Bybit custody and API access")[0])
    desktop.key("Escape")
    wait(lambda:not desktop.secret_window(),30)
    passed("Actual Bybit Connect asks only masked API key/secret, keeps both fields and footer visible at 24 px, reaches exact custody confirmation without a second app password, and cancels before connecting")
    stage("Restart actual wallet and verify persisted Dark/24 before Enter unlock")
    close_gracefully()
    launch(installation,home)
    observe_appearance("Restarted locked public Dark/24")
    unlock_with_enter(secret)
    restarted=observe_appearance("Restarted unlocked public Dark/24")
    if abs(restarted["brandGlyphHeight"]-changed["brandGlyphHeight"])>1 or json.loads(preferences.read_text())!=appearance:
        raise RuntimeError("Actual restart lost the selected theme or font size")
    passed("Settings Appearance has a distinct selected subtab; Light/18 defaults change to Dark/24 in the page and in-window dialogs, survive graceful restart, and Unlock submits with Enter without Retry storage check")
    # Restore the default through the same real controls so the remainder of
    # the all-network/recovery campaign uses the candidate's initial layout.
    stage("Restore Light/18 through actual Appearance settings")
    click("Settings",top=True)
    click("Appearance")
    scroll_public(4,35)
    wait(lambda:"Font size" in text())
    choose_public_option("Theme",0)
    appearance={"theme":"light","fontSize":24}
    wait(lambda:json.loads(preferences.read_text())==appearance)
    choose_public_option("Font size",2)
    appearance={"theme":"light","fontSize":18}
    wait(lambda:json.loads(preferences.read_text())==appearance)
    wait(lambda:any(row[11]=="CryptoWallet" and int(row[9])==baseline["brandGlyphHeight"] for row in public_rows()))
    observe_appearance("Restored Light/18 through actual settings")

def ethereum_tokens(expected_address=None):
    stage("Observe actual Ethereum native and ERC-20 balance rows")
    if expected_address is None:
        click("Main account",top=True)
        scroll_public(4,35)
        click("Ethereum")
        wait(lambda:"Copy address" in text())
        expected_address=request_receive_copy()
        if not re.fullmatch(r"0x[0-9a-fA-F]{40}",expected_address):
            raise RuntimeError("Fresh Ethereum copy did not return a complete address")
    scroll_public(4,35)
    wait(lambda:"Assets on Ethereum" in text())
    rows=public_rows(contrast=True)
    visible=" ".join(row[11] for row in rows)
    report["ethereumAssetPublicOCR"]=[{"word":row[11],"line":row[1:5],"box":[int(row[index]) for index in (6,7,8,9)]} for row in rows if row[11].strip("·.,:") in ("Token","Balance","ETH","USDT","Unknown","Decimals","18","6")]
    asset_heading=public_words_position("Assets on Ethereum",rows)
    if not asset_heading:raise RuntimeError("Ethereum asset list heading was not visible")
    def asset_button(symbol,require_enabled=False):
        desktop.park_pointer()
        # Ordinary OCR can retain a clean glyph where contrast merges its
        # border into the glyph box; contrast recovers low-opacity disabled
        # labels. Both observations must independently pass identical bounds,
        # exact-symbol, balance and button-fill checks before any click.
        point=None
        for contrast in (False,True):
            observed,picture,bounds=public_rows(contrast=contrast,with_picture=True,with_geometry=True)
            point=evm_asset_button_target(symbol,observed,picture,bounds,asset_heading[1],require_enabled)
            if point is not None:break
        if point is None:raise RuntimeError("Exact Ethereum asset button was not identified independently of its balance: "+symbol)
        report.setdefault("ethereumAssetButtonTargets",[]).append({"symbol":symbol,"center":list(point),"requireEnabled":require_enabled,"contrastOCR":contrast})
        return point
    if not all(public_words_position(title,rows) for title in ("Token","Balance","Decimals")):raise RuntimeError("Ethereum assets did not have Token, Balance and Decimals table headers")
    for symbol,decimals in (("ETH","18"),("USDT","6")):
        _,button_y=asset_button(symbol)
        words=[row[11] for row in rows if abs(int(row[7])+int(row[9])//2-button_y)<20]
        cell_rows,cell_picture,cell_bounds=public_rows(contrast=True,with_picture=True,with_geometry=True)
        if "Unknown" not in words or not public_decimal_cell(cell_rows,cell_picture,cell_bounds,button_y,decimals):
            raise RuntimeError("Offline Ethereum asset row did not display its separate unknown balance: "+symbol)
    contract="0xdAC17F958D2ee523a2206206994597C13D831ec7"
    if contract.lower() in re.sub(r"\s+","",visible).lower():raise RuntimeError("Token contract is permanently visible instead of hover-only")
    point=asset_button("USDT",require_enabled=True)
    mod.T.XTestFakeMotionEvent(desktop.d,-1,*point,0);mod.X.XFlush(desktop.d)
    captured=verify_evm_public_contract(point,contract,time.monotonic()+15)
    desktop.park_pointer()
    if not captured:raise RuntimeError("Exact full USDT contract was not readable in its actual public hover tooltip")
    crop,box,bounds=captured
    _,after_picture,after_bounds=public_rows(with_picture=True,with_geometry=True)
    if after_bounds!=bounds:raise RuntimeError("Public wallet geometry changed during hover verification")
    after_crop=after_picture.crop(box).convert("L")
    data=io.BytesIO();ImageOps.expand(after_crop.resize((after_crop.width*3,after_crop.height*3),Image.Resampling.LANCZOS),border=12,fill=255).save(data,format="PNG")
    after_raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","11"],input=data.getvalue(),stderr=subprocess.DEVNULL,timeout=15)
    if after_crop.tobytes()==crop.tobytes() or exact_evm_contract_text(after_raw,contract):raise RuntimeError("Token contract tooltip remained visible after the pointer left")
    report["ethereumAssetHover"]={"exactContract":True,"contractHiddenBeforeHover":True,"dismissedAfterPointerLeave":True,"tokenTableColumns":["Token","Balance","Decimals"]}
    passed("Actual Ethereum table shows separate ETH/USDT balances and 18/6 decimals, with the exact token contract visible only on hover")
    stage("Select USDT and inspect its ordinary Send presentation")
    desktop.click(*asset_button("USDT",require_enabled=True))
    time.sleep(.3)
    scroll_public(4,35)
    stage("Verify selected Ethereum USDT heading before opening Send")
    wait(lambda:bool(re.search(r"Ethereum\s+[^\w]*USDT\b",text())))
    click("Send")
    stage("Verify whole-USDT units in its ordinary Send form")
    wait(lambda:bool(re.search(r"Amount.*1 means 1 USDT",text())))
    if desktop.secret_window() or "Prepared payment" in text():
        raise RuntimeError("Token selection or opening Send requested authority or prepared a payment")
    if request_receive_copy()!=expected_address:
        raise RuntimeError("USDT selection replaced the owned Ethereum receive address")
    click("Send")
    wait(lambda:"Recipient address" not in text())
    # The token form changes layout above the list. Re-find the list before
    # switching back; no stored coordinates survive a state transition.
    scroll_public(4,35)
    heading=scroll_to_public_words("Assets on Ethereum")
    asset_heading=heading
    desktop.click(*asset_button("ETH",require_enabled=True))
    time.sleep(.3)
    scroll_public(4,35)
    if desktop.secret_window() or "Prepared payment" in text() or "Recipient address" in text():
        raise RuntimeError("Returning to ETH retained token-send authority or an open form")
    report["ethereumTokenPresentation"]={"network":"ethereum-mainnet","assets":["ETH","USDT"],"decimals":[18,6],"tokenContract":contract,"receiveAddressSHA256":hashlib.sha256(expected_address.encode()).hexdigest(),"balanceObservation":"Unknown: no RPC service in owned network namespace","preparedOrSent":False}
    passed("Explicit USDT selection uses whole-token Send units and the same verified owned receive address, then closes Send and returns to ETH without preparing or approving a payment")
    address_book_campaign()

def public_status_contains(message):
    rows,picture,bounds=public_rows(ocr_scale=1,with_picture=True,with_geometry=True)
    x,y,w,h=bounds
    if x<0 or y<0 or x+w>picture.width or y+h>picture.height or w<60 or h<40:
        raise RuntimeError("Status feedback capture exceeds owned public bounds")
    footer=" ".join(row[11] for row in rows if int(row[7])>=y+h-31 and x+30<=int(row[6])<x+w)
    if message in footer:return True
    crop=picture.crop((x+30,y+h-30,x+w-12,y+h-1)).convert("L")
    if appearance["theme"]=="dark":crop=ImageOps.invert(crop)
    for scale in (1,2):
        data=io.BytesIO();ImageOps.expand(crop.resize((crop.width*scale,crop.height*scale),Image.Resampling.LANCZOS),border=12,fill=255).save(data,format="PNG")
        raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","7"],input=data.getvalue(),stderr=subprocess.DEVNULL,timeout=10)
        if message in " ".join(raw.decode().split()):return True
    return False

def address_book_use_matches(rows):
    matches=[]
    for index,row in enumerate(rows[:-1]):
        second=rows[index+1]
        if row[11]=="Use" and second[11]=="address" and row[1:5]==second[1:5]:
            matches.append(((int(row[6])+int(second[6])+int(second[8]))//2,int(row[7])+int(row[9])//2))
    return matches

def address_book_use_position():
    for scale in (1,2):
        matches=address_book_use_matches(public_rows(ocr_scale=scale))
        if len(matches)>1:raise RuntimeError("Disposable address book unexpectedly contains multiple selectable saved rows")
        if matches:return matches[0]
    return None

def address_book_composer_cleared():
    rows,picture,bounds=public_rows(ocr_scale=1,with_picture=True,with_geometry=True)
    names=[row for row in rows if row[11]=="Name"]
    if len(names)!=1:return False
    label=names[0];left=int(label[6])+12;center=int(label[7])+int(label[9])//2
    box=(left,center+25,left+400,center+55)
    x,y,w,h=bounds
    if box[0]<x or box[1]<y or box[2]>x+w or box[3]>y+h:raise RuntimeError("Address-book composer check exceeds owned public bounds")
    # This read-only interior excludes borders and covers the entire public
    # Disposable recipient name. Save must visibly clear its freshly filled
    # composer; an earlier identical status message alone cannot pass.
    colors=picture.crop(box).getcolors(maxcolors=1)
    return colors==[(400*30,palette()["input"])]

def address_book_recipient_loaded(recipient):
    rows=public_rows(ocr_scale=1)
    label=public_words_position("Recipient address",rows)
    # Compare the complete observed input token below its own label,
    # excluding a receive address, longer identity or repaired glyph.
    return bool(label) and any(row[11]==recipient and label[1]+20<int(row[7])<label[1]+65 for row in rows)

def address_book_campaign():
    stage("Save and load a disposable Ethereum recipient through its address-book icons")
    recipient="0x"+"2"*40
    def field(label,value):
        lines={}
        for row in public_rows():lines.setdefault(tuple(row[1:5]),[]).append(row)
        positions=[position for line in lines.values() if len(line)==len(label.split()) and (position:=public_words_position(label,line))]
        if len(positions)!=1:raise RuntimeError("Address-book input label was not uniquely visible")
        position=positions[0]
        desktop.click(position[0],position[1]+40)
        desktop.key("a",ctrl=True)
        if value:desktop.type(value)
        else:desktop.key("BackSpace")
        time.sleep(.25)
    stage("Address book: open ordinary Send and fill disposable recipient")
    click("Send")
    wait(lambda:"Recipient address" in text())
    field("Recipient address",recipient)
    # Real keyboard focus visits input, Load icon, then Save icon. These are
    # the ordinary accessible controls, with no bridge or clipboard injection.
    desktop.key("Tab");desktop.key("Tab");desktop.key("Return")
    stage("Address book: inspect scoped form after keyboard Save icon activation")
    wait(lambda:"Save address" in text() and "Wallet / network" in text())
    field("Name","Disposable recipient")
    if address_book_composer_cleared():raise RuntimeError("Initial Save did not first fill its fresh composer")
    click("Save address")
    report["addressBookSaveFeedback"]={"initialObservedImmediately":"Address saved." in text()}
    stage("Address book: inspect freshly cleared composer and unique durable saved entry")
    use=wait(lambda:address_book_composer_cleared() and "Disposable recipient" in text() and address_book_use_position())
    stage("Address book: repeat the identical create without duplicating its durable recipient")
    field("Name","Disposable recipient");field("Address",recipient)
    if address_book_composer_cleared():raise RuntimeError("Identical Save retry did not first fill its fresh composer")
    click("Save address")
    report["addressBookSaveFeedback"]["repeatObservedImmediately"]="Address saved." in text()
    wait(lambda:address_book_composer_cleared() and "Disposable recipient" in text() and address_book_use_position())
    for kind,name,address,feedback in (
        ("invalid-address","Invalid recipient","invalid-ethereum-address","Enter a valid address for the selected network."),
        ("duplicate-name","Disposable recipient","0x"+"3"*40,"A contact already uses this name in this wallet."),
        ("duplicate-recipient","Another recipient",recipient,"This address and comment are already saved in this wallet."),
    ):
        stage("Address book: reject "+kind+" with actionable feedback and retain the usable saved recipient")
        field("Name",name);field("Address",address)
        click("Save address")
        wait(lambda:public_status_contains(feedback))
        visible=text()
        if "Address book unavailable" in visible or "Disposable recipient" not in visible or not address_book_use_position():
            raise RuntimeError("Rejected address-book edit discarded its known saved row or showed a false storage failure")
    use=wait(address_book_use_position)
    desktop.click(*use);time.sleep(.25)
    stage("Address book: inspect saved recipient after Use address")
    wait(lambda:address_book_recipient_loaded(recipient))
    field("Recipient address","")
    desktop.key("Tab");desktop.key("Return")
    stage("Address book: inspect scoped entries after keyboard Load icon activation")
    use=wait(lambda:"Wallet / network" in text() and address_book_use_position())
    desktop.click(*use);time.sleep(.25)
    stage("Address book: inspect loaded recipient after Use address")
    wait(lambda:address_book_recipient_loaded(recipient))
    if desktop.secret_window() or "Prepared payment" in text():raise RuntimeError("Address-book selection requested authority or prepared a payment")
    click("Send")
    wait(lambda:"Recipient address" not in text())
    report["addressBook"]={"saved":True,"loaded":True,"identicalCreateRetry":True,"singleObservedSavedRowAfterRetry":True,"retryComposerCleared":True,"rejectFeedback":["invalid-address","duplicate-name","duplicate-recipient"],"savedRowUsableAfterReject":True,"network":"ethereum-mainnet","recipientSHA256":hashlib.sha256(recipient.encode()).hexdigest(),"preparedOrSent":False}
    passed("Recipient Save/Load icons persist and restore a scoped entry, identical Save retries succeed, and invalid/duplicate edits retain usable saved contacts with actionable feedback and no payment authority")

def staking_page():
    stage("Open actual NEAR wallet Staking page")
    click("Main account",top=True)
    scroll_public(4,35)
    wait(lambda:"Account group" in text())
    stage("Select NEAR wallet row before staking")
    wait_near_navigation("wallet")
    scroll_public(4,35)
    stage("Select NEAR inner Staking tab and verify complete validator contract")
    wait_near_navigation("staking")
    if desktop.secret_window():raise RuntimeError("Opening staking unexpectedly requested private approval")
    report["stakingValidator"]="astro-stakers.poolv1.near"
    passed("Actual NEAR inner Staking tab displays AstroStakers and its complete mainnet contract without a private prompt")
    stage("Verify unqueried staking remains unknown and actual offline query fails honestly")
    scroll_to_public_words("Staked")
    visible=text()
    if not re.search(r"Staked[: ]+Unknown",visible,re.I):raise RuntimeError("Unqueried staking amount was not shown as unknown")
    scroll_public(4,35)
    scroll_to_public_words("Refresh staking state")
    click("Refresh staking state")
    deadline=time.monotonic()+45
    while time.monotonic()<deadline:
        if desktop.secret_window():raise RuntimeError("Read-only staking refresh opened a private approval")
        visible=text()
        if "state is unavailable" in visible:break
        scroll_public(5,2)
        time.sleep(.15)
    else:raise RuntimeError("Actual offline staking query did not show unavailable state")
    if "No pool balance" not in visible:raise RuntimeError("Failed real staking query did not disclaim verified pool balance")
    scroll_public(4,35)
    scroll_to_public_words("Staked")
    if not re.search(r"Staked[: ]+Unknown",text(),re.I):raise RuntimeError("Failed real staking query invented a staked balance")
    passed("Actual RPC request with no service available leaves pool balance unknown and reports unavailable instead of mocked success or zero")
    stage("Verify staking whole-NEAR amount and disabled unknown-state withdrawal")
    withdraw=scroll_to_public_words("Withdraw all",contrast=True)
    visible=text()
    if "1 means 1 NEAR" not in visible:raise RuntimeError("Staking form omitted whole-NEAR amount units")
    rows=public_rows(contrast=True)
    stake=public_words_position("Stake",rows)
    withdraw=public_words_position("Withdraw all",rows)
    if not stake or not withdraw:raise RuntimeError("Staking action row could not be located independently")
    # Assert the actual rendered disabled style, independently of application
    # DOM/bridge state. Then attempt only the disabled unknown-state control;
    # an enabled preparation/confirmation button is never clicked in this drill.
    desktop.grab_server()
    try:
        mod.X.XSync(desktop.d,0)
        if desktop.secret_window():raise PrivateWindowVisible("Refusing staking public pixels while a private prompt is visible")
        picture=desktop.image().convert("RGB")
    finally:
        desktop.ungrab_server()
    def background(point):
        x,y=point
        return Counter(picture.getpixel((x+dx,y+dy)) for dx in range(-8,9) for dy in range(11,15)).most_common(1)[0][0]
    enabled,disabled=background(stake),background(withdraw)
    expected_enabled,expected_disabled=palette()["button"],palette()["disabled"]
    if max(abs(a-b) for a,b in zip(enabled,expected_enabled))>2 or max(abs(a-b) for a,b in zip(disabled,expected_disabled))>2:
        report["stakingButtonBackgroundRGB"]={"stake":list(enabled),"withdraw":list(disabled)}
        raise RuntimeError("Actual staking Withdraw did not have the disabled appearance while pool state was unknown")
    report["stakingButtonBackgroundRGB"]={"stake":list(enabled),"withdraw":list(disabled)}
    desktop.click(*withdraw)
    time.sleep(.6)
    if desktop.secret_window():raise RuntimeError("Unknown-state Withdraw opened a private approval")
    if "Prepared staking operation" in text():raise RuntimeError("Unknown-state Withdraw prepared a transaction")
    passed("Actual staking form uses whole NEAR and its disabled Withdraw remains inert when pool state is unknown")
    stage("Return from staking to ordinary NEAR Wallet and all-network table")
    scroll_public(4,35)
    click("Wallet")
    wait(lambda:"Send" in text() and "Receive" in text())
    if "Validator:" in text():raise RuntimeError("Wallet section retained staking controls")
    click("Main account",top=True)
    scroll_public(4,35)
    wait(lambda:"Account group" in text() and "Ethereum" in text())
    passed("Returning from Staking restores the ordinary NEAR Wallet and all-network overview without preparing or approving a transaction")

save()
try:
    binary = args.binary.resolve()
    if not binary.is_file():
        raise RuntimeError("Build the Wails Go executable first")
    probe = subprocess.run(["unshare", "--user", "--map-current-user", "--keep-caps", "--net", "--mount", "/bin/sh", "-c", "mount --make-rprivate / && id -u"], capture_output=True, text=True, timeout=10)
    if probe.returncode or probe.stdout.strip() == "0":
        raise RuntimeError("Private network namespace preserving ordinary UID unavailable")
    report["artifact"] = {"version": subprocess.check_output([str(binary), "--version"], text=True, timeout=10).strip(), "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "runtime": subprocess.check_output([str(binary), "--runtime-info"], text=True, timeout=10).strip()}
    if report["artifact"]["runtime"] != "wails-webkitgtk4.1; portable-go; sandbox=none":
        raise RuntimeError("Acceptance candidate is not the sandbox-free Wails Go app")
    server = shutil.which("Xvfb") or str(ROOT / ".tools/xvfb/root/usr/bin/Xvfb")
    display = next((f":{n}" for n in range(180,190) if not pathlib.Path(f"/tmp/.X11-unix/X{n}").exists() and not pathlib.Path(f"/tmp/.X{n}-lock").exists()), None)
    if display is None:
        raise RuntimeError("No unused private test display")
    base_environment = dict(os.environ, DISPLAY=display, GDK_BACKEND="x11")
    for name in ("WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"):
        base_environment.pop(name, None)
    for name in ("HTTP_PROXY","HTTPS_PROXY","ALL_PROXY","http_proxy","https_proxy","all_proxy"):
        base_environment.pop(name,None)
    if ".tools/xvfb" in server:
        base_environment["LD_LIBRARY_PATH"] = str(ROOT / ".tools/xvfb/root/usr/lib/x86_64-linux-gnu")
    os.environ.update(DISPLAY=display, GDK_BACKEND="x11")
    with tempfile.TemporaryDirectory(prefix="cryptowallet-native-acceptance-") as temporary, tempfile.TemporaryFile() as display_log:
        folder = pathlib.Path(temporary)
        hosts_fixture=folder/"hosts"
        hosts_fixture.write_text("127.0.0.1 localhost\n::1 localhost\n127.0.0.1 ethereum-rpc.publicnode.com rpc.hyperliquid.xyz mainnet.base.org api.hyperliquid.xyz rpc.mainnet.near.org api.mainnet.aptoslabs.com toncenter.com\n")
        hosts_fixture.chmod(0o600)
        report["networkIsolation"]="Owned user/mount/network namespace; pinned public RPC hostnames resolve to the unreachable test-only loopback; no RPC service, mocked response or host /etc modification; namespace capabilities dropped before wallet exec."
        installations = [folder / "machine-a/app", folder / "machine-b/app"]
        for install in installations:
            install.mkdir(parents=True)
            shutil.copy2(binary, install / "cryptowallet")
            if sorted(p.name for p in install.iterdir()) != ["cryptowallet"]:
                raise RuntimeError("Portable installation contains a runtime/helper")
        xvfb = subprocess.Popen([server, display, "-screen", "0", "1600x1100x24", "-nolisten", "tcp"], env=base_environment, stdout=display_log, stderr=display_log)
        wait(lambda: pathlib.Path("/tmp/.X11-unix/X"+display[1:]).exists(), 10)
        desktop = Desktop(display.encode())
        if args.quick_import or args.staking_only or args.tokens_only or args.near_tokens_only or args.decred_copy_only or args.appearance_only or args.main_row_copy_only or args.recovery_layout_only or args.clipboard_only:
            recorded=b" ".join([b"abandon"]*23+[b"art"]) if args.recovery_layout_only else b"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
            home=folder/"machine-a/home"
            launch(installations[0],home)
            observe_appearance("Fresh isolated profile default Light/18")
            click("Restore from recovery phrase")
            desktop.wait("Create local wallet",30)
            desktop.inputs("Create local wallet",[different_password,different_password,recorded.decode(),""])
            desktop.accept("Create local wallet")
            desktop.wait("Imported account recovery limits",30)
            desktop.accept("Imported account recovery limits")
            wait(lambda:not desktop.secret_window())
            wait(lambda:"Main account" in text())
            if args.recovery_layout_only:
                overview_layout()
                click("Settings",top=True)
                verify_storage_location(home)
                backup_phrase(recorded,different_password)
            if args.staking_only:
                staking_page()
            if args.tokens_only:
                ethereum_tokens()
            if args.near_tokens_only:
                near_tokens()
            if args.decred_copy_only:
                for _ in range(3):
                    addresses(("Decred",))
                passed("Three repeated actual Decred direct copies completed with fresh native clipboard acknowledgments")
            if args.appearance_only:
                appearance_campaign(installations[0],home,different_password)
            if args.main_row_copy_only:
                observed=main_row_copy("0x9858EfFD232B4033E47d90003D41EC34EcaEda94")
                report["mainRowCopy"]["matchesIndependentFixture"]=True
                passed("Actual main-row icon copies the exact independently derived public-fixture Ethereum address")
            if args.clipboard_only:
                observed=main_row_copy("0x9858EfFD232B4033E47d90003D41EC34EcaEda94")
                clipboard_campaign(observed,different_password)
            close_gracefully()
            passed("Targeted public-fixture import reaches all-wallet overview and closes successfully")
            if args.tokens_only:
                stage("Address book: reopen the existing disposable encrypted profile after graceful exit")
                launch(installations[0],home)
                unlock_with_enter(different_password)
                click("Ethereum")
                wait(lambda:"Copy address" in text())
                click("Send")
                label=wait(lambda:public_words_position("Recipient address"))
                desktop.click(label[0],label[1]+40)
                desktop.key("Tab");desktop.key("Return")
                use=wait(lambda:"Disposable recipient" in text() and "ethereum-mainnet" in text() and address_book_use_position())
                desktop.click(*use);time.sleep(.25)
                wait(lambda:address_book_recipient_loaded("0x"+"2"*40))
                if desktop.secret_window() or "Prepared payment" in text():raise RuntimeError("Restarted address-book selection requested payment authority")
                report["addressBook"]["restoredAfterRestart"]=True
                close_gracefully()
                passed("Existing disposable encrypted profile retains its saved address through actual exit/restart/Unlock and fills the exact recipient without payment approval")
            if args.quick_import:
                launch(installations[0],home)
                unlock_with_enter(different_password)
                close_gracefully()
                passed("Targeted restart starts locked, unlocks through its actual masked prompt and exits successfully")
        else:
            launch(installations[0], folder / "machine-a/home")
            observe_appearance("Fresh isolated profile default Light/18")
            click_primary_public("Create your app password")
            stage("Enter two masked creation password fields")
            desktop.wait("Create local wallet", 30)
            desktop.inputs("Create local wallet", [password,password])
            stage("Accept private creation password")
            desktop.accept("Create local wallet")
            stage("Read generated twelve-word phrase")
            recorded = record_phrase()
            wait(lambda: not desktop.secret_window(), 30)
            wait(lambda: "Main account" in text(), 30)
            passed("Single Go executable generates a random phrase after masked app-password entry and verifies recorded words in dialogs inside the same main application window")
            overview_layout()
            staking_page()
            main_row_address=main_row_copy()
            clipboard_campaign(main_row_address,password)
            expected = addresses()
            if main_row_address!=expected.get("Ethereum"):
                raise RuntimeError("Main-row copy differed from the independently opened Ethereum wallet")
            report["mainRowCopy"]["matchesOpenedWallet"]=True
            if len(expected) != 8:
                raise RuntimeError("Native account table lost network coverage")
            report["walletsCompared"] = len(expected)
            report["walletAddressesSHA256"] = hashlib.sha256(json.dumps(expected,sort_keys=True).encode()).hexdigest()
            passed("All eight supported network wallets open and copy their full verified single-line addresses")
            passed("Middle-click closes wallet tabs and returns to the main account")
            click("Receive",last=True)
            desktop.wait("Verify receive address", 30)
            desktop.accept("Verify receive address")
            wait(lambda: not desktop.secret_window())
            passed("WebKitGTK Receive opens the Go-authored address and QR dialog")
            click("Send")
            wait(lambda: "Amount" in text())
            if "Scan local QR" in text():
                raise RuntimeError("Removed QR scan returned to send form")
            passed("Send stays hidden until requested and uses whole-asset Amount wording")
            click("Settings",top=True)
            verify_storage_location(folder / "machine-a/home")
            backup_phrase(recorded,password)
            appearance_campaign(installations[0],folder/"machine-a/home",password)
            close_gracefully()
            launch(installations[1], folder / "machine-b/home")
            click("Restore from recovery phrase")
            desktop.wait("Create local wallet",30)
            desktop.inputs("Create local wallet",[different_password,different_password,recorded.decode(),""])
            desktop.accept("Create local wallet")
            desktop.wait("Imported account recovery limits",30)
            desktop.accept("Imported account recovery limits")
            wait(lambda:not desktop.secret_window(),30)
            wait(lambda:"Main account" in text(),30)
            if addresses()!=expected:
                raise RuntimeError("Fresh native app restored different wallet addresses")
            passed("Fresh independent app copy restores the same eight wallets from its recorded phrase and a different local app password")
            close_gracefully()
            # Reuse the isolated home only after an actual app exit.
            launch(installations[1],folder/"machine-b/home")
            unlock_with_enter(different_password)
            passed("WebKitGTK restart begins locked and unlocks the restored encrypted profile")
            close_gracefully()
            passed("All four public window closes exit successfully within ten seconds without termination or force-kill")
        for log in logs:
            log.seek(0)
            output=log.read()
            if any(value in output for value in (password.encode(),different_password.encode(),recorded,bybit_key.encode(),bybit_secret.encode())):
                raise RuntimeError("Synthetic private canary escaped to process logs")
        report["status"]="PASS"
except Exception as exc:
    # No exception payload from OCR, GTK input or subprocess transport is logged.
    report.update(status="FAIL",reason=str(exc) if isinstance(exc,RuntimeError) else "Native acceptance failed; private diagnostics withheld")
    if "desktop" in globals():
        report["ownedWindowTitles"]=[title for _,title in desktop.windows()]
        report["ownedWindowMetadata"]=desktop.window_metadata()
finally:
    stop()
    if xvfb is not None:
        xvfb.terminate()
        try: xvfb.wait(timeout=5)
        except subprocess.TimeoutExpired: xvfb.kill();xvfb.wait(timeout=5)
    for log in logs: log.close()
    recorded=None
    report["sourceStable"] = source_identity() == report["sourceSHA256"]
    if report["status"] == "PASS" and not report["sourceStable"]:
        report.update(status="FAIL", reason="Source changed during acceptance; rerun the current candidate")
    save()
    print(json.dumps(report,indent=2))
raise SystemExit(0 if report["status"]=="PASS" else 1)
