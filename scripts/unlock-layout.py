#!/usr/bin/env python3
"""Actual installed Unlock overflow and public status-outline regression.

Only a fresh disposable profile and public synthetic credentials are used.
Empty Unlock pixels are inspected in RAM before typing, never saved or OCRed.
No DOM instrumentation, secret clipboard transport or transaction is used.
"""
import argparse
import ast
import base64
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

from PIL import Image, ImageOps

ROOT = pathlib.Path(__file__).resolve().parents[1]
PASSWORD = 'public-unlock-layout-password-79326'
PHRASE = 'abandon ' * 11 + 'about'


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def hashes():
    return {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in
            ('scripts/unlock-layout.py', 'scripts/packaged-recovery.py', 'scripts/packaged-send.py',
             'tests/e2e/x11.py', 'tests/e2e/phrase.py', 'tests/e2e/local_geth.py', 'scripts/install.py')}


def selected_helper(name):
    # Reuse the already exercised test driver's exact metadata-query guard and
    # numeric fill projection. This executes repository test code, never code
    # in the installed application or an injected renderer/debugging channel.
    tree = ast.parse((ROOT / 'scripts/packaged-recovery.py').read_text())
    definition = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == name)
    namespace = {'C': C, 'mod': mod, 'json': json, 'os': os}
    exec(compile(ast.Module(body=[definition], type_ignores=[]), 'unlock-layout-test-helper', 'exec'), namespace)
    return namespace[name]


def wait(predicate, seconds=45):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        result = predicate()
        if result:
            return result
        time.sleep(.1)
    raise RuntimeError('Owned Unlock layout workflow exceeded its bounded deadline')


def stop(process, group=False):
    if process is None or process.poll() is not None:
        return
    if group:
        os.killpg(process.pid, signal.SIGTERM)
    else:
        process.terminate()
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        if group:
            os.killpg(process.pid, signal.SIGKILL)
        else:
            process.kill()
        process.wait(timeout=5)


def capture_public_status(expected_bounds=None):
    # Classification, geometry and pixel capture form one operation on this
    # test-owned X server. A private prompt cannot map between the guard and
    # image read. Only the public status crop survives this function in RAM.
    mod.X.XGrabServer(desktop.d)
    try:
        mod.X.XSync(desktop.d, 0)
        if desktop.secret_window():
            raise RuntimeError('Refusing public status inspection while a private prompt is open')
        windows = desktop.windows()
        if len(windows) != 1 or windows[0][1] != 'CryptoWallet':
            raise RuntimeError('Unexpected window during public status inspection')
        bounds = desktop.geometry(windows[0][0])
        x, y, width, height = bounds
        if width < 100 or height < 52 or (expected_bounds is not None and bounds != expected_bounds):
            raise RuntimeError('Public window geometry changed during status inspection')
        started_at = time.monotonic()
        picture = desktop.image().crop((x, y + height - 52, x + width, y + height)).convert('RGB')
        captured_at = time.monotonic()
        return {'picture': picture, 'bounds': bounds, 'startedAt': started_at, 'capturedAt': captured_at}
    finally:
        mod.X.XUngrabServer(desktop.d)
        mod.X.XFlush(desktop.d)


def status_text(picture):
    # Decode an already captured public crop, never a live/private window.
    # Slow OCR must not determine when the application was observed.
    picture = ImageOps.invert(picture.convert('L'))
    picture = picture.resize((picture.width * 2, picture.height * 2), Image.Resampling.LANCZOS)
    buffer = io.BytesIO()
    picture.save(buffer, format='PNG')
    output = subprocess.check_output(['tesseract', 'stdin', 'stdout', '--psm', '7'],
                                     input=buffer.getvalue(), stderr=subprocess.DEVNULL, timeout=20)
    return ' '.join(output.decode().lower().split())


