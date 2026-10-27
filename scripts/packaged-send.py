#!/usr/bin/env python3
"""Uninstrumented Electron Send -> real private geth -> independent execution.

Only public synthetic fixtures and genesis-created local units are used. Both
the app and actual node run in a fresh loopback-only network namespace, as an
ordinary UID. No public chain is reachable, and no transaction is mocked.
"""
import argparse
import base64
import contextlib
import ctypes
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORTS = ROOT / 'reports'


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


node_fixture = load('packaged_real_geth', 'tests/e2e/local_geth.py')
PASSWORD = 'public-packaged-send-password-79326'
PHRASE = 'abandon ' * 11 + 'about'
AMOUNT = 12345678912345678
AMOUNT_DECIMAL = '0.012345678912345678'


class Blocked(RuntimeError):
    pass


def snapshot():
    names = ['scripts/packaged-send.py', 'tests/e2e/local_geth.py', 'tests/e2e/x11.py',
             'tests/oracles/evm/geth-genesis.json']
    return {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in names}


def save(report):
    REPORTS.mkdir(exist_ok=True)
    temporary = REPORTS / 'packaged-send.tmp'
    temporary.write_text(json.dumps(report, indent=2) + '\n')
    temporary.replace(REPORTS / 'packaged-send.json')


def wait(predicate, seconds=45):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = predicate()
        if result:
            return result
        time.sleep(.15)
    raise RuntimeError('Owned packaged Send workflow exceeded its deadline')


