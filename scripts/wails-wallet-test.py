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
from PIL import Image

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
args = parser.parse_args()
if not re.fullmatch(r"[a-z0-9-]{1,80}",args.report_name):
    raise SystemExit("Report name must be a simple owned test name")
report = {"status": "RUNNING", "scope": __doc__, "producerSHA256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(), "steps": [], "notRun": ["Separate physical machines", "Clean Ubuntu24/26 installed desktop matrix", "Wayland", "OS suspend/session lock", "Real funded sends", "Public RPC acceptance", "Live Bybit operations", "Independent security review"]}
report["instrumented"] = args.instrumented
if args.quick_import:
    report["scope"]="Targeted isolated WebKitGTK import/restart/Unlock check using the public abandon/about BIP39 fixture. This is not the full generated-root nine-wallet restore campaign."
    report["notRun"].append("Generated-root setup, backup and exact nine-wallet address comparison (quick mode)")
if args.staking_only:
    report["scope"]="Actual uninstrumented NEAR Wallet/Staking navigation after importing a public disposable BIP39 fixture into a fresh isolated profile. Full AstroStakers contract, whole-NEAR units, unknown observed state, real offline RPC failure and disabled Withdraw are checked through X11 pixels/OCR/input. No RPC response is mocked and no transaction is prepared, approved or sent."
    report["notRun"].extend(["Generated-root setup, backup and exact nine-wallet address comparison (staking-only mode)", "Funded stake/unstake/withdraw execution and public testnet acceptance"])
if args.instrumented:
    report["scope"] = "Instrumented Go statement-coverage run of the real native desktop workflows. " + __doc__.replace("Uninstrumented", "Instrumented")
report_path = REPORTS / (args.report_name+".json")
app = xvfb = None
logs = []
password = "public-wails-wallet-password-79326"
different_password = "different-public-native-password-79326"
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

class Desktop(mod.Desktop):
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
        private_primary(prefix)
        time.sleep(.2)
        x,y,w,h=self.geometry(window)
        picture=self.image().convert("RGB")
        rows=[yy for yy in range(y,y+h-75) if all(picture.getpixel((xx,yy))==(17,23,32) for xx in (x+40,x+w//2,x+w-40))]
        groups=[]
        for yy in rows:
            if not groups or yy!=groups[-1][-1]+1:groups.append([])
            groups[-1].append(yy)
        centers=[group[len(group)//2] for group in groups if 20<=len(group)<=55]
        if len(centers)!=len(values):raise RuntimeError("Private masked input rectangles did not match expected field count: "+str(len(centers)))
        report.setdefault("privateInputGeometry",[]).append({"title":prefix,"fieldCount":len(values),"centers":[yy-y for yy in centers],"typedBytes":[len(value.encode()) for value in values]})
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
            points=[xx for xx in range(w//2,w) if picture.getpixel((xx,yy))==(185,229,208)]
            if len(points)>=50:pixels.append((yy,points))
        if len(pixels)>=15 and pixels[-1][0]-pixels[0][0]>=20:
            points=max(pixels,key=lambda row:len(row[1]))[1]
            return x+(points[0]+points[-1])//2,y+h-70+(pixels[0][0]+pixels[-1][0])//2
        time.sleep(.1)
    raise RuntimeError("Private prompt did not finish rendering its enabled footer action")

def public_rows():
    # Make classification and pixel capture one X11 operation. An async Go
    # prompt cannot become visible between the allowlist check and image read.
    # This is only the owned private :18x test server; release it before OCR.
    mod.X.XGrabServer(desktop.d)
    mod.X.XSync(desktop.d,0)
    try:
        if desktop.secret_window():
            raise PrivateWindowVisible("Refusing public OCR while a private dialog is open")
        picture=desktop.image()
    finally:
        mod.X.XUngrabServer(desktop.d)
        mod.X.XFlush(desktop.d)
    data = io.BytesIO()
    picture.resize((picture.width*2,picture.height*2),Image.Resampling.LANCZOS).save(data, format="PNG")
    raw = subprocess.check_output(["tesseract", "stdin", "stdout", "--psm", "11", "tsv"], input=data.getvalue(), stderr=subprocess.DEVNULL)
    rows = []
    for line in raw.decode().splitlines()[1:]:
        fields = line.split("\t")
        if len(fields) == 12 and fields[11].strip():
            for position in (6,7,8,9):fields[position]=str(int(fields[position])//2)
            rows.append(fields)
    return rows

def private_rows(prefix):
    window,_=desktop.wait(prefix,30)
    desktop.focus(window)
    x,y,w,h=desktop.geometry(window)
    from PIL import ImageOps
    picture=ImageOps.invert(desktop.image().crop((x,y,x+w,y+h)).convert("L"))
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
    wanted=words.lower().split()
    rows=private_rows(prefix)
    for i,row in enumerate(rows):
        segment=rows[i:i+len(wanted)]
        if [r[11].lower().strip("·.,:") for r in segment]==wanted and all(r[1:5]==row[1:5] for r in segment):
            window,_=desktop.wait(prefix,30);x,y,_,_=desktop.geometry(window)
            desktop.click(x+int(row[6])+int(row[8])//2,y+int(row[7])+int(row[9])//2)
            return
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
    candidates=[yy for yy in range(h-75) if all(max(abs(v-t) for v,t in zip(picture.getpixel((xx,yy)),(56,73,89)))<8 for xx in lefts)]
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
                crop=ImageOps.invert(picture.crop((left+34,center-15,left+cell-7,center+15)).convert("L"))
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
    # Creation and Unlock each have exactly one filled primary button on their
    # verified public page. Identify its rectangle independently of OCR
    # contrast, then require the exact next private prompt and workflow state.
    if heading not in text():
        raise RuntimeError("Expected locked public page missing before primary action")
    mod.X.XGrabServer(desktop.d);mod.X.XSync(desktop.d,0)
    try:
        if desktop.secret_window():raise PrivateWindowVisible("Private dialog unexpectedly open during setup action")
        picture=desktop.image().convert("RGB")
    finally:
        mod.X.XUngrabServer(desktop.d);mod.X.XFlush(desktop.d)
    candidates=[]
    for yy in range(145,picture.height-50):
        points=[xx for xx in range(picture.width) if picture.getpixel((xx,yy))==(185,229,208)]
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
        if not top and words in ("Ethereum","Base","Horizen","HyperEVM","HyperCore","NEAR","Aptos","Decred","TON") and attempt in (2,5,8):
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
    report["currentStep"]="Wait for initial Wails public window"
    desktop.wait("CryptoWallet", 30)
    report["currentStep"]="Wait for initial Wails public wallet state"
    wait(lambda: "Create your app password" in text() or "Your encrypted wallet is on this device" in text())

def addresses():
    result = {}
    # These are the display labels in the pinned mainnet registry.
    for network in ("Ethereum", "Base", "Horizen", "HyperEVM", "HyperCore", "NEAR", "Aptos", "Decred", "TON"):
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
        deadline=time.monotonic()+30
        while time.monotonic()<deadline:
            if any(title.startswith("Verify receive address") for _,title in desktop.windows()):break
            if desktop.secret_window():raise RuntimeError("Unexpected native dialog during public address copy")
            try:
                click("Copy address")
            except PrivateWindowVisible:
                if any(title.startswith("Verify receive address") for _,title in desktop.windows()):break
                raise
            until=time.monotonic()+.75
            while time.monotonic()<until:
                if any(title.startswith("Verify receive address") for _,title in desktop.windows()):break
                time.sleep(.05)
            if any(title.startswith("Verify receive address") for _,title in desktop.windows()):break
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

def scroll_to_public_words(words):
    for _ in range(12):
        position=public_words_position(words)
        if position:return position
        scroll_public(5,3)
    raise RuntimeError("Expected staking control or disclosure missing: "+words)

def staking_page():
    stage("Open actual NEAR wallet Staking page")
    click("Main account",top=True)
    scroll_public(4,35)
    click("NEAR")
    wait(lambda:"Copy address" in text())
    scroll_public(4,35)
    click("Staking")
    wait(lambda:"AstroStakers" in text())
    wait(lambda:"astro-stakers.poolv1.near" in text())
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
    withdraw=scroll_to_public_words("Withdraw all")
    visible=text()
    if "1 means 1 NEAR" not in visible:raise RuntimeError("Staking form omitted whole-NEAR amount units")
    rows=public_rows()
    stake=public_words_position("Stake",rows)
    withdraw=public_words_position("Withdraw all",rows)
    if not stake or not withdraw:raise RuntimeError("Staking action row could not be located independently")
    # Assert the actual rendered disabled style, independently of application
    # DOM/bridge state. Then attempt only the disabled unknown-state control;
    # an enabled preparation/confirmation button is never clicked in this drill.
    mod.X.XGrabServer(desktop.d);mod.X.XSync(desktop.d,0)
    try:
        if desktop.secret_window():raise PrivateWindowVisible("Refusing staking public pixels while a private prompt is visible")
        picture=desktop.image().convert("RGB")
    finally:
        mod.X.XUngrabServer(desktop.d);mod.X.XFlush(desktop.d)
    def background(point):
        x,y=point
        return Counter(picture.getpixel((x+dx,y+dy)) for dx in range(-8,9) for dy in range(11,15)).most_common(1)[0][0]
    enabled,disabled=background(stake),background(withdraw)
    expected_enabled,expected_disabled=(34,41,51),(28,34,43)
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
        hosts_fixture.write_text("127.0.0.1 localhost\n::1 localhost\n127.0.0.1 ethereum-rpc.publicnode.com rpc.hyperliquid.xyz mainnet.base.org horizen.calderachain.xyz api.hyperliquid.xyz rpc.mainnet.near.org api.mainnet.aptoslabs.com toncenter.com\n")
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
        if args.quick_import or args.staking_only:
            recorded=b"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
            home=folder/"machine-a/home"
            launch(installations[0],home)
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
            close_gracefully()
            passed("Targeted public-fixture import reaches all-wallet overview and closes successfully")
            if args.quick_import:
                launch(installations[0],home)
                click_primary_public("Unlock local wallet")
                desktop.wait("Unlock local wallet",30)
                desktop.inputs("Unlock local wallet",[different_password])
                desktop.accept("Unlock local wallet")
                wait(lambda:not desktop.secret_window())
                wait(lambda:"Main account" in text())
                close_gracefully()
                passed("Targeted restart starts locked, unlocks through its actual masked prompt and exits successfully")
        else:
            launch(installations[0], folder / "machine-a/home")
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
            stage("Main account row copy action")
            row = next(r for r in public_rows() if r[11].lower()=="ethereum" and int(r[7])>100)
            main_x,_,main_width,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
            desktop.click(main_x+main_width-55,int(row[7])+int(row[9])//2)
            stage("Main row verified receive dialog")
            desktop.wait("Verify receive address",30);desktop.accept("Verify receive address")
            stage("Main row copy returns to main account")
            wait(lambda:not desktop.secret_window())
            wait(lambda:"Account group" in text())
            if not re.fullmatch(r"0x[0-9a-fA-F]{40}",clipboard()):raise RuntimeError("Main row copy did not serve a verified Ethereum address")
            passed("Main-account row copy icon opens verified receive details without opening that wallet tab")
            expected = addresses()
            if len(expected) != 9:
                raise RuntimeError("Native account table lost network coverage")
            report["walletsCompared"] = len(expected)
            report["walletAddressesSHA256"] = hashlib.sha256(json.dumps(expected,sort_keys=True).encode()).hexdigest()
            passed("All nine supported network wallets open and copy their full verified single-line addresses")
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
            passed("Fresh independent app copy restores the same nine wallets from its recorded phrase and a different local app password")
            close_gracefully()
            # Reuse the isolated home only after an actual app exit.
            launch(installations[1],folder/"machine-b/home")
            click_primary_public("Unlock local wallet")
            desktop.wait("Unlock local wallet",30)
            desktop.inputs("Unlock local wallet",[different_password])
            desktop.accept("Unlock local wallet")
            wait(lambda:not desktop.secret_window(),30)
            wait(lambda:"Main account" in text(),30)
            passed("WebKitGTK restart begins locked and unlocks the restored encrypted profile")
            close_gracefully()
            passed("All three public window closes exit successfully within ten seconds without termination or force-kill")
        for log in logs:
            log.seek(0)
            output=log.read()
            if any(value in output for value in (password.encode(),different_password.encode(),recorded)):
                raise RuntimeError("Synthetic private canary escaped to process logs")
        report["status"]="PASS"
except Exception as exc:
    # No exception payload from OCR, GTK input or subprocess transport is logged.
    report.update(status="FAIL",reason=str(exc) if isinstance(exc,RuntimeError) else "Native acceptance failed; private diagnostics withheld")
    if "desktop" in globals():
        report["ownedWindowTitles"]=[title for _,title in desktop.windows()]
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
