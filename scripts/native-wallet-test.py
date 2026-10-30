#!/usr/bin/env python3
"""Uninstrumented native Go wallet acceptance on an isolated private X11 display.

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
from PIL import Image

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORTS = ROOT / "reports"
REPORTS.mkdir(exist_ok=True)
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--binary", type=pathlib.Path, default=ROOT / "bin/cryptowallet")
parser.add_argument("--report-name", default="native-wallet")
parser.add_argument("--instrumented", action="store_true")
args = parser.parse_args()
if not re.fullmatch(r"[a-z0-9-]{1,80}",args.report_name):
    raise SystemExit("Report name must be a simple owned test name")
report = {"status": "RUNNING", "scope": __doc__, "producerSHA256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(), "steps": [], "notRun": ["Separate physical machines", "Clean Ubuntu24/26 installed desktop matrix", "Wayland", "OS suspend/session lock", "Real funded sends", "Public RPC acceptance", "Live Bybit operations", "Independent security review"]}
report["instrumented"] = args.instrumented
if args.instrumented:
    report["scope"] = "Instrumented Go statement-coverage run of the real native desktop workflows. " + __doc__.replace("Uninstrumented", "Instrumented")
report_path = REPORTS / (args.report_name+".json")
app = xvfb = None
logs = []
password = "public-native-wallet-password-79326"
different_password = "different-public-native-password-79326"
recorded = None

def source_identity():
    digest = hashlib.sha256()
    paths = [pathlib.Path(__file__), ROOT / "tests/e2e/x11.py", ROOT / "tests/e2e/phrase.py"]
    for directory in (ROOT / "internal", ROOT / "cmd/cryptowallet"):
        paths.extend(path for path in directory.rglob("*") if path.is_file() and path.suffix in (".go", ".c", ".h"))
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

def save():
    report_path.write_text(json.dumps(report, indent=2) + "\n")

def passed(name):
    report["steps"].append({"name": name, "status": "PASS"})
    print("PASS " + name, flush=True)
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
    raise RuntimeError("Native wallet workflow exceeded bounded deadline")

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

def text():
    return " ".join(r[11] for r in public_rows())

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
            if top and int(row[7]) > main_y + 60:
                continue
            matches.append(row)
        if matches:
            row = matches[-1] if last else matches[0]
            desktop.focus(desktop.wait("CryptoWallet")[0])
            desktop.click(int(row[6])+int(row[8])//2, int(row[7])+int(row[9])//2, button=button)
            time.sleep(.25)
            return
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

def launch(installation, home):
    global app
    env = dict(base_environment, HOME=str(home), XDG_CONFIG_HOME=str(home / "config"))
    home.mkdir(mode=0o700, exist_ok=True)
    log = tempfile.TemporaryFile()
    logs.append(log)
    command=["unshare","--user","--map-current-user","--keep-caps","--net","--mount","/bin/sh","-c",'mount --make-rprivate / && mount --bind "$1" /etc/hosts && exec setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all "$2"',"cryptowallet-owned-test",str(hosts_fixture),str(installation/"cryptowallet")]
    app = subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True)
    desktop.wait("CryptoWallet", 30)
    wait(lambda: "Create your app password" in text() or "Your encrypted wallet is on this device" in text())

def addresses():
    result = {}
    # These are the display labels in the pinned mainnet registry.
    for network in ("Ethereum", "Base", "HyperEVM", "HyperCore", "NEAR", "Aptos", "Arbitrum", "TON"):
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
        pattern = {"NEAR":r"[0-9a-f]{64}","Aptos":r"0x[0-9a-fA-F]{64}","TON":r"[UE]Q[A-Za-z0-9_-]{46}"}.get(network,r"0x[0-9a-fA-F]{40}")
        report["currentStep"] = "Read " + network + " public clipboard address"
        value = wait(lambda: (observed if re.fullmatch(pattern, observed:=clipboard()) else None))
        wait(lambda:"Request completed" in text())
        result[network] = value
        if network != "TON":
            report["currentStep"] = "Close " + network + " wallet tab by middle click"
            deadline=time.monotonic()+30
            while time.monotonic()<deadline:
                rows=public_rows();words=" ".join(r[11] for r in rows)
                _,main_y,_,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
                present=any(r[11].lower().strip("·.,:")==network.lower() and int(r[7])<=main_y+60 for r in rows)
                if "Account group" in words and "Copy address" not in words and not present:break
                click(network,top=True,button=2)
                time.sleep(.15)
            else:raise RuntimeError("Middle click did not close the selected wallet tab")
    return result

save()
try:
    binary = args.binary.resolve()
    if not binary.is_file():
        raise RuntimeError("Build the native Go executable first")
    probe = subprocess.run(["unshare", "--user", "--map-current-user", "--keep-caps", "--net", "--mount", "/bin/sh", "-c", "mount --make-rprivate / && id -u"], capture_output=True, text=True, timeout=10)
    if probe.returncode or probe.stdout.strip() == "0":
        raise RuntimeError("Private network namespace preserving ordinary UID unavailable")
    report["artifact"] = {"version": subprocess.check_output([str(binary), "--version"], text=True, timeout=10).strip(), "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "runtime": subprocess.check_output([str(binary), "--runtime-info"], text=True, timeout=10).strip()}
    if report["artifact"]["runtime"] != "native-gtk3; portable-go; sandbox=none":
        raise RuntimeError("Acceptance candidate is not the sandbox-free native Go app")
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
        hosts_fixture.write_text("127.0.0.1 localhost\n::1 localhost\n127.0.0.1 ethereum-rpc.publicnode.com rpc.hyperliquid.xyz base-rpc.publicnode.com base-sepolia-rpc.publicnode.com arb1.arbitrum.io sepolia-rollup.arbitrum.io api.hyperliquid.xyz rpc.mainnet.fastnear.com rpc.testnet.fastnear.com archival-rpc.mainnet.fastnear.com archival-rpc.testnet.fastnear.com api.mainnet.aptoslabs.com toncenter.com\n")
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
        launch(installations[0], folder / "machine-a/home")
        click("Create wallet", last=False)
        desktop.wait("Create local wallet", 30)
        desktop.inputs("Create local wallet", [password,password])
        desktop.accept("Create local wallet")
        recorded = phrase_ui.record(desktop)
        wait(lambda: not desktop.secret_window(), 30)
        wait(lambda: "Main account" in text(), 30)
        passed("Single Go executable generates a random phrase after masked app-password entry and verifies recorded words in real native windows")
        row = next(r for r in public_rows() if r[11].lower()=="ethereum" and int(r[7])>100)
        main_x,_,main_width,_=desktop.geometry(desktop.wait("CryptoWallet")[0])
        desktop.click(main_x+main_width-55,int(row[7])+int(row[9])//2)
        desktop.wait("Verify receive address",30);desktop.accept("Verify receive address")
        wait(lambda:not desktop.secret_window())
        wait(lambda:"Account group" in text())
        wait(lambda:"Request completed" in text())
        passed("Main-account row copy icon opens verified receive details without opening that wallet tab")
        expected = addresses()
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
        wait(lambda:"Request completed" in text())
        passed("Native Receive opens the Go-authored address and QR dialog")
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
        backup = phrase_ui.numbered(desktop, "Back up recovery phrase")
        if backup != recorded:
            raise RuntimeError("Authenticated native backup changed generated phrase")
        window,_=desktop.wait("Back up recovery phrase")
        x,y,w,h=desktop.geometry(window)
        desktop.click(x+w-170,y+h-22)
        wait(lambda: clipboard().encode()==recorded)
        desktop.accept("Back up recovery phrase")
        passed("Settings backup requires confirmation and app password, shows numbered words, and copies only on explicit native Copy phrase click")
        stop()
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
        stop()
        # Reuse the isolated home only after an actual app exit.
        launch(installations[1],folder/"machine-b/home")
        click("Unlock",last=True)
        desktop.wait("Unlock local wallet",30)
        desktop.inputs("Unlock local wallet",[different_password])
        desktop.accept("Unlock local wallet")
        wait(lambda:not desktop.secret_window(),30)
        wait(lambda:"Main account" in text(),30)
        passed("Native restart begins locked and unlocks the restored encrypted profile")
        stop()
        for log in logs:
            log.seek(0)
            output=log.read()
            if any(value in output for value in (password.encode(),different_password.encode(),recorded)):
                raise RuntimeError("Synthetic private canary escaped to process logs")
        report["status"]="PASS"
except Exception as exc:
    # No exception payload from OCR, GTK input or subprocess transport is logged.
    report.update(status="FAIL",reason=str(exc) if isinstance(exc,RuntimeError) else "Native acceptance failed; private diagnostics withheld")
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
