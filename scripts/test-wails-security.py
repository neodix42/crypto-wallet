#!/usr/bin/env python3
"""Exercise the actual embedded-origin WebKit policy in a private X11 display.

Uses public synthetic content and an owned loopback HTTP witness. No wallet,
user profile, RPC credential, secret, external network or fund movement is used.
Build headers must be available through pkg-config; no packages are installed.
"""
import argparse
import hashlib
import http.server
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parent.parent
SOURCES = [ROOT / 'third_party/wails/wallet-tests/native-policy.c',
           ROOT / 'third_party/wails/internal/frontend/desktop/linux/wallet_security.h',
           ROOT / 'third_party/wails/pkg/assetserver/assetserver.go',
           ROOT / 'third_party/wails/internal/frontend/walletpolicy/policy.go',
           ROOT / 'third_party/wails/internal/frontend/dispatcher/dispatcher.go',
           ROOT / 'third_party/wails/internal/frontend/desktop/linux/frontend.go',
           ROOT / 'third_party/wails/internal/frontend/desktop/linux/window.c',
           ROOT / 'third_party/wails/wallet-tests/native-clipboard.c',
           ROOT / 'third_party/wails/internal/frontend/desktop/linux/wallet_clipboard.h',
           ROOT / 'third_party/wails/internal/frontend/desktop/linux/clipboard.go',
           ROOT / 'third_party/wails/internal/frontend/walletclipboard/clipboard.go',
           ROOT / 'third_party/wails/pkg/runtime/clipboard.go',
           Path(__file__).resolve()]


def hashes():
    return {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in SOURCES}


