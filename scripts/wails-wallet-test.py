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
mode.add_argument("--decred-copy-only", action="store_true", help="Targeted repeated Decred receive-copy diagnostics after disposable public-fixture import; no spending or public RPC")
mode.add_argument("--appearance-only", action="store_true", help="Targeted actual appearance persistence, Enter unlock and private Bybit credential layout; cancels custody confirmation without connecting")
mode.add_argument("--main-row-copy-only", action="store_true", help="Targeted actual main-account Ethereum row icon and verified receive/copy using a public disposable phrase; no wallet opening, RPC service or payment")
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
    report["scope"]="Targeted actual Ethereum ETH/USDT wallet presentation on an isolated display after importing the public disposable BIP39 fixture. Public pixels verify separate unknown balances, the complete USDT contract, decimals, explicit selection, whole-USDT Send units and the unchanged owned receive address. No RPC response is mocked and no token is imported, transaction prepared, approved or sent. This partial mode is not full wallet/recovery acceptance."
    report["notRun"].extend(["Generated-root setup, backup, fresh-copy restore, all-eight-wallet comparison and restart/unlock (tokens-only mode)", "Custom token metadata import and funded token execution (offline graphical mode)"])
if args.decred_copy_only:
    report["scope"]="Targeted repeated actual Decred verified receive/copy on an isolated display after importing a public disposable BIP39 fixture; private windows remain excluded from public pixels and failure diagnostics contain only X11 window metadata. No payment, public RPC or complete wallet acceptance claim."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (Decred copy diagnostic mode)")
if args.appearance_only:
    report["scope"]="Targeted actual Light/18 px defaults, selected Settings tabs, Dark/24 px rendering and persistence after graceful restart, Enter-only unlock, and private two-field Bybit/footer layout. Disposable public fixture and API canaries only; custody confirmation is cancelled before encrypted credentials or any Bybit observation. No private pixels or values are retained."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (appearance-only mode)")
