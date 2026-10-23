#!/usr/bin/env python3
"""Real GTK key/button input on an owned private Xvfb display. Public synthetic
fixtures only. This does not establish Wayland or the clean-desktop matrix."""
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import time

root = pathlib.Path(__file__).resolve().parents[1]
reports = root / "reports"
reports.mkdir(exist_ok=True)
report = {
    "status": "NOT_RUN",
    "scope": "Real GTK input and private X11 XTest events; synthetic fixture values only",
    "notRun": ["Wayland native input", "Five-minute wall-clock sustained typing", "Clean installed desktop OS-lock/suspend matrix"],
    "tests": [],
}
start = time.monotonic()
node = None
try:
    server = shutil.which("Xvfb")
    local = root / ".tools/xvfb/root/usr/bin/Xvfb"
    if not server and local.is_file():
        server = str(local)
    if not server:
        raise RuntimeError("Xvfb unavailable; install the test-only Xvfb dependency")
    display = next((f":{n}" for n in range(180, 190)
                    if not pathlib.Path(f"/tmp/.X11-unix/X{n}").exists()
                    and not pathlib.Path(f"/tmp/.X{n}-lock").exists()), None)
    if not display:
        raise RuntimeError("No unused isolated :18x test display; refusing to attach to an existing display")
    env = os.environ.copy()
    env.update(DISPLAY=display, GDK_BACKEND="x11")
    if pathlib.Path(server) == local:
        libraries = str(root / ".tools/xvfb/root/usr/lib/x86_64-linux-gnu")
        env["LD_LIBRARY_PATH"] = libraries + (":" + env["LD_LIBRARY_PATH"] if env.get("LD_LIBRARY_PATH") else "")
        env["PATH"] = str(local.parent) + ":" + env.get("PATH", "")
    with tempfile.TemporaryFile(mode="w+") as log:
        node = subprocess.Popen([server, display, "-screen", "0", "1280x1024x24", "-nolisten", "tcp"],
                                env=env, stdout=log, stderr=log)
        for _ in range(100):
            if node.poll() is not None:
                log.seek(0)
                raise RuntimeError("Private Xvfb failed: " + log.read(4096))
            if pathlib.Path("/tmp/.X11-unix/X" + display[1:]).exists():
                break
            time.sleep(.05)
        else:
            raise RuntimeError("Private Xvfb did not become ready")
        command = ["go", "test", "-race", "-tags", "automation", "-count=1", "-json",
                   "-coverprofile=" + str(reports / "native-input.cover"), "./internal/secureui"]
        result = subprocess.run(command, cwd=root, env=env, text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, timeout=90)
        for line in result.stdout.splitlines():
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                print(line, flush=True)
                continue
            if event.get("Action") in ("pass", "fail", "skip") and event.get("Test"):
                report["tests"].append({"name": event["Test"], "status": event["Action"].upper(), "seconds": event.get("Elapsed", 0)})
            if event.get("Action") == "output":
                print(event.get("Output", ""), end="", flush=True)
        required = {"TestRealX11NativeKeyAndButtonActivity", "TestNativeProgrammaticFillAndDialogTimersDoNotCountAsInput",
                    "TestNativeActivityCallbackIsValueFreeRateLimitedAndCleared", "TestTP033RealNativeMaskedInput",
                    "TestTP042RealNativeConfirmation"}
        passed = {test["name"] for test in report["tests"] if test["status"] == "PASS"}
        report.update(status="PASS" if result.returncode == 0 and required <= passed else "FAIL", exit=result.returncode)
        coverage = subprocess.run(["go", "tool", "cover", "-func=" + str(reports / "native-input.cover")],
                                  cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        report["coverage"] = coverage.stdout.splitlines()[-1] if coverage.returncode == 0 else "NOT_RUN"
except (OSError, RuntimeError, subprocess.TimeoutExpired) as error:
    report.update(status="BLOCKED", reason=str(error))
finally:
    if node is not None:
        node.terminate()
        try:
            node.wait(timeout=5)
        except subprocess.TimeoutExpired:
            node.kill()
            node.wait()
    report["seconds"] = round(time.monotonic() - start, 3)
    (reports / "native-input.json").write_text(json.dumps(report, indent=2) + "\n")
print(json.dumps(report, indent=2))
raise SystemExit(0 if report["status"] == "PASS" else 1)
