#!/usr/bin/env python3
"""Native adapter regression on a fresh profile/private Xvfb; public fixtures only."""
import json
import hashlib
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time

root = pathlib.Path(__file__).resolve().parents[2]
reports = root / "reports"
reports.mkdir(exist_ok=True)
report = {"status": "NOT_RUN", "scope": "Native GTK public-tree and private-prompt adapter; synthetic public fixtures on an isolated X11 display", "notRun": ["Clean Ubuntu desktop matrix", "Wayland", "Real credentials or funds", "Independent security review"], "tests": []}
local = root / ".tools/xvfb/root/usr/bin/Xvfb"
server = shutil.which("Xvfb") or (str(local) if local.is_file() else None)
node = None
try:
    if not server:
        raise RuntimeError("Xvfb unavailable")
    display = next((f":{n}" for n in range(180, 190) if not pathlib.Path(f"/tmp/.X11-unix/X{n}").exists() and not pathlib.Path(f"/tmp/.X{n}-lock").exists()), None)
    if not display:
        raise RuntimeError("No unused private :18x display")
    with tempfile.TemporaryDirectory(prefix="cryptowallet-native-ui-") as tmp, tempfile.TemporaryFile() as display_log:
        binary = pathlib.Path(tmp) / "nativeui.test"
        # Compile with the existing development cache, before isolating HOME.
        subprocess.run([str(root / "scripts/with-toolchain.sh"), "go", "test", "-c", "-race", "-tags", "portable,automation", "-covermode=atomic", "-coverpkg=./internal/nativeui", "-o", str(binary), "./internal/nativeui"], cwd=root, check=True, timeout=180)
        env = dict(os.environ, DISPLAY=display, GDK_BACKEND="x11", CRYPTOWALLET_NATIVE_TEST="1", HOME=tmp, XDG_CONFIG_HOME=tmp + "/config")
        for key in ("WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"):
            env.pop(key, None)
        if pathlib.Path(server) == local:
            env["LD_LIBRARY_PATH"] = str(root / ".tools/xvfb/root/usr/lib/x86_64-linux-gnu") + (":" + env["LD_LIBRARY_PATH"] if env.get("LD_LIBRARY_PATH") else "")
        node = subprocess.Popen([server, display, "-screen", "0", "1600x1100x24", "-nolisten", "tcp"], env=env, stdout=display_log, stderr=display_log)
        for _ in range(100):
            if node.poll() is not None:
                raise RuntimeError("Private Xvfb exited before readiness")
            if pathlib.Path("/tmp/.X11-unix/X" + display[1:]).exists():
                break
            time.sleep(.05)
        else:
            raise RuntimeError("Private Xvfb did not become ready")
        result = subprocess.run([str(binary), "-test.v", "-test.timeout=45s", "-test.coverprofile=" + str(reports / "native-ui.cover")], env=env, cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
        print(result.stdout, end="")
        for line in result.stdout.splitlines():
            if line.startswith(("--- PASS:", "--- FAIL:", "--- SKIP:")):
                fields = line.split()
                report["tests"].append({"name": fields[2], "status": fields[1].rstrip(":")})
        required = {"TestNativeDialogsAndCancellationOnPrivateX11", "TestPublicViewIsBoundedPlainTextAndNeverASecretInput", "TestRenderSnapshotDoesNotShareControllerTrees", "TestNumberedPhraseClearsTemporarySourceAndPreservesWords"}
        passed = {t["name"] for t in report["tests"] if t["status"] == "PASS"}
        report["status"] = "PASS" if result.returncode == 0 and required <= passed else "FAIL"
        report["exit"] = result.returncode
        profile = reports / "native-ui.cover"
        if profile.is_file():
            report["coverage"] = {"file": str(profile.relative_to(root)), "sha256": hashlib.sha256(profile.read_bytes()).hexdigest(), "measurement": "Go statement coverage; C GTK binding coverage is not measured"}
except (RuntimeError, OSError, subprocess.TimeoutExpired, subprocess.CalledProcessError) as exc:
    report.update(status="BLOCKED", reason=str(exc))
finally:
    if node is not None:
        node.terminate()
        try:
            node.wait(timeout=5)
        except subprocess.TimeoutExpired:
            node.kill()
            node.wait(timeout=5)
    (reports / "native-ui.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
sys.exit(0 if report["status"] == "PASS" else 1)