if args.main_row_copy_only:
    report["scope"]="Targeted actual Ethereum main-account copy icon, measured public button geometry and real Go-authored receive approval using the public disposable abandon/about phrase. The icon must keep the account overview open and copy the exact independent fixture address. No RPC response, private phrase or transaction is mocked."
    report["notRun"].append("Generated-root setup, backup and exact eight-wallet address comparison (main-row-copy-only mode)")
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
        paths.extend(path for path in directory.rglob("*") if path.is_file() and "node_modules" not in path.parts and path.suffix in (".go", ".c", ".h", ".tsx", ".ts", ".css", ".html", ".js", ".json"))
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
    rows=[yy for yy in range(y,y+h-75) if all(picture.getpixel((xx,yy))==color for xx in (x+40,x+w//2,x+w-40))]
    groups=[]
    for yy in rows:
        if not groups or yy!=groups[-1][-1]+1:groups.append([])
        groups[-1].append(yy)
    return [(group[0],group[-1]) for group in groups if 20<=len(group)<=80]

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
        # the 1440x900 wallet and centered private prompts. Move only, without
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
        window, title = super().wait(prefix, seconds)
        if args.instrumented and title != "CryptoWallet" and not title.startswith("CryptoWallet · "):
            actual, format, count, remaining, data = C.c_ulong(), C.c_int(), C.c_ulong(), C.c_ulong(), C.c_void_p()
            atom = mod.X.XInternAtom(self.d, b"_NET_WM_PID", 0)
            status = mod.X.XGetWindowProperty(self.d, window, atom, 0, 1, 0, 6, C.byref(actual), C.byref(format), C.byref(count), C.byref(remaining), C.byref(data))
            try:
                if status != 0 or actual.value != 6 or format.value != 32 or count.value != 1 or not data.value:
                    raise RuntimeError("Owned private window did not expose its process identity")
                pid = C.cast(data, C.POINTER(C.c_ulong))[0]
                if pid <= 1 or pid in report.get("processIDs", []):
                    raise RuntimeError("Private window did not belong to a distinct prompt process")
                processes = report.setdefault("privateProcessIDs", [])
                if pid not in processes:
                    processes.append(pid)
            finally:
                if data.value:
                    mod.X.XFree(data)
        return window, title
    def image(self):
        # Private image remains in memory, including during disposable phrase OCR.
        p = mod.X.XGetImage(self.d, self.root, 0, 0, 1600, 1100, 0xffffffff, 2)
        try:
            data = C.string_at(p.contents.data, p.contents.bytes_per_line * 1100)
            return Image.frombytes("RGB", (1600, 1100), data, "raw", "BGRX", p.contents.bytes_per_line, 1)
        finally:
            mod.X.XDestroyImage(p)
    def secret_window(self):
        # Deny every unexpected mapped window, including newly added private
        # prompt titles. Public OCR is allowed only for the ordinary portfolio.
        return any(title != "CryptoWallet" and not title.startswith("CryptoWallet · ") for _, title in self.windows())
    def accept(self, prefix):
        window,_=self.wait(prefix,30)
        self.focus(window)
        x,y=private_primary(prefix)
        report.setdefault("privateActionGeometry",[]).append({"title":prefix,"window":self.geometry(window),"point":[x,y]})
        self.click(x,y)
    def inputs(self, prefix, values):
        window,_=self.wait(prefix,30)
        self.focus(window)
        primary=private_primary(prefix)
        time.sleep(.2)
        x,y,w,h=self.geometry(window)
        picture=self.image().convert("RGB")
        rectangles=masked_field_rectangles(picture,(x,y,w,h),palette()["input"])
        centers=[(top+bottom)//2 for top,bottom in rectangles]
        if len(centers)!=len(values):raise RuntimeError("Private masked input rectangles did not match expected field count: "+str(len(centers)))
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
        window,_=desktop.wait(prefix,30);x,y,w,h=desktop.geometry(window)
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

def public_rows(contrast=False, with_picture=False, with_geometry=False):
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
    picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).save(data, format="PNG")
    raw = subprocess.check_output(["tesseract", "stdin", "stdout", "--psm", "11", "tsv"], input=data.getvalue(), stderr=subprocess.DEVNULL)
    rows = []
    for line in raw.decode().splitlines()[1:]:
        fields = line.split("\t")
        if len(fields) == 12 and fields[11].strip():
            for position in (6,7,8,9):fields[position]=str(int(fields[position])//2)
            rows.append(fields)
    if with_geometry:return rows,captured,geometry
    return (rows,captured) if with_picture else rows

def private_rows(prefix):
    window,_=desktop.wait(prefix,30)
    desktop.focus(window)
    x,y,w,h=desktop.geometry(window)
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
    if (prefix,words)!=("Back up recovery phrase","Copy phrase"):
        raise RuntimeError("Unexpected private control request")
    wanted=words.lower().split()
    for _ in range(8):
        rows=private_rows(prefix)
        for i,row in enumerate(rows):
            segment=rows[i:i+len(wanted)]
            if [r[11].lower().strip("·.,:") for r in segment]==wanted and all(r[1:5]==row[1:5] for r in segment):
                window,_=desktop.wait(prefix,30);x,y,_,_=desktop.geometry(window)
                desktop.click(x+int(row[6])+int(row[8])//2,y+int(row[7])+int(row[9])//2)
                return
        # At larger fonts the authenticated backup's explanatory text and
        # explicit Copy button extend below its numbered words. Scroll only
        # this known private body's owned window; its footer stays pinned.
        window,_=desktop.wait(prefix,30);x,y,w,h=desktop.geometry(window)
        mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-18,y+h//2,0)
        for _ in range(3):
            mod.T.XTestFakeButtonEvent(desktop.d,5,1,0)
            mod.T.XTestFakeButtonEvent(desktop.d,5,0,0)
        mod.X.XFlush(desktop.d)
        time.sleep(.2)
    raise RuntimeError("Private control could not be identified; no private OCR diagnostic retained")

def numbered_phrase(prefix="Record recovery phrase"):
    # Phrase pixels and OCR remain exclusively in RAM on the owned display.
    # Only ordinals and a word count may enter a failure report.
    if prefix not in ("Record recovery phrase","Back up recovery phrase"):
        raise RuntimeError("Unexpected private phrase view")
    for _ in range(8):
        rows=private_rows(prefix)
        raw=" ".join(r[11] for r in rows).encode()
        found={int(i):word for i,word in re.findall(rb"\b(\d{1,2})[.),:]?\s+([a-z]+)\b",raw)}
        if set(found)==set(range(1,13)):
            phrase=b" ".join(found[i] for i in range(1,13))
            if valid_mnemonic(phrase):return phrase
        time.sleep(.2)
    window,_=desktop.wait(prefix,30)
    x,y,w,h=desktop.geometry(window)
    picture=desktop.image().crop((x,y,x+w,y+h)).convert("RGB")
    from PIL import ImageOps
    cell=(w-58-16)//3
    lefts=[29+i*(cell+8) for i in range(3)]
    candidates=[yy for yy in range(h-75) if all(max(abs(v-t) for v,t in zip(picture.getpixel((xx,yy)),palette()["border"]))<8 for xx in lefts)]
    groups=[]
    for yy in candidates:
        if not groups or yy!=groups[-1][-1]+1:groups.append([])
        groups[-1].append(yy)
    groups=[group for group in groups if 15<=len(group)<=60]
    if len(groups)==4:
        parsed={}
        for row,group in enumerate(groups):
            center=group[len(group)//2]
            for column,left in enumerate(lefts):
                crop=picture.crop((left+34,center-15,left+cell-7,center+15)).convert("L")
                if appearance["theme"]=="dark":crop=ImageOps.invert(crop)
                data=io.BytesIO();crop.resize((crop.width*4,crop.height*4),Image.Resampling.LANCZOS).save(data,format="PNG")
                value=subprocess.check_output(["tesseract","stdin","stdout","--psm","7","-c","tessedit_char_whitelist=abcdefghijklmnopqrstuvwxyz"],input=data.getvalue(),stderr=subprocess.DEVNULL).strip().lower()
                if re.fullmatch(rb"[a-z]+",value):parsed[row*3+column+1]=value
        if len(parsed)==12:
            phrase=b" ".join(parsed[i] for i in range(1,13))
            if valid_mnemonic(phrase):return phrase
    raise RuntimeError("Disposable private phrase OCR incomplete: "+str(len(found))+" ordinals; "+str(len(groups))+" grid rows; no words or pixels retained")

def record_phrase():
    deadline=time.monotonic()+30
    while time.monotonic()<deadline:
        if any(title.startswith("Record recovery phrase") for _,title in desktop.windows()):break
        if not desktop.secret_window():
            visible=text()
            for marker in ("Use an app password","The passwords did not match","Request cancelled","Request failed or is unavailable"):
                if marker in visible:
                    report["creationPublicResultCategory"]=marker
                    raise RuntimeError("Creation did not reach generated phrase; fixed public error category recorded")
        time.sleep(.1)
    phrase=numbered_phrase()
    desktop.accept("Record recovery phrase")
    desktop.wait("Confirm recovery phrase",30)
    words=phrase.split()
    desktop.inputs("Confirm recovery phrase",[words[i].decode("ascii") for i in (1,5,11)])
    desktop.accept("Confirm recovery phrase")
    return phrase

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
                before=rows[i-1][11].lower() if i and rows[i-1][1:5]==row[1:5] else ""
                after=rows[i+1][11].lower() if i+1<len(rows) and rows[i+1][1:5]==row[1:5] else ""
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
            desktop.click(int(row[6])+int(row[8])//2, int(row[7])+int(row[9])//2, button=button)
            time.sleep(.25)
            return
        if not top and words in ("Ethereum","Base","HyperEVM","HyperCore","NEAR","Aptos","Decred","TON") and attempt in (2,5,8):
            if desktop.secret_window():raise PrivateWindowVisible("Refusing public scrolling while a private prompt is visible")
            x,y,w,h=desktop.geometry(desktop.wait("CryptoWallet")[0])
            mod.T.XTestFakeMotionEvent(desktop.d,-1,x+w-22,y+h//2,0)
            for _ in range(4):
                mod.T.XTestFakeButtonEvent(desktop.d,5,1,0);mod.T.XTestFakeButtonEvent(desktop.d,5,0,0)
            mod.X.XFlush(desktop.d);time.sleep(.2)
        time.sleep(.15)
    raise RuntimeError("Public native control not found: " + words)

def clipboard():
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
    p = gtk.gtk_clipboard_wait_for_text(gtk.gtk_clipboard_get(gdk.gdk_atom_intern_static_string(b"CLIPBOARD")))
    if not p:
        return ""
    result = C.string_at(p).decode()
    glib.g_free(p)
    return result

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
    env = dict(base_environment, HOME=str(home), XDG_CONFIG_HOME=str(home / "config"))
    home.mkdir(mode=0o700, exist_ok=True)
    log = tempfile.TemporaryFile()
    logs.append(log)
    command=["unshare","--user","--map-current-user","--keep-caps","--net","--mount","/bin/sh","-c",'mount --make-rprivate / && mount --bind "$1" /etc/hosts && exec setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all "$2"',"cryptowallet-owned-test",str(hosts_fixture),str(installation/"cryptowallet")]
    app = subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True)
    if args.instrumented:
        report.setdefault("processIDs", []).append(app.pid)
    report["currentStep"]="Wait for initial Wails public window"
    desktop.wait("CryptoWallet", 30)
    report["currentStep"]="Wait for initial Wails public wallet state"
    wait(lambda: "Create your app password" in text() or "Your encrypted wallet is on this device" in text())

def receive_dialog_state(windows):
    # A private GTK window can be mapped under its executable name before
    # Wails assigns the verified title. It remains private throughout; this
    # classifier never permits public OCR or acceptance of that loading view.
    private = [title for _, title in windows if title != "CryptoWallet" and not title.startswith("CryptoWallet · ")]
    if not private:
        return "absent"
    if len(private) != 1:
        raise RuntimeError("Unexpected native dialog during public address copy")
    if private[0] == "Verify receive address":
        return "ready"
    if private[0] == "cryptowallet":
        return "starting"
    raise RuntimeError("Unexpected native dialog during public address copy")

def request_receive_copy(seconds=30):
    deadline = time.monotonic() + seconds
    requested = starting = False
    next_click = 0
    while time.monotonic() < deadline:
        state = receive_dialog_state(desktop.windows())
        if state == "ready":
            if not requested:
                raise RuntimeError("Unexpected receive dialog before public address request")
            return
        if state == "starting":
            if not requested:
                raise RuntimeError("Unexpected native dialog before public address request")
            starting = True
        elif starting:
            raise RuntimeError("Receive dialog closed before showing its verified address title")
        elif time.monotonic() >= next_click:
            try:
                click("Copy address")
            except PrivateWindowVisible:
                if not requested:
                    raise
            else:
                requested = True
                desktop.park_pointer()
                next_click = time.monotonic() + .75
        # Once a private loading window is visible, inspect only window titles
        # until its exact verified title appears. Never click or OCR it.
        time.sleep(.05)
    raise RuntimeError("Verified receive dialog did not appear within its bounded deadline")

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
    deadline=time.monotonic()+seconds
    requested=starting=False
    next_click=0
    desktop.park_pointer()
    while time.monotonic()<deadline:
        state=receive_dialog_state(desktop.windows())
        if state=="ready":
            if not requested:raise RuntimeError("Unexpected receive dialog before main-row address request")
            return
        if state=="starting":
            if not requested:raise RuntimeError("Unexpected native dialog before main-row address request")
            starting=True
        elif starting:
            raise RuntimeError("Main-row receive dialog closed before showing its verified title")
        elif time.monotonic()>=next_click:
            try:
                rows,picture,geometry=public_rows(with_picture=True,with_geometry=True)
                point=main_row_copy_target(rows,picture,geometry)
            except PrivateWindowVisible:
                if not requested:raise
            else:
                if point is None:
                    if report["mainRowCopy"]["controlState"]=="clipped":
                        reveals=report["mainRowCopy"].get("horizontalReveals",0)
                        if reveals>=3:raise RuntimeError("Ethereum copy icon remained clipped after bounded horizontal reveal")
                        reveal_main_row_copy(geometry)
                        report["mainRowCopy"]["horizontalReveals"]=reveals+1
                    # The actual row can be disabled while Go starts its
                    # verified prompt. Wait; never replace a pending click.
                elif desktop.secret_window():
                    if not requested:raise PrivateWindowVisible("Private prompt appeared before main-row address input")
                else:
                    if desktop.geometry(desktop.wait("CryptoWallet")[0])!=geometry:
                        raise RuntimeError("Main-row wallet geometry changed before address input")
                    desktop.focus(desktop.wait("CryptoWallet")[0])
                    desktop.click(*point)
                    requested=True
                    report.setdefault("mainRowCopy",{}).setdefault("attempts",0)
                    report["mainRowCopy"]["attempts"]+=1
                    desktop.park_pointer()
                    next_click=time.monotonic()+.75
        # A mapped private loading/ready window is inspected only by title.
        # No public pixel capture or replacement click is allowed while open.
        time.sleep(.05)
    raise RuntimeError("Main-row verified receive dialog did not appear within its bounded deadline")

def main_row_copy(expected_address=None):
    stage("Main-account Ethereum row icon and verified receive request")
    request_main_row_copy()
    stage("Main row verified receive dialog")
    desktop.wait("Verify receive address",30)
    desktop.accept("Verify receive address")
    stage("Main row copy returns to main account")
    wait(lambda:not desktop.secret_window())
    wait(lambda:"Account group" in text())
    if "Copy address" in text():raise RuntimeError("Main-row copy opened a wallet page")
    copied=wait(lambda:value if re.fullmatch(r"0x[0-9a-fA-F]{40}",value:=clipboard()) else None)
    if expected_address is not None and copied!=expected_address:
        raise RuntimeError("Main-row copy differed from the independently derived Ethereum fixture address")
    report["mainRowCopy"]["clipboardSHA256"]=hashlib.sha256(copied.encode()).hexdigest()
    passed("Main-account row copy icon opens verified receive details without opening that wallet tab")
    return copied

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
        # Balance redraws can race a test pointer click. Retry only this
        # read-only address request, and require its real native confirmation;
        # this helper is never used for signing, trading or withdrawing.
        request_receive_copy()
        # The signer independently displays its verified receive details before
        # fulfilling a public copy request; acknowledge that actual native view.
        report["currentStep"] = "Wait for " + network + " verified receive dialog"
        desktop.wait("Verify receive address",30)
        desktop.accept("Verify receive address")
        report["currentStep"] = "Wait for " + network + " public portfolio after receive dialog"
        wait(lambda:not desktop.secret_window())
        pattern = {"NEAR":r"[0-9a-f]{64}","Aptos":r"0x[0-9a-fA-F]{64}","Decred":r"Ds[1-9A-HJ-NP-Za-km-z]{32,34}","TON":r"[UE]Q[A-Za-z0-9_-]{46}"}.get(network,r"0x[0-9a-fA-F]{40}")
        report["currentStep"] = "Read " + network + " public clipboard address"
        value = wait(lambda: (observed if re.fullmatch(pattern, observed:=clipboard()) else None))
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

def public_words_position(words, rows=None):
    wanted=[word.lower() for word in words.split()]
    rows=public_rows() if rows is None else rows
    for index,row in enumerate(rows):
        segment=rows[index:index+len(wanted)]
        if [r[11].lower().strip("·.,:") for r in segment]!=wanted or not all(r[1:5]==row[1:5] for r in segment):continue
        left=int(segment[0][6]);right=int(segment[-1][6])+int(segment[-1][8])
        return (left+right)//2,int(row[7])+int(row[9])//2
    return None

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
        x,y,w,h=desktop.geometry(window)
        picture=desktop.image().convert("RGB")
        observed={"page":picture.getpixel((x+4,y+4))}
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
    desktop.key("Return")
    desktop.wait("Unlock local wallet",30)
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
    preferences=home/"config/localwallet/ui/appearance.json"
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
    passed("Settings Appearance has a distinct selected subtab; Light/18 defaults change to Dark/24 in public/private windows, survive graceful restart, and Unlock submits with Enter without Retry storage check")
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
        request_receive_copy()
        desktop.wait("Verify receive address",30)
        desktop.accept("Verify receive address")
        wait(lambda:not desktop.secret_window())
        expected_address=wait(lambda:(value if re.fullmatch(r"0x[0-9a-fA-F]{40}",value:=clipboard()) else None))
    scroll_public(4,35)
    wait(lambda:"Assets on Ethereum" in text())
    rows=public_rows()
    visible=" ".join(row[11] for row in rows)
    report["ethereumAssetPublicOCR"]=[{"word":row[11],"line":row[1:5],"box":[int(row[index]) for index in (6,7,8,9)]} for row in rows if row[11].strip("·.,:") in ("ETH","USDT","Unknown","Decimals","18","6")]
    asset_heading=public_words_position("Assets on Ethereum",rows)
    if not asset_heading:raise RuntimeError("Ethereum asset list heading was not visible")
    def asset_button(symbol):
        candidates=[row for row in public_rows() if row[11]==symbol and int(row[7])>asset_heading[1]]
        if not candidates:raise RuntimeError("Configured Ethereum asset button was not visible: "+symbol)
        first_y=min(int(row[7]) for row in candidates)
        # The first matching asset line contains its selector and balance.
        # Explanatory paragraphs farther below also mention ETH/USDT; those
        # words cannot be used as buttons or substituted for a balance row.
        row=min((row for row in candidates if int(row[7])<=first_y+20),key=lambda value:int(value[6]))
        return int(row[6])+int(row[8])//2,int(row[7])+int(row[9])//2
    for symbol in ("ETH","USDT"):
        _,button_y=asset_button(symbol)
        lines={}
        for row in rows:
            if abs(int(row[7])+int(row[9])//2-button_y)<45:
                lines.setdefault(tuple(row[1:5]),[]).append(row[11])
        if not any(re.search(r"\bUnknown\s+"+symbol+r"\b"," ".join(words)) for words in lines.values()):
            raise RuntimeError("Offline Ethereum asset row did not display its separate unknown balance: "+symbol)
    if not re.search(r"Decimals[: ]+18\b",visible) or not re.search(r"Decimals[: ]+6\b",visible):
        raise RuntimeError("Ethereum native/token decimal metadata was not visible")
    # Require all forty contract digits from actual public pixels. Restrict OCR
    # to hexadecimal glyphs; pixels and OCR stay in memory and never enter an
    # artifact. X11 classification and capture remain one atomic operation.
    desktop.grab_server()
    try:
        mod.X.XSync(desktop.d,0)
        if desktop.secret_window():raise PrivateWindowVisible("Refusing Ethereum public contract OCR while a private prompt is visible")
        picture=desktop.image().convert("L")
    finally:
        desktop.ungrab_server()
    data=io.BytesIO()
    picture.resize((picture.width*3,picture.height*3),Image.Resampling.LANCZOS).save(data,format="PNG")
    raw=subprocess.check_output(["tesseract","stdin","stdout","--psm","11","-c","tessedit_char_whitelist=0x0123456789abcdefABCDEF"],input=data.getvalue(),stderr=subprocess.DEVNULL,timeout=15)
    contract="0xdAC17F958D2ee523a2206206994597C13D831ec7"
    if contract.lower() not in re.sub(r"\s+","",raw.decode()).lower():
        raise RuntimeError("Complete pinned USDT contract was not readable in Ethereum public pixels")
    passed("Actual Ethereum wallet displays separate unknown ETH and USDT balances, full token contract and 18/6 decimal metadata without mocked RPC or zero-balance claims")
    stage("Select USDT and inspect its ordinary Send presentation")
    desktop.click(*asset_button("USDT"))
    time.sleep(.3)
    scroll_public(4,35)
    click("Send")
    wait(lambda:bool(re.search(r"Amount.*1 means 1 USDT",text())))
    if desktop.secret_window() or "Prepared payment" in text():
        raise RuntimeError("Token selection or opening Send requested authority or prepared a payment")
    request_receive_copy()
    desktop.wait("Verify receive address",30)
    desktop.accept("Verify receive address")
    wait(lambda:not desktop.secret_window())
    if clipboard()!=expected_address:
        raise RuntimeError("USDT selection replaced the owned Ethereum receive address")
    click("Send")
    wait(lambda:"Recipient address" not in text())
    # The token form changes layout above the list. Re-find the list before
    # switching back; no stored coordinates survive a state transition.
    scroll_public(4,35)
    heading=scroll_to_public_words("Assets on Ethereum")
    asset_heading=heading
    desktop.click(*asset_button("ETH"))
    time.sleep(.3)
    scroll_public(4,35)
    if desktop.secret_window() or "Prepared payment" in text() or "Recipient address" in text():
        raise RuntimeError("Returning to ETH retained token-send authority or an open form")
    report["ethereumTokenPresentation"]={"network":"ethereum-mainnet","assets":["ETH","USDT"],"decimals":[18,6],"tokenContract":contract,"receiveAddressSHA256":hashlib.sha256(expected_address.encode()).hexdigest(),"balanceObservation":"Unknown: no RPC service in owned network namespace","preparedOrSent":False}
    passed("Explicit USDT selection uses whole-token Send units and the same verified owned receive address, then closes Send and returns to ETH without preparing or approving a payment")

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
        if args.quick_import or args.staking_only or args.tokens_only or args.decred_copy_only or args.appearance_only or args.main_row_copy_only:
            recorded=b"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
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
            if args.staking_only:
                staking_page()
            if args.tokens_only:
                ethereum_tokens()
            if args.decred_copy_only:
                for _ in range(3):
                    addresses(("Decred",))
                passed("Three repeated actual Decred receive/copy prompts completed without ambiguous mapped windows")
            if args.appearance_only:
                appearance_campaign(installations[0],home,different_password)
            if args.main_row_copy_only:
                observed=main_row_copy("0x9858EfFD232B4033E47d90003D41EC34EcaEda94")
                report["mainRowCopy"]["matchesIndependentFixture"]=True
                passed("Actual main-row icon copies the exact independently derived public-fixture Ethereum address")
            close_gracefully()
            passed("Targeted public-fixture import reaches all-wallet overview and closes successfully")
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
            passed("Single Go executable generates a random phrase after masked app-password entry and verifies recorded words in separate private WebKitGTK windows")
            staking_page()
            main_row_address=main_row_copy()
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
            click("Back up recovery phrase")
            desktop.wait("Back up recovery phrase", 30)
            desktop.accept("Back up recovery phrase")
            desktop.wait("Confirm app password",30)
            desktop.inputs("Confirm app password",[password])
            desktop.accept("Confirm app password")
            backup = numbered_phrase("Back up recovery phrase")
            if backup != recorded:
                raise RuntimeError("Authenticated WebKitGTK backup changed generated phrase")
            window,_=desktop.wait("Back up recovery phrase")
            x,y,w,h=desktop.geometry(window)
            private_click("Back up recovery phrase","Copy phrase")
            wait(lambda: clipboard().encode()==recorded)
            desktop.accept("Back up recovery phrase")
            wait(lambda:not desktop.secret_window())
            if clipboard().encode()!=recorded:raise RuntimeError("Explicit phrase copy was lost when its private view closed")
            passed("Settings backup requires confirmation and app password, shows numbered words, and copies only on explicit private Copy phrase click")
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
