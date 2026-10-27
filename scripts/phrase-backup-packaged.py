#!/usr/bin/env python3
"""Exercise first-run import and authenticated phrase backup in the real package.

Only public synthetic fixtures and fresh private profiles are used. All phrase
pixels/OCR remain in RAM; no screenshot, plaintext export, secret clipboard,
renderer instrumentation, debug endpoint or funded payment is used.
"""
import argparse
import ast
import base64
import ctypes as C
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import shutil
import signal
import subprocess
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
PASSWORD = 'public-phrase-backup-password-79326'
WRONG_PASSWORD = 'incorrect-public-backup-password-79326'
PHRASE = 'abandon ' * 11 + 'about'
BIP39_PASSPHRASE = 'TREZOR'  # Published BIP39 vector; never a user credential.
EXPECTED_ADDRESS = '0x9858EfFD232B4033E47d90003D41EC34EcaEda94'
CONFIRM_TITLE = 'Back up recovery phrase'
PASSWORD_TITLE = 'Confirm app password to back up recovery phrase'
PASSPHRASE_TITLE = 'Keep your separate BIP39 passphrase'


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def hashes():
    return {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in
            ('scripts/phrase-backup-packaged.py', 'scripts/packaged-recovery.py', 'scripts/packaged-send.py',
             'tests/e2e/x11.py', 'tests/e2e/phrase.py', 'tests/e2e/local_geth.py', 'scripts/install.py')}


def helper(name):
    # Extract an existing test helper without running the canonical driver's
    # top-level test. No code is evaluated in the installed application.
    tree = ast.parse((ROOT / 'scripts/packaged-recovery.py').read_text())
    node = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == name)
    namespace = {'C': C, 'mod': mod, 'json': json, 'os': os}
    exec(compile(ast.Module(body=[node], type_ignores=[]), 'phrase-backup-test-helper', 'exec'), namespace)
    return namespace[name]


def stop(process, group=False):
    if process is None or process.poll() is not None:
        return
    os.killpg(process.pid, signal.SIGTERM) if group else process.terminate()
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL) if group else process.kill()
        process.wait(timeout=5)


def wait(predicate, seconds=45):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if app is not None and app.poll() is not None:
            raise RuntimeError('Owned packaged wallet exited during the phrase-backup workflow')
        value = predicate()
        if value:
            return value
        time.sleep(.1)
    raise RuntimeError('Owned packaged phrase-backup workflow exceeded its bounded deadline')


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--report-prefix', default='phrase-backup-packaged')
args = parser.parse_args()
if not re.fullmatch(r'[a-z0-9-]{1,80}', args.report_prefix):
    raise SystemExit('Report prefix must be a simple test-owned name')
reports = ROOT / 'reports'
reports.mkdir(exist_ok=True)
report_path = reports / (args.report_prefix + '.json')
report = {'status': 'RUNNING', 'sourceSHA256': hashes(), 'steps': [],
          'scope': 'Uninstrumented installed app on private X11: first-run phrase import, Settings Wallet confirmation and per-view app-password authentication, ordered numbered backup words, wrong password/cancellation, repeat display, optional imported BIP39-passphrase warning, unchanged ciphertext and public clipboard, locked/restart protection; synthetic public fixtures only',
          'notRun': ['separate physical machines', 'clean supported Ubuntu X11/Wayland OS matrix',
                     'OS suspend/session-lock races', 'public funded network acceptance', 'mainnet spending',
                     'independent security review']}


def save():
    report_path.write_text(json.dumps(report, indent=2) + '\n')


def passed(name):
    report['steps'].append({'name': name, 'status': 'PASS'})
    print('PASS ' + name, flush=True)
    save()


