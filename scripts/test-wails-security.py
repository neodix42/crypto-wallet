#!/usr/bin/env python3
"""Exercise the actual embedded-origin WebKit policy in a private X11 display.

Uses public synthetic content and an owned loopback HTTP witness. No wallet,
user profile, RPC credential, secret, external network or fund movement is used.
Build headers must be available through pkg-config; no packages are installed.
"""
import argparse
import contextlib
import hashlib
import http.server
import json
import os
import re
import select
import signal
import socket
import struct
import sys
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


def x11_setup_ready(number,seconds,server_pid):
    """Complete setup on the announced owned socket, without GTK or user state."""
    if seconds<=0:return False
    path=Path('/tmp/.X11-unix')/('X'+number)
    if not path.is_socket():return False
    deadline=time.monotonic()+seconds
    try:
        with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as connection:
            connection.settimeout(max(.001,deadline-time.monotonic()))
            connection.connect(str(path))
            peer_pid,peer_uid,_=struct.unpack('=iII',connection.getsockopt(socket.SOL_SOCKET,socket.SO_PEERCRED,12))
            if peer_pid!=server_pid or peer_uid!=os.geteuid():return False
            connection.sendall(struct.pack('<BBHHHHH',ord('l'),0,11,0,0,0,0))
            def read_exact(count):
                data=bytearray()
                while len(data)<count:
                    remaining=deadline-time.monotonic()
                    if remaining<=0:return None
                    connection.settimeout(remaining)
                    part=connection.recv(count-len(data))
                    if not part:return None
                    data.extend(part)
                return bytes(data)
            header=read_exact(8)
            if header is None:return False
            accepted,_,major,minor,units=struct.unpack('<BBHHH',header)
            if accepted!=1 or major!=11 or minor!=0 or not 8<=units<=16384:return False
            body=read_exact(units*4)
            if body is None or len(body)<32 or body[20]!=1:return False
            vendor_length=struct.unpack_from('<H',body,16)[0]
            screen=32+((vendor_length+3)&~3)+body[21]*8
            if screen+40>len(body):return False
            return struct.unpack_from('<HH',body,screen+20)==(1280,800)
    except (OSError,ValueError,struct.error):
        return False


def wait_private_x11(server,announcement,diagnostics,report,seconds=5):
    """Wait for one bounded displayfd line and an actual live X11 handshake."""
    started=time.monotonic()
    deadline=started+seconds
    pending=bytearray()
    number=None
    def fail(reason):
        size=os.fstat(diagnostics.fileno()).st_size
        tail=os.pread(diagnostics.fileno(),4096,max(0,size-4096))
        report['displayStartup']={'status':'FAIL','announcementReceived':number is not None,
            'x11SetupVerified':False,'exitCode':server.poll(),'elapsedSeconds':round(time.monotonic()-started,3),
            'stderrBytes':size,'stderrTruncated':size>4096,'stderrTail':tail.decode('utf-8',errors='replace')}
        raise RuntimeError(reason)
    while time.monotonic()<deadline:
        if server.poll() is not None:fail('Private X11 test display exited before readiness')
        if number is not None:
            if x11_setup_ready(number,max(0,deadline-time.monotonic()),server.pid):
                if server.poll() is not None:fail('Private X11 test display exited during its setup handshake')
                report['displayStartup']={'status':'READY','announcementReceived':True,'x11SetupVerified':True,
                    'display':':'+number,'elapsedSeconds':round(time.monotonic()-started,3),
                    'stderrBytes':os.fstat(diagnostics.fileno()).st_size}
                return number
            time.sleep(min(.05,max(0,deadline-time.monotonic())))
            continue
        ready,_,_=select.select([announcement],[],[],min(.05,max(0,deadline-time.monotonic())))
        if not ready:continue
        try:part=os.read(announcement,17-len(pending))
        except BlockingIOError:continue
        if not part:fail('Private X11 test display closed its announcement before readiness')
        pending.extend(part)
        if b'\n' in pending:
            if not re.fullmatch(rb'[0-9]{1,5}\n',pending) or int(pending)>65535:
                fail('Private X11 test display sent an invalid announcement')
            number=str(int(pending))
        elif len(pending)>=16:
            fail('Private X11 test display sent an oversized announcement')
    fail('Private X11 test display did not complete its setup handshake' if number is not None
         else 'Private X11 test display did not announce readiness')