def observe_status_expiry(cancelled_at, expected_bounds):
    samples = []
    for target, latest in ((6.8, 7.0), (8.0, 8.5)):
        time.sleep(max(0, cancelled_at + target - time.monotonic()))
        sample = capture_public_status(expected_bounds)
        if sample['capturedAt'] >= cancelled_at + latest:
            raise RuntimeError('Public status capture missed its bounded observation time')
        samples.append(sample)
    # Both immutable samples exist before either potentially slow decoder
    # starts. This retains the real seven-second UI check on busy CI runners.
    decoding_at = time.monotonic()
    before, after = (status_text(sample['picture']) for sample in samples)
    finished_at = time.monotonic()
    visible = 'request cancelled' in before
    absent = after == 'status'
    return {'preExpiryCaptureSecondsAfterCancel': round(samples[0]['capturedAt'] - cancelled_at, 3),
            'postExpiryCaptureSecondsAfterCancel': round(samples[1]['capturedAt'] - cancelled_at, 3),
            'captureIntervalsSecondsAfterCancel': [[round(sample[key] - cancelled_at, 3)
                                                   for key in ('startedAt', 'capturedAt')] for sample in samples],
            'maximumAbsenceCaptureSeconds': 8.5,
            'visibleBeforeExpiry': visible, 'absentAfterExpiry': absent,
            'ocrCompletedSecondsAfterCancel': round(finished_at - cancelled_at, 3),
            'ocrSeconds': round(finished_at - decoding_at, 3),
            'privatePixelsRetained': False}


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--report-prefix', default='unlock-layout')
args = parser.parse_args()
if not re.fullmatch(r'[a-z0-9-]{1,80}', args.report_prefix):
    raise SystemExit('Report prefix must be a simple test-owned name')
REPORTS = ROOT / 'reports'
REPORTS.mkdir(exist_ok=True)
report_path = REPORTS / (args.report_prefix + '.json')
report = {'status': 'RUNNING', 'scope': 'Uninstrumented installed app on owned private X11: actual empty Unlock internal scrollbar and wheel stability, complete input/OK geometry, public cancellation status without a rectangle, seven-second feedback lifetime; public fixture only',
          'sourceSHA256': hashes(), 'steps': [], 'notRun': ['clean Ubuntu/Wayland OS matrix', 'public funded network acceptance', 'mainnet spending', 'independent security review']}


def save():
    report_path.write_text(json.dumps(report, indent=2) + '\n')


def check(name, condition):
    report['steps'].append({'name': name, 'status': 'PASS' if condition else 'FAIL'})
    print(('PASS ' if condition else 'FAIL ') + name, flush=True)
    save()