def check(report_path):
    before = hashes()
    report = {'status': 'FAIL', 'scope': 'Actual system WebKitGTK in isolated disposable X11 HOME; no wallet process or real secrets',
              'checks': [], 'sourceSHA256': before,
              'notRun': ['Full wallet workflows', 'Windows/macOS', 'Native OS lifecycle', 'Remote provider acceptance']}
    hits = []
    production = re.search(r'rw\.Header\(\)\.Set\("Content-Security-Policy", "([^"]+)"\)', SOURCES[2].read_text())
    fixture = re.search(r'static const char \*policy="([^"]+)"', SOURCES[0].read_text())
    if not production or not fixture or production.group(1) != fixture.group(1):
        raise RuntimeError('Native probe CSP differs from production asset response policy')

    class Witness(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            hits.append(self.path)
            self.send_response(200)
            self.end_headers()
        def log_message(self, *_):
            pass

    try:
        for executable in ['gcc', 'pkg-config']:
            if not shutil.which(executable):
                raise RuntimeError(f'Required test build tool unavailable: {executable}')
        xvfb = shutil.which('Xvfb') or str(ROOT / '.tools/xvfb/root/usr/bin/Xvfb')
        if not Path(xvfb).is_file():
            raise RuntimeError('Private X11 test requires Xvfb')
        flags = subprocess.check_output(['pkg-config', '--cflags', '--libs', 'gtk+-3.0', 'webkit2gtk-4.1'], text=True).split()
        with tempfile.TemporaryDirectory(prefix='cryptowallet-webkit-security-') as temporary:
            directory = Path(temporary)
            binary = directory / 'native-policy'
            subprocess.run(['gcc', '-Werror=implicit-function-declaration', '-Wno-deprecated-declarations',
                            str(SOURCES[0]), '-o', str(binary), *flags], check=True, capture_output=True, timeout=30)
            report['checks'].append('Native security callbacks compile against actual WebKitGTK headers')
            report['producerSHA256'] = hashlib.sha256(binary.read_bytes()).hexdigest()
            clipboard_binary = directory / 'native-clipboard'
            subprocess.run(['gcc', '-Werror=implicit-function-declaration', '-Wno-deprecated-declarations',
                            str(ROOT / 'third_party/wails/wallet-tests/native-clipboard.c'),
                            '-o', str(clipboard_binary), *flags], check=True, capture_output=True, timeout=30)
            report['clipboardProducerSHA256'] = hashlib.sha256(clipboard_binary.read_bytes()).hexdigest()
            with http.server.ThreadingHTTPServer(('127.0.0.1', 0), Witness) as witness:
                witness_thread = threading.Thread(target=witness.serve_forever, daemon=True)
                witness_thread.start()
                with (directory / 'display').open('w+') as display:
                    xserver = subprocess.Popen([xvfb, '-displayfd', str(display.fileno()), '-screen', '0', '1280x800x24',
                                                '-nolisten', 'tcp', '-ac'], pass_fds=(display.fileno(),),
                                               stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
                    try:
                        number = ''
                        for _ in range(100):
                            display.seek(0)
                            number = display.read().strip()
                            if number:
                                break
                            if xserver.poll() is not None:
                                raise RuntimeError('Private X11 test display exited')
                            time.sleep(.05)
                        if not number:
                            raise RuntimeError('Private X11 test display did not become ready')
                        (directory / 'runtime').mkdir(mode=0o700)
                        environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'DISPLAY': ':' + number,
                                       'HOME': str(directory), 'XDG_RUNTIME_DIR': str(directory / 'runtime'),
                                       'XDG_CACHE_HOME': str(directory / 'cache'), 'XDG_DATA_HOME': str(directory / 'data'),
                                       'XDG_CONFIG_HOME': str(directory / 'config'), 'GSETTINGS_BACKEND': 'memory',
                                       'DBUS_SESSION_BUS_ADDRESS': 'disabled:', 'GDK_BACKEND': 'x11',
                                       'LIBGL_ALWAYS_SOFTWARE': '1', 'NO_AT_BRIDGE': '1',
                                       'WALLET_PROBE_ENDPOINT': f'http://127.0.0.1:{witness.server_address[1]}'}
                        result = subprocess.run([str(binary)], env=environment, capture_output=True, text=True, timeout=25)
                        if result.returncode or 'PASS real WebKit:' not in result.stdout:
                            # Probe uses synthetic public content only; retain a bounded diagnostic.
                            raise RuntimeError(f'Native policy exited {result.returncode}: {(result.stdout + result.stderr)[-2000:]}')
                        if hits:
                            raise RuntimeError('Browser request escaped into the owned HTTP witness')
                        copied = subprocess.run([str(clipboard_binary)], env=environment, capture_output=True,
                                                text=True, timeout=10)
                        if copied.returncode or 'PASS native clipboard:' not in copied.stdout:
                            raise RuntimeError('Native clipboard changed the unrequested PRIMARY selection')
                    finally:
                        xserver.terminate()
                        xserver.wait(timeout=5)
                        xserver.stderr.close()
                witness.shutdown()
            report['checks'].extend([
                'Strict custom-scheme origin/path validation rejects file/data/http/foreign hosts/userinfo/ports/query/fragment/traversal',
                'Actual context and WebView are ephemeral and report sandbox disabled without environment bypass',
                'Storage, page cache, developer tools, WebRTC and browser clipboard access settings disabled',
                'Production-style CSP blocks actual HTTP fetch and image requests; owned HTTP witness received zero requests',
                'Production-style CSP blocks actual eval and localStorage access',
                'Actual popup and external navigation denied while embedded origin remains unchanged',
                'Actual native copy changes only CLIPBOARD and preserves the independent PRIMARY selection'])
            report['httpRequestsObserved'] = len(hits)
            report['sourceStable'] = before == hashes()
            if not report['sourceStable']:
                raise RuntimeError('Security policy source changed during test')
            report['status'] = 'PASS'
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        report['reason'] = str(error)
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return report['status'] == 'PASS'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, default=ROOT / 'reports/wails-security.json')
    raise SystemExit(0 if check(parser.parse_args().report) else 1)