@contextlib.contextmanager
def private_x11_display(xvfb,report,seconds=5):
    # stderr must never be an unread pipe: ordinary xkbcomp startup warnings
    # can fill it before Xvfb writes displayfd. Keep only a bounded tail in a
    # failure report, and use a separate nonblocking pipe for the announcement.
    announcement,child_announcement=os.pipe()
    os.set_blocking(announcement,False)
    server=None
    try:
        with tempfile.TemporaryFile() as diagnostics:
            try:
                server=subprocess.Popen([xvfb,'-displayfd',str(child_announcement),'-screen','0','1280x800x24',
                    '-nolisten','tcp','-ac'],pass_fds=(child_announcement,),stdout=subprocess.DEVNULL,stderr=diagnostics,
                    start_new_session=True)
                os.close(child_announcement)
                child_announcement=None
                yield wait_private_x11(server,announcement,diagnostics,report,seconds)
            finally:
                if server is not None:
                    primary_failure=sys.exc_info()[0] is not None
                    forced=False
                    try:
                        # This exact PGID was created by start_new_session.
                        # xkbcomp descendants need cleanup even if Xvfb exits
                        # first; never signal the caller's process group.
                        try:os.killpg(server.pid,signal.SIGTERM)
                        except ProcessLookupError:pass
                        try:server.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            forced=True
                            try:os.killpg(server.pid,signal.SIGKILL)
                            except ProcessLookupError:pass
                            server.wait(timeout=5)
                        try:os.killpg(server.pid,0)
                        except ProcessLookupError:pass
                        else:
                            forced=True
                            try:os.killpg(server.pid,signal.SIGKILL)
                            except ProcessLookupError:pass
                        report['displayCleanup']={'status':'PASS','forced':forced,'exitCode':server.returncode}
                    except (OSError,subprocess.SubprocessError) as error:
                        report['displayCleanup']={'status':'FAIL','forced':forced,'reason':str(error)}
                        if not primary_failure:raise RuntimeError('Owned X11 test display could not be reaped') from error
    finally:
        os.close(announcement)
        if child_announcement is not None:os.close(child_announcement)


def check(report_path):
    before = hashes()
    report = {'status': 'FAIL', 'scope': 'Actual system WebKitGTK in isolated disposable X11 HOME; no wallet process or real secrets',
              'checks': [], 'sourceSHA256': before,
              'runtimePolicy':'NOT_RUN','runtimeClipboard':'NOT_RUN',
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
                try:
                    with private_x11_display(xvfb,report) as number:
                        (directory / 'runtime').mkdir(mode=0o700)
                        environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'DISPLAY': ':' + number,
                                       'HOME': str(directory), 'XDG_RUNTIME_DIR': str(directory / 'runtime'),
                                       'XDG_CACHE_HOME': str(directory / 'cache'), 'XDG_DATA_HOME': str(directory / 'data'),
                                       'XDG_CONFIG_HOME': str(directory / 'config'), 'GSETTINGS_BACKEND': 'memory',
                                       'DBUS_SESSION_BUS_ADDRESS': 'disabled:', 'GDK_BACKEND': 'x11',
                                       'LIBGL_ALWAYS_SOFTWARE': '1', 'NO_AT_BRIDGE': '1',
                                       'WALLET_PROBE_ENDPOINT': f'http://127.0.0.1:{witness.server_address[1]}'}
                        report['runtimePolicy']='RUNNING'
                        result = subprocess.run([str(binary)], env=environment, capture_output=True, text=True, timeout=25)
                        if result.returncode or 'PASS real WebKit:' not in result.stdout:
                            # Probe uses synthetic public content only; retain a bounded diagnostic.
                            raise RuntimeError(f'Native policy exited {result.returncode}: {(result.stdout + result.stderr)[-2000:]}')
                        if hits:
                            raise RuntimeError('Browser request escaped into the owned HTTP witness')
                        report['runtimePolicy']='PASS'
                        report['runtimeClipboard']='RUNNING'
                        copied = subprocess.run([str(clipboard_binary)], env=environment, capture_output=True,
                                                text=True, timeout=10)
                        if copied.returncode or 'PASS native clipboard:' not in copied.stdout:
                            raise RuntimeError('Native clipboard changed the unrequested PRIMARY selection')
                        report['runtimeClipboard']='PASS'
                finally:
                    witness.shutdown()
                    witness_thread.join(timeout=1)
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
        for gate in ('runtimePolicy','runtimeClipboard'):
            if report[gate]=='RUNNING':report[gate]='FAIL'
        report['reason'] = str(error)
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return report['status'] == 'PASS'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, default=ROOT / 'reports/wails-security.json')
    raise SystemExit(0 if check(parser.parse_args().report) else 1)
