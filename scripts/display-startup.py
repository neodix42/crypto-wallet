#!/usr/bin/env python3
"""Probe an uninstrumented portable wallet on owned disposable displays only.

X11 checks a mapped public window; virtual Wayland checks process startup only.
This does not certify hardware displays, native input, or the supported OS matrix.
"""
import argparse
import hashlib
import importlib.util
import json
import os
import pathlib
import select
import shutil
import signal
import subprocess
import tempfile
import time

from install import verify_binaries

root = pathlib.Path(__file__).resolve().parents[1]
reports = root / 'reports'


def stop(process):
    if process is None or process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        process.wait()
        return
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)


def wait_for(check, process, seconds=20):
    deadline = time.monotonic() + seconds
    while not check():
        if process.poll() is not None:
            raise RuntimeError('Owned display process stopped during startup')
        if time.monotonic() >= deadline:
            raise RuntimeError('Owned display did not become ready')
        time.sleep(.1)


def probe(backend, launcher):
    result = {'backend': backend, 'status': 'NOT_RUN'}
    server = shutil.which('Xvfb')
    local = root / '.tools/xvfb/root/usr/bin/Xvfb'
    if backend == 'x11' and not server and local.is_file():
        server = str(local)
    required = ['dbus-daemon'] + (['kwin_wayland'] if backend == 'wayland' else [])
    missing = [name for name in required if not shutil.which(name)]
    if backend == 'x11' and not server:
        missing.append('Xvfb')
    if missing:
        return {**result, 'status': 'BLOCKED', 'reason': 'Missing optional test prerequisites: ' + ', '.join(missing)}
    display = None
    if backend == 'x11':
        display = next((f':{n}' for n in range(189, 179, -1) if not pathlib.Path(f'/tmp/.X11-unix/X{n}').exists() and not pathlib.Path(f'/tmp/.X{n}-lock').exists()), None)
        if display is None:
            return {**result, 'status': 'BLOCKED', 'reason': 'No unused owned X11 test display'}

    processes = []
    app_started = False
    temporary = tempfile.TemporaryDirectory(prefix='cryptowallet-display-fixture-')
    folder = pathlib.Path(temporary.name)
    env = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
    for name, relative in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'), ('XDG_CACHE_HOME', 'cache'), ('XDG_DATA_HOME', 'data'), ('XDG_STATE_HOME', 'state'), ('XDG_RUNTIME_DIR', 'runtime'), ('TMPDIR', 'tmp')]:
        path = folder / relative
        path.mkdir(mode=0o700)
        env[name] = str(path)
    with tempfile.TemporaryFile(mode='w+') as bus_log, tempfile.TemporaryFile(mode='w+') as display_log, tempfile.TemporaryFile(mode='w+') as app_log:
        try:
            bus = subprocess.Popen(['dbus-daemon', '--session', '--nofork', '--nopidfile', '--print-address=1', '--address=unix:path=' + str(folder / 'runtime/session-bus')], env=env, stdout=subprocess.PIPE, stderr=bus_log, text=True, start_new_session=True)
            processes.append(bus)
            # A local daemon prints one public Unix address before activation.
            if not select.select([bus.stdout], [], [], 5)[0]:
                raise RuntimeError('Private session bus did not publish an address')
            address = bus.stdout.readline().strip()
            if not address.startswith('unix:'):
                raise RuntimeError('Private session bus is not Unix-only')
            env['DBUS_SESSION_BUS_ADDRESS'] = address
            if backend == 'x11':
                env['DISPLAY'] = display
                server_env = dict(env)
                if pathlib.Path(server) == local:
                    server_env['LD_LIBRARY_PATH'] = str(root / '.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
                command = [server, display, '-screen', '0', '1280x1024x24', '-nolisten', 'tcp']
                display_process = subprocess.Popen(command, env=server_env, stdout=display_log, stderr=display_log, start_new_session=True)
                processes.append(display_process)
                wait_for(lambda: pathlib.Path('/tmp/.X11-unix/X' + display[1:]).exists(), display_process)
                spec = importlib.util.spec_from_file_location('owned_x11', root / 'tests/e2e/x11.py')
                module = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(module)
                desktop = module.Desktop(display.encode())
            else:
                env['WAYLAND_DISPLAY'] = 'wallet-fixture-wayland'
                command = ['kwin_wayland', '--virtual', '--no-lockscreen', '--no-global-shortcuts', '--no-kactivities', '--socket', env['WAYLAND_DISPLAY'], '--width', '1280', '--height', '1024']
                display_process = subprocess.Popen(command, env=env, stdout=display_log, stderr=display_log, start_new_session=True)
                processes.append(display_process)
                wait_for(lambda: (folder / 'runtime' / env['WAYLAND_DISPLAY']).is_socket(), display_process)
            app = subprocess.Popen([str(launcher), '--ozone-platform=' + backend], env=env, stdout=app_log, stderr=app_log, start_new_session=True)
            processes.append(app)
            app_started = True
            if backend == 'x11':
                desktop.wait('CryptoWallet', 20)
            time.sleep(5)
            if app.poll() is not None:
                raise RuntimeError('Portable wallet exited during display startup')
            # Read before cleanup: forced group shutdown can emit unrelated
            # GPU/network-service retry messages while children are terminating.
            app_log.flush()
            app_log.seek(0)
            output = app_log.read()
            result.update(log=output, mesaWarning='MESA-LOADER:' in output)
            if result['mesaWarning']:
                raise RuntimeError('Mesa loader warning during live startup')
            if "Error occurred in handler for 'wallet:request'" in output:
                raise RuntimeError('Unexpected wallet request handler trace during startup')
            if backend == 'x11':
                screenshot = reports / 'display-startup-x11.png'
                desktop.screenshot(screenshot)
                result['screenshotSHA256'] = hashlib.sha256(screenshot.read_bytes()).hexdigest()
            result.update(status='PASS', scope='Mapped public X11 window and process alive for 5 seconds' if backend == 'x11' else 'Private virtual Wayland startup process alive for 5 seconds; window visibility/input not verified', displayCommand=command, compositorGPUEnvironmentOverrides=[])
        except (FileNotFoundError, ImportError) as error:
            result.update(status='BLOCKED', reason=str(error))
        except Exception as error:
            result.update(status='FAIL' if app_started else 'BLOCKED', reason=str(error))
            app_log.seek(0)
            result['log'] = app_log.read()
        finally:
            for process in reversed(processes):
                stop(process)
            display_log.seek(0)
            result['displayLog'] = display_log.read()
            # Private desktop portals may leave a disconnected mount entry for
            # a moment after the owned session bus exits. Retry only this tree.
            for attempt in range(5):
                try:
                    temporary.cleanup()
                    break
                except OSError as error:
                    if attempt == 4:
                        result.update(status='FAIL', reason='Owned temporary display cleanup failed: ' + str(error))
                    time.sleep(.2)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend', choices=['all', 'x11', 'wayland'], default='all')
    args = parser.parse_args()
    reports.mkdir(exist_ok=True)
    launcher = (root / 'cryptowallet').resolve() if (root / 'cryptowallet').exists() else root / 'dist/CryptoWallet-linux-x64/cryptowallet'
    report = {'status': 'NOT_RUN', 'scope': 'Uninstrumented portable startup on owned disposable displays; no wallet creation, secret input or user profile access', 'notRun': ['actual host hardware/display reproduction', 'native Wayland credential/backup workflows', 'stock Ubuntu OS/display acceptance matrix'], 'rendererPolicy': 'Expected hardware acceleration disabled before ready; separately enforced by frontend startup tests alongside sandbox retention', 'electronAPI': 'https://www.electronjs.org/docs/latest/api/app#appdisablehardwareacceleration', 'backends': []}
    try:
        runtime = launcher.parent
        report['artifact'] = {'version': subprocess.check_output([str(launcher), '--version'], env={'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8'}, text=True, timeout=10).strip(), 'binaryManifest': verify_binaries(runtime), 'asarSHA256': hashlib.sha256((runtime / 'resources/app.asar').read_bytes()).hexdigest()}
        report['backends'] = [probe(backend, launcher) for backend in (['x11', 'wayland'] if args.backend == 'all' else [args.backend])]
        states = {row['status'] for row in report['backends']}
        report['status'] = 'FAIL' if 'FAIL' in states else 'BLOCKED' if 'BLOCKED' in states else 'PASS'
    except Exception as error:
        report.update(status='FAIL', reason=str(error))
    (reports / 'display-startup.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return {'PASS': 0, 'BLOCKED': 2}.get(report['status'], 1)


if __name__ == '__main__':
    raise SystemExit(main())