app = xvfb = desktop = handler_refs = None
logs = []
started = time.monotonic()
mod = load('unlock_layout_x11', 'tests/e2e/x11.py')
mod.X.XGrabServer.argtypes = [C.c_void_p]
mod.X.XUngrabServer.argtypes = [C.c_void_p]
mod.X.XSync.argtypes = [C.c_void_p, C.c_int]
gui_module = load('unlock_layout_public_gui', 'scripts/packaged-send.py')
fill_rectangles = selected_helper('publicfillrectangles')
save()
try:
    from install import verify_binaries
    runtime = (ROOT / 'cryptowallet').resolve().parent
    launcher = runtime / 'cryptowallet'
    report['artifact'] = {'version': subprocess.check_output([str(launcher), '--version'], text=True, timeout=10).strip(),
                          'binaryManifest': verify_binaries(runtime),
                          'asarSHA256': hashlib.sha256((runtime / 'resources/app.asar').read_bytes()).hexdigest()}
    display = next((f':{n}' for n in range(180, 190)
                    if not pathlib.Path(f'/tmp/.X11-unix/X{n}').exists() and not pathlib.Path(f'/tmp/.X{n}-lock').exists()), None)
    if not display:
        raise RuntimeError('No free owned private display; refusing the user display')
    server = shutil.which('Xvfb') or str(ROOT / '.tools/xvfb/root/usr/bin/Xvfb')
    env = {**os.environ, 'DISPLAY': display}
    for key in ('WAYLAND_DISPLAY', 'XDG_RUNTIME_DIR', 'DBUS_SESSION_BUS_ADDRESS'):
        env.pop(key, None)
    if '.tools/' in server:
        env['LD_LIBRARY_PATH'] = str(ROOT / '.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
    xvlog = tempfile.TemporaryFile(mode='w+')
    logs.append(xvlog)
    xvfb = subprocess.Popen([server, display, '-screen', '0', '1280x1024x24', '-nolisten', 'tcp'], env=env, stdout=xvlog, stderr=xvlog)
    wait(lambda: pathlib.Path('/tmp/.X11-unix/X' + display[1:]).exists(), 10)
    desktop = mod.Desktop(display.encode())
    handler_refs = selected_helper('install_owned_window_query_handler')(desktop, report, report_path)
    gui = gui_module.Gui(desktop)
    with tempfile.TemporaryDirectory(prefix='cryptowallet-unlock-layout-') as temporary:
        owned = pathlib.Path(temporary)
        home = owned / 'home'
        home.mkdir(mode=0o700)
        profile = owned / 'profile'
        env.update(HOME=str(home), XDG_CONFIG_HOME=str(profile))

        def launch():
            global app
            log = tempfile.TemporaryFile(mode='w+')
            logs.append(log)
            app = subprocess.Popen([str(launcher), '--ozone-platform=x11'], env=env, stdout=log, stderr=log, start_new_session=True)
            desktop.wait('CryptoWallet', 20)
            time.sleep(.7)

        launch()
        gui.click_button('Restore from recovery phrase')
        gui.prompt('Create local wallet')
        desktop.inputs('Create local wallet', [PASSWORD, PASSWORD, PHRASE, ''])
        desktop.accept('Create local wallet')
        gui.prompt('Imported account recovery limits')
        desktop.accept('Imported account recovery limits')
        wait(lambda: not desktop.secret_window())
        wait(lambda: 'your wallets' in gui.text().lower())
        vault = profile / 'cryptowallet/mainnet/vault.lwv'
        vault_before = hashlib.sha256(vault.read_bytes()).hexdigest()
        check('Fresh disposable public-fixture phrase restore completes with an encrypted vault', True)
        stop(app, group=True)
        launch()
        wait(lambda: 'welcome back' in gui.text().lower())
        gui.click('Unlock wallet')
        gui.prompt('Unlock local wallet')

        def empty_geometry(stage):
            window, title = desktop.wait('Unlock local wallet')
            if title != 'CryptoWallet — Unlock local wallet':
                raise RuntimeError('Unexpected empty Unlock dialog identity')
            x, y, width, height = desktop.geometry(window)
            mod.T.XTestFakeMotionEvent(desktop.d, -1, 5, 5, 0)
            mod.X.XFlush(desktop.d)
            time.sleep(.1)
            # No typing has occurred in this newly opened window. RAM-only
            # private pixels flow solely to numeric color/rectangle checks.
            picture = desktop.image().convert('RGB')
            inputs = [box for box in fill_rectangles(picture, (x, y, x+width, y+height), {(11, 19, 31)})
                      if box[2]-box[0] >= width*.6 and 35 <= box[3]-box[1] <= 60]
            buttons = [box for box in fill_rectangles(picture, (x, y+height//2, x+width, y+height), {(66, 105, 170)})
                       if 80 <= box[2]-box[0] <= 150 and 32 <= box[3]-box[1] <= 50]
            if len(inputs) != 1 or len(buttons) != 1:
                raise RuntimeError('Empty Unlock controls are clipped or ambiguous; no private pixels retained')
            field, button = inputs[0], buttons[0]
            # Native classic dark/light scrollbar rails are neutral gray. The
            # prompt's real backgrounds are colored. Inspect only its right
            # main-content edge between the input label and footer, recording
            # counts rather than private pixels, text, colors or image bytes.
            top = max(y, field[1]-30)
            bottom = button[1]-15
            columns = []
            for xx in range(x+width-18, x+width-1):
                count = sum(max(picture.getpixel((xx, yy)))-min(picture.getpixel((xx, yy))) <= 2
                            and min(picture.getpixel((xx, yy))) >= 10 for yy in range(top, bottom))
                if count >= 25:
                    columns.append(xx)
            relative = lambda box: [value-(x if index % 2 == 0 else y) for index, value in enumerate(box)]
            sample = {'stage': stage, 'nativeSize': [width, height], 'inputBounds': relative(field),
                      'okBounds': relative(button), 'inputToOKGapUpperBoundPixels': button[1]-field[3],
                      'leftInputClearance': field[0]-x, 'rightInputClearance': x+width-field[2],
                      'nativeRailColumns': len(columns), 'privatePixelsRetained': False}
            del picture, inputs, buttons, field, button
            return sample, (window, x, y, width, height)

        observations = []
        first, bounds = empty_geometry('before wheel')
        observations.append(first)
        for button, stage in ((5, 'after downward wheel'), (4, 'after upward wheel')):
            window, x, y, width, height = bounds
            desktop.focus(window)
            # Wheel over main's left padding; no field editing or renderer event
            # injection. Both directions detect initial autofocus at either end.
            mod.T.XTestFakeMotionEvent(desktop.d, -1, x+10, y+first['inputBounds'][1]+5, 0)
            for _ in range(10):
                mod.T.XTestFakeButtonEvent(desktop.d, button, 1, 0)
                mod.T.XTestFakeButtonEvent(desktop.d, button, 0, 0)
            mod.X.XFlush(desktop.d)
            time.sleep(.2)
            observation, bounds = empty_geometry(stage)
            observations.append(observation)
        report['emptyUnlockGeometry'] = observations
        check('Empty Unlock has no native internal scrollbar rail or scrollbar-width loss',
              all(row['nativeRailColumns'] == 0 and abs(row['leftInputClearance']-row['rightInputClearance']) <= 2 for row in observations))
        check('Real wheel input in either direction cannot scroll the empty Unlock form',
              all(row['inputBounds'] == first['inputBounds'] and row['okBounds'] == first['okBounds'] for row in observations))
        check('Compact Unlock keeps its complete input and OK button visible with a small gap',
              all(row['nativeSize'] == [620, 304] and row['inputBounds'][3]-row['inputBounds'][1] >= 46
                  and row['okBounds'][3]-row['okBounds'][1] >= 42 and 0 <= row['inputToOKGapUpperBoundPixels'] <= 48 for row in observations))
        cancelled_at = time.monotonic()
        desktop.key('Escape')
        wait(lambda: not desktop.secret_window())
        wait(lambda: 'request cancelled' in gui.text().lower())
        x, y, width, height = desktop.geometry(desktop.wait('CryptoWallet')[0])

        bounds = (x, y, width, height)
        picture = capture_public_status(bounds)['picture']
        outline_colors = {(232, 166, 157), (232, 177, 165), (245, 201, 160)}
        long_runs = []
        for yy in range(52):
            run = longest = 0
            for xx in range(3, width-25):
                run = run+1 if picture.getpixel((xx, yy)) in outline_colors else 0
                longest = max(longest, run)
            # Also reject a compact rectangle around a short status string.
            # Ordinary alert text uses a different color and the former lone
            # left accent was only three pixels wide.
            if longest >= 40:
                long_runs.append({'rowWithinStatusbar': yy, 'horizontalPixels': longest})
        report['publicStatusOutline'] = {'longOutlineRuns': long_runs, 'privatePixelsRetained': False}
        picture.save(REPORTS / (args.report_prefix + '-status.png'))
        check('Focused public error status has no rectangle around its text', not long_runs)
        timing = observe_status_expiry(cancelled_at, bounds)
        report['publicStatusTiming'] = timing
        check('Public error feedback is readable in the 6.8-second capture and absent in the capture by 8.5 seconds',
              timing['visibleBeforeExpiry'] and timing['absentAfterExpiry'])
        check('Cancelling empty Unlock preserves the encrypted wallet byte-for-byte', hashlib.sha256(vault.read_bytes()).hexdigest() == vault_before)
        stop(app, group=True)
        for log in logs:
            log.flush()
            log.seek(0)
            data = log.read().encode()
            needles = [value for secret in (PASSWORD.encode(), PHRASE.encode()) for value in (secret, secret.hex().encode(), base64.b64encode(secret))]
            if any(value in data for value in needles):
                raise RuntimeError('Synthetic secret canary appeared in captured process logs')
        check('Captured app logs contain none of the tested password/phrase canaries', True)
    if hashes() != report['sourceSHA256']:
        raise RuntimeError('Targeted layout test source or helper changed while running')
    report['sourceStable'] = True
    report['status'] = 'PASS' if all(step['status'] == 'PASS' for step in report['steps']) else 'FAIL'
except Exception as error:
    report.update(status='FAIL', sourceStable=False,
                  reason=str(error) if isinstance(error, RuntimeError) else 'Owned targeted layout automation failed; no private diagnostic retained')
    print(report['reason'], flush=True)
finally:
    stop(app, group=True)
    if handler_refs is not None:
        handler_refs[2]()
    stop(xvfb)
    # Stop the app before deleting any cache paths recreated during failure
    # unwinding. This tree belongs solely to the disposable test run.
    if 'owned' in globals() and owned.exists():
        shutil.rmtree(owned)
    for log in logs:
        log.close()
    report['cleanup'] = {'walletExited': app is None or app.poll() is not None,
                         'ownedXvfbExited': xvfb is None or xvfb.poll() is not None,
                         'temporaryProfilesRemoved': 'owned' not in globals() or not owned.exists()}
    report['seconds'] = round(time.monotonic()-started, 3)
    save()
print(json.dumps(report, indent=2))
raise SystemExit(report['status'] != 'PASS')
