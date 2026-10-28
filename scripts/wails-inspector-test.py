#!/usr/bin/env python3
"""Reject inherited inspector listeners in the actual Go/Wails executable.

An empty disposable testnet profile and owned Xvfb are used. No private inputs,
existing wallet profile, RPC credential, external service or funds participate.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent


def source_identity():
    paths = [Path(__file__).resolve(), ROOT / 'go.mod', ROOT / 'go.sum',
             ROOT / 'third_party/wails/WALLET-UPSTREAM.json', ROOT / 'third_party/wails/WALLET-PATCHES.json']
    paths.extend((ROOT / 'internal/webui').glob('*.go'))
    paths.extend((ROOT / 'cmd/cryptowallet').glob('*.go'))
    return {path.relative_to(ROOT).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(paths)}


def process_tree(parent):
    rows = {}
    for path in Path('/proc').iterdir():
        if not path.name.isdigit():
            continue
        try:
            raw = (path / 'stat').read_text()
            name = raw[raw.index('(') + 1:raw.rindex(')')]
            values = raw[raw.rindex(')') + 2:].split()
            rows[int(path.name)] = {'parent': int(values[1]), 'group': int(values[2]),
                                     'state': values[0], 'name': name, 'start': values[19]}
        except (OSError, ValueError, IndexError):
            continue
    owned = {parent}
    for _ in range(16):
        more = {pid for pid, row in rows.items() if row['parent'] in owned}
        if more <= owned:
            break
        owned.update(more)
    return {pid: rows[pid] for pid in owned if pid in rows}


def listening(port):
    try:
        with socket.create_connection(('127.0.0.1', port), timeout=.1):
            return True
    except OSError:
        return False


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def run(binary, report_path):
    report = {'status': 'NOT_RUN', 'scope': __doc__, 'checks': [],
              'notRun': ['Real wallet secrets or profiles', 'OS matrix', 'Independent security review']}
    before = source_identity()
    report['sourceSHA256'] = before
    child = xserver = None
    try:
        if os.getuid() == 0:
            raise RuntimeError('Run this graphical candidate test as an ordinary user')
        if not binary.is_file() or binary.is_symlink():
            raise RuntimeError('Candidate must be a regular executable')
        runtime = subprocess.check_output([str(binary), '--runtime-info'], text=True, timeout=10).strip()
        if runtime != 'wails-webkitgtk4.1; portable-go; sandbox=none':
            raise RuntimeError('Candidate is not the Wails Go executable')
        report['artifact'] = {'sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'runtime': runtime}
        xvfb = shutil.which('Xvfb') or str(ROOT / '.tools/xvfb/root/usr/bin/Xvfb')
        if not Path(xvfb).is_file():
            raise RuntimeError('Test requires Xvfb')
        ports = [free_port(), free_port()]
        while ports[0] == ports[1]:
            ports[1] = free_port()
        if any(listening(port) for port in ports):
            raise RuntimeError('Owned inspector witness ports were not initially free')
        with tempfile.TemporaryDirectory(prefix='cryptowallet-inspector-') as temporary:
            directory = Path(temporary)
            (directory / 'runtime').mkdir(mode=0o700)
            with (directory / 'display').open('w+') as display:
                xserver = subprocess.Popen([xvfb, '-displayfd', str(display.fileno()), '-screen', '0', '1600x1100x24',
                                            '-nolisten', 'tcp', '-ac'], pass_fds=(display.fileno(),),
                                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                for _ in range(100):
                    display.seek(0)
                    number = display.read().strip()
                    if number:
                        break
                    if xserver.poll() is not None:
                        raise RuntimeError('Private X11 display exited')
                    time.sleep(.05)
                else:
                    raise RuntimeError('Private X11 display did not initialize')
                environment = {'PATH': '/usr/bin:/bin', 'DISPLAY': ':' + number, 'HOME': str(directory),
                    'XDG_RUNTIME_DIR': str(directory / 'runtime'), 'XDG_CACHE_HOME': str(directory / 'cache'),
                    'XDG_DATA_HOME': str(directory / 'data'), 'XDG_CONFIG_HOME': str(directory / 'config'),
                    'DBUS_SESSION_BUS_ADDRESS': 'disabled:', 'GSETTINGS_BACKEND': 'memory', 'NO_AT_BRIDGE': '1',
                    'GDK_BACKEND': 'x11', 'LIBGL_ALWAYS_SOFTWARE': '1',
                    'WEBKIT_INSPECTOR_SERVER': f'127.0.0.1:{ports[0]}',
                    'WEBKIT_INSPECTOR_HTTP_SERVER': f'127.0.0.1:{ports[1]}'}
                child = subprocess.Popen([str(binary), '--profile=testnet'], env=environment,
                                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
                deadline = time.monotonic() + 20
                helpers = set()
                while time.monotonic() < deadline:
                    if child.poll() is not None:
                        raise RuntimeError('Candidate exited before WebKit initialized')
                    if any(listening(port) for port in ports):
                        raise RuntimeError('Inherited WebKit inspector opened a listener')
                    names = {row['name'] for row in process_tree(child.pid).values()}
                    helpers = {name for name in names if name.startswith('WebKit')}
                    if any(name.startswith('WebKitWebPro') for name in helpers) and any('NetworkPr' in name for name in helpers):
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError('Actual WebKit helper processes did not initialize')
                end = time.monotonic() + 3
                while time.monotonic() < end:
                    if child.poll() is not None or any(listening(port) for port in ports):
                        raise RuntimeError('Candidate exited or inherited inspector listener appeared')
                    time.sleep(.1)
                report['checks'] = ['Actual candidate launched with an empty isolated testnet profile',
                    'Actual WebKitNetworkProcess and WebKitWebProcess initialized',
                    'Inherited TCP and HTTP inspector variables opened zero listeners throughout observation']
                report['helpersObserved'] = sorted(helpers)
                report['observationSeconds'] = 3
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=10)
                child = None
                xserver.terminate()
                xserver.wait(timeout=5)
                xserver = None
        report['sourceStable'] = source_identity() == before
        if not report['sourceStable']:
            raise RuntimeError('Relevant candidate source changed during observation')
        report['status'] = 'PASS'
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        report.update(status='FAIL', reason=str(error))
    finally:
        if child is not None:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            child.wait(timeout=10)
        if xserver is not None:
            xserver.terminate()
            xserver.wait(timeout=5)
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return report['status'] == 'PASS'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=ROOT / 'bin/cryptowallet')
    parser.add_argument('--report', type=Path, default=ROOT / 'reports/wails-inspector.json')
    args = parser.parse_args()
    raise SystemExit(0 if run(args.binary.absolute(), args.report) else 1)