app = xvfb = desktop = handler_refs = None
logs = []
started = time.monotonic()
mod = load('phrase_backup_test_x11', 'tests/e2e/x11.py')
phrase_ui = load('phrase_backup_test_words', 'tests/e2e/phrase.py')
gui_module = load('phrase_backup_test_gui', 'scripts/packaged-send.py')
save()
try:
    from install import verify_binaries
    runtime = (ROOT / 'wallet').resolve().parent
    launcher = runtime / 'wallet-launcher'
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
    handler_refs = helper('install_owned_window_query_handler')(desktop, report, report_path)
    gui = gui_module.Gui(desktop)
    os.environ['DISPLAY'] = display
    os.environ['GDK_BACKEND'] = 'x11'
    for key in ('WAYLAND_DISPLAY', 'XDG_RUNTIME_DIR', 'DBUS_SESSION_BUS_ADDRESS'):
        os.environ.pop(key, None)
    clipboard = helper('clip')
    with tempfile.TemporaryDirectory(prefix='localwallet-phrase-backup-') as temporary:
        owned = pathlib.Path(temporary)
        home = owned / 'home'
        home.mkdir(mode=0o700)
        env['HOME'] = str(home)
        os.environ['HOME'] = str(home)
        profile = owned / 'profile'

        def launch(target):
            global app, profile
            profile = target
            env['XDG_CONFIG_HOME'] = str(profile)
            log = tempfile.TemporaryFile(mode='w+')
            logs.append(log)
            app = subprocess.Popen([str(launcher), '--ozone-platform=x11'], env=env, stdout=log, stderr=log, start_new_session=True)
            desktop.wait('LocalWallet', 20)
            time.sleep(.7)

        def public():
            wait(lambda: not desktop.secret_window() and
                 all(title == 'LocalWallet' for _, title in desktop.windows()))

        def wallet_settings():
            desktop.focus(desktop.wait('LocalWallet')[0])
            gui.click('Settings')
            rows = gui.ocr()
            nav_top = min(int(row[7]) for row in rows if row[11].lower() == 'settings')
            matches = [row for row in rows if row[11].lower() == 'wallet' and int(row[7]) > nav_top + 25]
            if not matches:
                raise RuntimeError('Settings Wallet section is unavailable')
            row = min(matches, key=lambda value: (int(value[7]), int(value[6])))
            desktop.click(int(row[6]) + int(row[8]) // 2, int(row[7]) + int(row[9]) // 2)
            time.sleep(.2)

        def state():
            # These are encrypted vault generations only. Observing their exact
            # bytes also proves no hidden recovery/generation write occurred.
            folder = profile / 'localwallet/mainnet'
            return {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                    for path in folder.glob('vault*.lwv')}

        def unchanged(before):
            if state() != before:
                raise RuntimeError('Phrase backup or cancellation changed an encrypted vault generation')

        def import_fixture(passphrase=''):
            gui.click_button('Restore from recovery phrase')
            gui.prompt('Create local wallet')
            desktop.inputs('Create local wallet', [PASSWORD, PASSWORD, PHRASE, passphrase])
            desktop.accept('Create local wallet')
            gui.prompt('Imported account recovery limits')
            desktop.accept('Imported account recovery limits')
            public()
            wait(lambda: 'your wallets' in gui.text().lower())

        def begin_backup():
            wallet_settings()
            gui.click('Back up recovery phrase')
            gui.prompt(CONFIRM_TITLE)
            if not desktop.secret_window():
                raise RuntimeError('Backup confirmation is outside the protected prompt classification')

        def authenticate(value=PASSWORD):
            desktop.accept(CONFIRM_TITLE)
            gui.prompt(PASSWORD_TITLE)
            desktop.inputs(PASSWORD_TITLE, [value])
            desktop.accept(PASSWORD_TITLE)

        def read_backup():
            gui.prompt(CONFIRM_TITLE)
            if not desktop.secret_window():
                raise RuntimeError('Recovery backup is outside the protected secret-window classification')
            result = phrase_ui.numbered(desktop, CONFIRM_TITLE)
            if result != PHRASE.encode('ascii'):
                raise RuntimeError('Displayed numbered backup words do not match the disposable recovery root')
            # No recovered words or hashes of them enter a report. The public
            # fixture's independently known address was separately verified.
            del result

        launch(profile)
        text = gui.text().lower()
        if 'create app password' not in text or 'restore from recovery phrase' not in text:
            raise RuntimeError('Verified first-run profile does not offer both create and phrase import')
        gui.click_button('Restore from recovery phrase')
        gui.prompt('Create local wallet')
        desktop.key('Escape')
        public()
        if state():
            raise RuntimeError('Cancelling first-run phrase import created a vault')
        passed('First run offers create and phrase import; cancelling isolated import leaves the profile empty')
        import_fixture()
        gui.click('Ethereum')
        wait(lambda: 'balance' in gui.text().lower())
        gui.click('Copy verified address')
        gui.prompt('Verify receive address')
        desktop.accept('Verify receive address')
        public()
        if clipboard() != EXPECTED_ADDRESS:
            raise RuntimeError('First-run import differs from the independent public Ethereum fixture')
        before = state()
        if 'vault.lwv' not in before:
            raise RuntimeError('Fixture restore did not create the encrypted vault')
        passed('First-run isolated phrase import restores the independently known Ethereum receive address')

        begin_backup()
        desktop.key('Escape')
        public()
        unchanged(before)
        passed('Cancelling the backup confirmation reveals no phrase and preserves encrypted generations')

        begin_backup()
        desktop.accept(CONFIRM_TITLE)
        gui.prompt(PASSWORD_TITLE)
        desktop.key('Escape')
        public()
        unchanged(before)
        passed('Cancelling required backup-password entry reveals no phrase and preserves encrypted generations')

        begin_backup()
        authenticate(WRONG_PASSWORD)
        public()
        unchanged(before)
        passed('An incorrect app password cannot reveal the recovery phrase or modify the vault')

        begin_backup()
        authenticate()
        read_backup()
        desktop.key('a', ctrl=True)
        desktop.key('c', ctrl=True)
        if clipboard() != EXPECTED_ADDRESS:
            raise RuntimeError('Recovery backup changed the public address clipboard')
        desktop.accept(CONFIRM_TITLE)
        public()
        unchanged(before)
        passed('Authenticated backup displays all twelve numbered words in order without a new word challenge')
        passed('Recovery words cannot be copied with keyboard selection and the public address clipboard stays unchanged')

        begin_backup()
        authenticate()
        read_backup()
        desktop.key('Escape')
        public()
        unchanged(before)
        if clipboard() != EXPECTED_ADDRESS:
            raise RuntimeError('Repeated recovery backup changed the public address clipboard')
        passed('A second backup requires fresh confirmation and app-password authentication for the same unchanged root')

        gui.click('Lock wallet')
        public()
        wait(lambda: 'welcome back' in gui.text().lower())
        wallet_settings()
        if 'back up recovery phrase' in gui.text().lower():
            raise RuntimeError('A locked wallet still exposes its phrase-backup action')
        unchanged(before)
        passed('Lock removes the phrase-backup action and retains encrypted generations')
        stop(app, group=True)
        launch(profile)
        wait(lambda: 'welcome back' in gui.text().lower())
        if desktop.secret_window():
            raise RuntimeError('Restart restored a private phrase window without explicit authorization')
        gui.click('Unlock wallet')
        gui.prompt('Unlock local wallet')
        desktop.inputs('Unlock local wallet', [PASSWORD])
        desktop.accept('Unlock local wallet')
        public()
        wait(lambda: 'your wallets' in gui.text().lower())
        wallet_settings()
        if 'back up recovery phrase' not in gui.text().lower():
            raise RuntimeError('Unlocked restarted wallet lost its explicit backup action')
        # Unlock legitimately persists journal recovery and reencrypts the vault.
        # Require this new backup to preserve the post-unlock generation, while
        # the displayed original fixture proves the root itself stayed stable.
        restarted_before = state()
        begin_backup()
        authenticate()
        read_backup()
        desktop.accept(CONFIRM_TITLE)
        public()
        unchanged(restarted_before)
        passed('Restart begins locked and fresh authenticated backup still shows the original unchanged root')
        stop(app, group=True)

        launch(owned / 'passphrase-profile')
        import_fixture(BIP39_PASSPHRASE)
        passphrase_before = state()
        begin_backup()
        authenticate()
        gui.prompt(PASSPHRASE_TITLE)
        desktop.key('Escape')
        public()
        unchanged(passphrase_before)
        passed('A nonempty imported BIP39 passphrase requires its separate recovery warning; cancel prevents phrase display')
        begin_backup()
        authenticate()
        gui.prompt(PASSPHRASE_TITLE)
        desktop.accept(PASSPHRASE_TITLE)
        read_backup()
        desktop.accept(CONFIRM_TITLE)
        public()
        unchanged(passphrase_before)
        passed('After acknowledging the separate passphrase requirement, backup shows the same numbered root words')
        stop(app, group=True)

        needles = [value for secret in (PASSWORD.encode(), WRONG_PASSWORD.encode(), PHRASE.encode())
                   for value in (secret, secret.hex().encode(), base64.b64encode(secret))]
        for path in owned.rglob('*'):
            if path.is_file() and path.stat().st_size < 32 << 20:
                if any(value in path.read_bytes() for value in needles):
                    raise RuntimeError('Tested synthetic password or phrase canary appeared in persisted app files')
        for log in logs:
            log.flush()
            log.seek(0)
            data = log.read().encode()
            if any(value in data for value in needles):
                raise RuntimeError('Tested synthetic password or phrase canary appeared in captured logs')
        passed('Captured logs and persisted profiles contain none of the tested phrase/password canaries')
    if hashes() != report['sourceSHA256']:
        raise RuntimeError('Phrase-backup producer or test helper changed during execution')
    if verify_binaries(runtime) != report['artifact']['binaryManifest'] or hashlib.sha256((runtime / 'resources/app.asar').read_bytes()).hexdigest() != report['artifact']['asarSHA256']:
        raise RuntimeError('Packaged artifact changed during the phrase-backup test')
    report.update(status='PASS', sourceStable=True, privatePixelsRetained=False,
                  secretClipboardTransport=False, productionInstrumentation=False)
except Exception as error:
    report.update(status='FAIL', sourceStable=False,
                  reason=str(error) if isinstance(error, RuntimeError) else 'Owned phrase-backup automation failed; no secret diagnostic retained')
    print(report['reason'], flush=True)
finally:
    stop(app, group=True)
    if handler_refs is not None:
        handler_refs[2]()
    stop(xvfb)
    # On failure the still-running app can recreate cache paths while the
    # TemporaryDirectory unwinds. Remove only our owned test tree after exit.
    if 'owned' in globals() and owned.exists():
        shutil.rmtree(owned)
    for log in logs:
        log.close()
    report['cleanup'] = {'walletExited': app is None or app.poll() is not None,
                         'ownedXvfbExited': xvfb is None or xvfb.poll() is not None,
                         'temporaryProfilesRemoved': 'owned' not in globals() or not owned.exists()}
    report['seconds'] = round(time.monotonic() - started, 3)
    save()
print(json.dumps(report, indent=2))
raise SystemExit(report['status'] != 'PASS')