class Gui:
    def __init__(self, desktop):
        self.desktop = desktop

    def ocr(self, public_prompt=None, threshold=140):
        from PIL import Image, ImageOps
        titles = [title for _, title in self.desktop.windows()]
        if public_prompt is not None:
            expected = 'CryptoWallet — ' + public_prompt
            if expected not in titles or any(title.startswith('CryptoWallet — ') and title != expected for title in titles):
                raise RuntimeError('Unexpected prompt during public approval inspection')
        elif self.desktop.secret_window():
            raise RuntimeError('Refusing to OCR a credential or recovery dialog')
        # No image or OCR temporary file is ever saved, including failure paths.
        picture = self.desktop.image()
        if public_prompt is not None:
            window = next(window for window, title in self.desktop.windows() if title == 'CryptoWallet — ' + public_prompt)
            x, y, width, height = self.desktop.geometry(window)
            picture = picture.crop((x, y, x + width, y + height))
        picture = ImageOps.invert(picture.convert('L'))
        picture = picture.resize((picture.width * 2, picture.height * 2), Image.Resampling.LANCZOS).point(lambda value: 0 if value < threshold else 255)
        buffer = io.BytesIO()
        picture.save(buffer, format='PNG')
        output = subprocess.check_output(['tesseract', 'stdin', 'stdout', 'tsv'], input=buffer.getvalue(), stderr=subprocess.DEVNULL, timeout=20).decode()
        rows = []
        for line in output.splitlines()[1:]:
            columns = line.split('\t')
            if len(columns) == 12 and columns[11].strip():
                for number in range(6, 10):
                    columns[number] = str(int(columns[number]) // 2)
                rows.append(columns)
        return rows

    def text(self, **options):
        return ' '.join(row[11] for row in self.ocr(**options))

    def click(self, phrase, last=False):
        wanted = phrase.lower().split()
        for attempt in range(16):
            for threshold in (140, 90):
                rows = self.ocr(threshold=threshold)
                matches = []
                for index, row in enumerate(rows):
                    segment = rows[index:index + len(wanted)]
                    words = [value[11].lower().strip('.,:') for value in segment]
                    if words == wanted and all(value[1:5] == row[1:5] for value in segment):
                        matches.append(row)
                if matches:
                    row = matches[-1] if last else matches[0]
                    self.desktop.focus(self.desktop.wait('CryptoWallet')[0])
                    self.desktop.click(int(row[6]) + int(row[8]) // 2, int(row[7]) + int(row[9]) // 2)
                    return
            self.desktop.key('Home', ctrl=True) if attempt in (0, 8) else self.desktop.key('Page_Down')
            time.sleep(.2)
        raise RuntimeError('Required public packaged Send control was unavailable')

    def click_button(self, phrase):
        # Explanatory prose can repeat a button's caption. Require the label's
        # actual normal/hover control fill, using public pixels only. This
        # narrow selector is used for the first-run phrase-import button.
        wanted = phrase.lower().split()
        for attempt in range(12):
            for threshold in (140, 90):
                rows = self.ocr(threshold=threshold)
                picture = self.desktop.image().convert('RGB')
                for index, row in enumerate(rows):
                    segment = rows[index:index + len(wanted)]
                    if [value[11].lower().strip('.,:') for value in segment] != wanted or not all(value[1:5] == row[1:5] for value in segment):
                        continue
                    x, y = int(row[6]), int(row[7]) + int(row[9]) // 2
                    if picture.getpixel((max(0, x-8), y)) not in ((36, 54, 80), (50, 76, 112)):
                        continue
                    self.desktop.focus(self.desktop.wait('CryptoWallet')[0])
                    self.desktop.click(x + int(row[8]) // 2, y)
                    return
            self.desktop.key('Home', ctrl=True) if attempt in (0, 6) else self.desktop.key('Page_Down')
            time.sleep(.2)
        raise RuntimeError('Actual first-run phrase-import button could not be identified from public pixels')

    def prompt(self, prefix):
        try:
            window, _ = self.desktop.wait(prefix, 45)
        except RuntimeError:
            titles = [title for _, title in self.desktop.windows()]
            if not self.desktop.secret_window():
                self.desktop.focus(self.desktop.wait('CryptoWallet')[0])
                self.desktop.key('Home', ctrl=True)
                public = self.text().lower()
                known = ('wallet locked', 'unlock wallet', 'waiting for go wallet', 'request completed',
                         'request was cancelled', 'operation failed', 'check wallet state',
                         'could not', '127.0.0.1:18547', '1000 eth', 'request unavailable',
                         'request failed', 'request cancelled', 'welcome back', 'your wallets')
                print('Public workflow state: ' + json.dumps({value: value in public for value in known}), flush=True)
            print('Owned mapped window state: ' + json.dumps({'mainWindow': 'CryptoWallet' in titles,
                'errorNotice': 'CryptoWallet — Wallet request failed' in titles}), flush=True)
            raise
        self.desktop.focus(window)
        return window


def stop_process(process):
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def inside(parent_namespace, owned_directory):
    start = time.monotonic()
    report = {'status': 'RUNNING', 'sourceSHA256': snapshot(), 'steps': [],
        'scope': 'Uninstrumented portable Electron/Go Send on private X11 into actual pinned geth Engine-API chain; public genesis-funded fixture only',
        'publicNetworkTests': 'NOT_RUN', 'mainnetSpending': 'NOT_APPLICABLE: no outside network route',
        'osMatrix': 'NOT_RUN: same-host private Xvfb and user/network namespaces'}
    save(report)
    app = xvfb = node = None
    logs = []
    def passed(name):
        report['steps'].append({'name': name, 'status': 'PASS'})
        save(report)
        print('PASS ' + name, flush=True)
    try:
        if os.getuid() == 0 or os.readlink('/proc/self/ns/net') == parent_namespace:
            raise Blocked('Ordinary-user isolated network namespace is unavailable')
        subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True, capture_output=True, timeout=5)
        if {name for _, name in socket.if_nameindex()} != {'lo'}:
            raise Blocked('Network isolation has an unexpected non-loopback interface')
        report['networkIsolation'] = {'uid': os.getuid(), 'interfaces': ['lo'],
            'networkNamespace': os.readlink('/proc/self/ns/net'), 'externalRoutes': False}
        from install import verify_binaries
        x11 = load('packaged_send_x11', 'tests/e2e/x11.py')
        class XErrorEvent(ctypes.Structure):
            _fields_ = [('type', ctypes.c_int), ('display', ctypes.c_void_p),
                        ('resource', ctypes.c_ulong), ('serial', ctypes.c_ulong),
                        ('error_code', ctypes.c_ubyte), ('request_code', ctypes.c_ubyte), ('minor_code', ctypes.c_ubyte)]
        unexpected_x_errors = []
        handler_type = ctypes.CFUNCTYPE(ctypes.c_int, ctypes.c_void_p, ctypes.POINTER(XErrorEvent))
        @handler_type
        def x_error(_display, event):
            # Mapped windows can disappear between QueryTree and attributes.
            # Ignore only that ordinary BadWindow race on this owned display.
            if event.contents.error_code != 3:
                unexpected_x_errors.append(int(event.contents.error_code))
            return 0
        x11.X.XSetErrorHandler.argtypes = [handler_type]
        x11.X.XSetErrorHandler(x_error)
        class TestedDesktop(x11.Desktop):
            def type(self, value):
                # XStringToKeysym requires the named semicolon keysym. Passing
                # the literal ';' silently dropped ':' in the older driver.
                for char in value:
                    shifted = char.isupper() or char in '_:!'
                    symbol = char.lower() if char.isupper() else {
                        '_': 'minus', ':': 'semicolon', '!': '1', ' ': 'space',
                        '/': 'slash', '.': 'period', '-': 'minus'}.get(char, char)
                    code = x11.X.XKeysymToKeycode(self.d, x11.X.XStringToKeysym(symbol.encode()))
                    shift = x11.X.XKeysymToKeycode(self.d, x11.X.XStringToKeysym(b'Shift_L'))
                    if not code:
                        raise RuntimeError('Unsupported synthetic XTest character')
                    if shifted:
                        x11.T.XTestFakeKeyEvent(self.d, shift, 1, 0)
                    x11.T.XTestFakeKeyEvent(self.d, code, 1, 0)
                    x11.T.XTestFakeKeyEvent(self.d, code, 0, 0)
                    if shifted:
                        x11.T.XTestFakeKeyEvent(self.d, shift, 0, 0)
                x11.X.XFlush(self.d)
                time.sleep(.1)
        binary = node_fixture.cached_geth(ROOT)
        launcher = (ROOT / 'cryptowallet').resolve() if (ROOT / 'cryptowallet').exists() else ROOT / 'dist/CryptoWallet-linux-x64/cryptowallet'
        runtime = launcher.parent
        report['artifact'] = {'version': subprocess.check_output([str(launcher), '--version'], text=True, timeout=10).strip(),
            'binaryManifest': verify_binaries(runtime), 'asarSHA256': hashlib.sha256((runtime / 'resources/app.asar').read_bytes()).hexdigest()}
        report['node'] = {'implementation': 'geth1.17.9-stable', 'image': node_fixture.IMAGE,
            'binarySHA256': node_fixture.GETH_SHA256, 'chainId': 11155111,
            'consensusScope': 'Actual EVM execution with test-only Engine-API block production; no public-network/beacon consensus acceptance claim'}
        with contextlib.nullcontext(owned_directory) as temporary:
            folder = pathlib.Path(temporary)
            home, config, runtime_dir = (folder / name for name in ('home', 'config', 'run'))
            for path in (home, config, runtime_dir):
                path.mkdir(mode=0o700)
            # Neither geth nor GUI test tooling inherits user service tokens,
            # wallet configuration, proxy settings or runtime injection flags.
            os.environ.clear()
            os.environ.update(PATH='/usr/bin:/bin', HOME=str(home), USER='wallet-test-fixture',
                              XDG_CONFIG_HOME=str(config), XDG_RUNTIME_DIR=str(runtime_dir),
                              LANG='C.UTF-8', LC_ALL='C.UTF-8', GOTRACEBACK='none')
            node = node_fixture.LocalGeth(ROOT, folder / 'geth', binary)
            node.start()
            passed('Pinned actual geth executes fresh blocks with public genesis-only funds')
            display = next((f':{number}' for number in range(180, 190)
                if not pathlib.Path(f'/tmp/.X11-unix/X{number}').exists() and not pathlib.Path(f'/tmp/.X{number}-lock').exists()), None)
            if display is None:
                raise Blocked('No unused owned X11 test display is available')
            env = {**os.environ, 'DISPLAY': display, 'HOME': str(home), 'XDG_CONFIG_HOME': str(config),
                   'XDG_RUNTIME_DIR': str(runtime_dir), 'GOTRACEBACK': 'none'}
            os.environ['DISPLAY'] = display
            for key in ('WAYLAND_DISPLAY', 'DBUS_SESSION_BUS_ADDRESS', 'XAUTHORITY'):
                env.pop(key, None)
            server = shutil.which('Xvfb') or str(ROOT / '.tools/xvfb/root/usr/bin/Xvfb')
            xvfb_env = dict(env)
            if '.tools/' in server:
                xvfb_env['LD_LIBRARY_PATH'] = str(ROOT / '.tools/xvfb/root/usr/lib/x86_64-linux-gnu')
            xvfb_log = tempfile.TemporaryFile(mode='w+')
            logs.append(xvfb_log)
            xvfb = subprocess.Popen([server, display, '-screen', '0', '1280x1024x24', '-nolisten', 'tcp'],
                                    env=xvfb_env, stdout=xvfb_log, stderr=xvfb_log)
            wait(lambda: pathlib.Path('/tmp/.X11-unix/X' + display[1:]).exists(), 10)
            desktop = TestedDesktop(display.encode())
            gui = Gui(desktop)
            # Validate XTest punctuation on a separate public harness field
            # before entering any app credential. This is never shipped UI.
            try:
                import tkinter
            except ImportError:
                raise Blocked('Public XTest validation requires the test-only tkinter prerequisite') from None
            driver = tkinter.Tk(screenName=display)
            driver.title('Public XTest driver fixture')
            entry = tkinter.Entry(driver)
            entry.pack()
            driver.update()
            entry.focus_force()
            driver.update()
            desktop.type(node.endpoint)
            driver.update()
            typed = entry.get()
            typed_correctly = typed == node.endpoint
            entry.delete(0, tkinter.END)
            entry.focus_force()
            driver.update()
            desktop.type(AMOUNT_DECIMAL)
            driver.update()
            typed_correctly = typed_correctly and entry.get() == AMOUNT_DECIMAL
            driver.destroy()
            if not typed_correctly:
                raise RuntimeError('XTest public URL punctuation typing differs')
            app_log = tempfile.TemporaryFile(mode='w+')
            logs.append(app_log)
            app = subprocess.Popen([str(launcher), '--profile=testnet', '--ozone-platform=x11'], env=env,
                                   stdout=app_log, stderr=app_log)
            gui.prompt('CryptoWallet')
            time.sleep(1)
            gui.click_button('Restore from recovery phrase')
            gui.prompt('Create local wallet')
            desktop.inputs('Create local wallet', [PASSWORD, PASSWORD, PHRASE, ''])
            desktop.accept('Create local wallet')
            gui.prompt('Imported account recovery limits')
            desktop.accept('Imported account recovery limits')
            wait(lambda: (config / 'localwallet/testnet/vault.lwv').is_file())
            passed('Fresh explicit testnet profile restores the public BIP39 fixture through masked Electron input')
            gui.click('Ethereum Sepolia')
            gui.click('Configure trusted RPC')
            gui.prompt('Configure trusted RPC for ethereum-sepolia')
            desktop.inputs('Configure trusted RPC for ethereum-sepolia', [node.endpoint, '', '', ''])
            desktop.accept('Configure trusted RPC for ethereum-sepolia')
            gui.prompt('Approve RPC privacy and trust')
            def rpc_approval_visible():
                compact = re.sub('[^a-z0-9]', '', gui.text(public_prompt='Approve RPC privacy and trust').lower())
                return 'ethereumsepolia' in compact and '12700118547' in compact
            wait(rpc_approval_visible, 10)
            desktop.accept('Approve RPC privacy and trust')
            wait(lambda: '1000 eth' in gui.text().lower())
            passed('Electron configures and approves the actual loopback RPC; funded native balance is visible')
            recipient_before = int(node.rpc('eth_getBalance', [node_fixture.RECIPIENT, 'latest']), 16)
            source_before = int(node.rpc('eth_getBalance', [node_fixture.SOURCE, 'latest']), 16)
            nonce_before = int(node.rpc('eth_getTransactionCount', [node_fixture.SOURCE, 'pending']), 16)
            if recipient_before != 0 or source_before != node_fixture.INITIAL_ATOMIC or nonce_before != 0:
                raise RuntimeError('Fresh local funding or nonce precondition differs')
            first_block = int(node.rpc('eth_blockNumber', []), 16)
            gui.click('Send')
            gui.click('Full recipient')
            desktop.type(node_fixture.RECIPIENT)
            desktop.key('Tab')
            desktop.type(AMOUNT_DECIMAL)
            gui.click('Prepare', last=True)
            # The public preview is actual Go-prepared plan data. No internal API
            # or DOM instrumentation is used to submit or approve the operation.
            desktop.focus(desktop.wait('CryptoWallet')[0])
            desktop.key('Home', ctrl=True)
            wait(lambda: 'request completed' in gui.text().lower())
            passed('Ordinary Electron form prepares the exact decimal payment through the real Go/node pipeline')
            gui.click('Review')
            gui.prompt('Authorize exact transfer')
            def payment_approval_visible():
                text = gui.text(public_prompt='Authorize exact transfer').lower()
                compact = re.sub('[^a-z0-9]', '', text)
                return 'ethereumsepolia' in compact and 'atomicamount' in compact and str(AMOUNT) in compact
            wait(payment_approval_visible, 10)
            report['approval'] = {'networkVisible': True, 'atomicAmountVisible': True, 'atomicAmount': str(AMOUNT)}
            desktop.accept('Authorize exact transfer')
            gui.prompt('Reauthenticate this transfer')
            desktop.inputs('Reauthenticate this transfer', [PASSWORD])
            desktop.accept('Reauthenticate this transfer')
            passed('Electron Send prepares, presents the exact Go-authored plan and reauthenticates in its masked dialog')
            wait(lambda: int(node.rpc('eth_getBalance', [node_fixture.RECIPIENT, 'latest']), 16) == AMOUNT, 30)
            latest = int(node.rpc('eth_blockNumber', []), 16)
            transactions = []
            for height in range(first_block, latest + 1):
                block = node.rpc('eth_getBlockByNumber', [hex(height), True])
                transactions.extend(tx for tx in block['transactions'] if tx['from'].lower() == node_fixture.SOURCE.lower())
            if len(transactions) != 1:
                raise RuntimeError('Actual geth observed an unexpected number of source transactions')
            transaction = transactions[0]
            if transaction.get('to', '').lower() != node_fixture.RECIPIENT.lower() or int(transaction['value'], 16) != AMOUNT or transaction['input'] != '0x' or int(transaction['nonce'], 16) != 0 or int(transaction['chainId'], 16) != 11155111:
                raise RuntimeError('Independently queried actual transaction differs from the approved transfer')
            receipt = node.rpc('eth_getTransactionReceipt', [transaction['hash']])
            if receipt is None or receipt.get('status') != '0x1' or receipt.get('transactionHash') != transaction['hash']:
                raise RuntimeError('Actual geth execution receipt did not establish success')
            gas_used, gas_price = int(receipt['gasUsed'], 16), int(receipt['effectiveGasPrice'], 16)
            source_after = int(node.rpc('eth_getBalance', [node_fixture.SOURCE, 'latest']), 16)
            if source_before - source_after != AMOUNT + gas_used * gas_price:
                raise RuntimeError('Independent source debit does not equal value plus actual execution fee')
            wait(lambda: int(node.rpc('eth_blockNumber', []), 16) >= latest + 3, 10)
            if int(node.rpc('eth_getTransactionCount', [node_fixture.SOURCE, 'pending']), 16) != 1 or int(node.rpc('eth_getBalance', [node_fixture.RECIPIENT, 'latest']), 16) - recipient_before != AMOUNT:
                raise RuntimeError('Delayed observation detected a duplicate or missing payment')
            report['transfer'] = {'identity': transaction['hash'], 'source': node_fixture.SOURCE, 'recipient': node_fixture.RECIPIENT,
                'chainId': 11155111, 'atomicAmount': str(AMOUNT), 'nativeRecipientBefore': str(recipient_before),
                'nativeRecipientAfter': str(AMOUNT), 'sourceBefore': str(source_before), 'sourceAfter': str(source_after),
                'actualFee': str(gas_used * gas_price), 'receipt': receipt, 'sourcePendingNonceAfter': 1}
            passed('Independent actual geth receipt, recipient delta, source fee debit and delayed nonce prove exactly one executed payment')
            stop_process(app)
            app = None
            needles = []
            for value in (PASSWORD.encode(), PHRASE.encode()):
                needles.extend((value, value.hex().encode(), base64.b64encode(value)))
            for path in home.rglob('*'):
                if path.is_file() and path.stat().st_size < 32 * 1024 * 1024 and any(needle in path.read_bytes() for needle in needles):
                    raise RuntimeError('Synthetic credential canary was persisted in the disposable home')
            for path in config.rglob('*'):
                if path.is_file() and path.stat().st_size < 32 * 1024 * 1024 and any(needle in path.read_bytes() for needle in needles):
                    raise RuntimeError('Synthetic credential canary was persisted in the disposable profile')
            for log in logs:
                log.flush()
                log.seek(0)
                data = log.read().encode()
                if any(needle in data for needle in needles):
                    raise RuntimeError('Synthetic credential canary appeared in captured process logs')
            passed('Disposable app files and captured logs contain no tested password/phrase canaries')
            if unexpected_x_errors:
                raise RuntimeError('Owned X11 driver reported an unexpected protocol error')
        if snapshot() != report['sourceSHA256']:
            raise RuntimeError('Packaged Send test sources changed during execution')
        report['status'] = 'PASS'
    except Blocked as error:
        report.update(status='BLOCKED', reason=str(error))
    except RuntimeError as error:
        report.update(status='FAIL', reason=str(error))
    except Exception:
        report.update(status='FAIL', reason='Packaged Send prerequisite or automation failed; no secret diagnostic retained')
    finally:
        stop_process(app)
        if node is not None:
            node.close()
        stop_process(xvfb)
        for log in logs:
            log.close()
        report['seconds'] = round(time.monotonic() - start, 3)
        save(report)
    print(json.dumps(report, indent=2))
    return 0 if report['status'] == 'PASS' else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--isolated-network', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--parent-network-namespace', help=argparse.SUPPRESS)
    parser.add_argument('--owned-directory', type=pathlib.Path, help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.isolated_network:
        if not args.parent_network_namespace or not re.fullmatch(r'net:\[[0-9]+\]', args.parent_network_namespace) or not args.owned_directory or not args.owned_directory.is_dir():
            raise SystemExit('Missing isolation proof')
        return inside(args.parent_network_namespace, args.owned_directory)
    report = {'status': 'BLOCKED', 'scope': 'Actual private geth/Electron packaged Send only', 'sourceSHA256': snapshot()}
    try:
        if os.getuid() == 0:
            raise Blocked('Packaged Send must run as an ordinary user')
        node_fixture.cached_geth(ROOT)  # Cached image only; never pulls a node.
        save({**report, 'status': 'RUNNING'})
        with tempfile.TemporaryDirectory(prefix='cryptowallet-packaged-send-') as temporary:
            process = subprocess.Popen(['unshare', '--user', '--map-current-user', '--net', '--keep-caps', sys.executable,
                str(pathlib.Path(__file__).resolve()), '--isolated-network', '--parent-network-namespace', os.readlink('/proc/self/ns/net'),
                '--owned-directory', temporary], cwd=ROOT, start_new_session=True)
            try:
                code = process.wait(timeout=420)
            finally:
                # Every node/app/Xvfb process inherits this owned group. Even a
                # fatal C-library error cannot leave test processes running.
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                    time.sleep(.25)
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                if process.poll() is None:
                    process.wait(timeout=5)
            latest = json.loads((REPORTS / 'packaged-send.json').read_text())
            if latest.get('status') == 'RUNNING':
                latest.update(status='FAIL', reason='Owned packaged Send process ended without completing evidence')
                save(latest)
            return code
    except Exception:
        if 'process' in locals():
            report = json.loads((REPORTS / 'packaged-send.json').read_text())
            report['status'] = 'FAIL'
            report['reason'] = 'Owned packaged Send execution or bounded deadline failed; no network fallback attempted'
        else:
            report['reason'] = 'Pinned cached node or ordinary-user namespace prerequisite unavailable; no network fallback attempted'
        save(report)
        print(json.dumps(report, indent=2))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
