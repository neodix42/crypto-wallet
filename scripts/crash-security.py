#!/usr/bin/env python3
"""Owned-process crash/canary drill using real Go binaries and disposable roots.

The client emulates only the typed Electron prompt channel. No GUI automation,
live network request, Decred operation, public testnet, or real user profile is
used. No private prompt bytes or secret diagnostics are written to reports.
"""
import base64
import fcntl
import hashlib
import hmac
import json
import os
import pathlib
import queue
import re
import secrets
import shutil
import signal
import struct
import subprocess
import tempfile
import threading
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORTS = ROOT / 'reports'


class DrillFailure(Exception):
    pass


def require(value, reason):
    if not value:
        raise DrillFailure(reason)


def read_exact(stream, size):
    result = bytearray()
    while len(result) < size:
        part = stream.read(size - len(result))
        if not part:
            raise EOFError()
        result.extend(part)
    return bytes(result)


class Client:
    def __init__(self, install, home, logs, public, canaries):
        self.home = home
        self.install = install
        self.home.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.log = tempfile.TemporaryFile()
        logs.append(self.log)
        env = {key: value for key, value in os.environ.items()
               if key in ('PATH', 'LANG', 'LC_ALL')}
        env.update(HOME=str(home), XDG_CONFIG_HOME=str(home / 'config'),
                   TMPDIR=str(home / 'tmp'), GOTRACEBACK='none')
        (home / 'tmp').mkdir(mode=0o700, exist_ok=True)
        self.process = subprocess.Popen([str(install / 'wallet-core'), '--environment=mainnet'],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.log,
            env=env, cwd=home, start_new_session=True)
        self.events = queue.Queue()
        self.public = public
        self.canaries = canaries
        self.session = secrets.token_hex(16)
        self.counter = 0
        self.request = None
        self.prompt = None
        self.signers = set()
        self.stopped = False
        self.reader = threading.Thread(target=self.pump, daemon=True)
        self.reader.start()

    def pump(self):
        try:
            while True:
                size = struct.unpack('>I', read_exact(self.process.stdout, 4))[0]
                require(0 < size <= 1 << 20, 'Response exceeded IPC bounds')
                raw = read_exact(self.process.stdout, size)
                message = json.loads(raw)
                if message.get('method') != 'prompt':
                    self.public.append(raw)
                self.events.put(message)
        except Exception:
            self.events.put(None)

    def write(self, message):
        raw = json.dumps(message, separators=(',', ':')).encode()
        require(len(raw) <= 1 << 20, 'Test request exceeded IPC bounds')
        self.process.stdin.write(struct.pack('>I', len(raw)) + raw)
        self.process.stdin.flush()

    def begin(self, method, body=None):
        self.counter += 1
        self.request = dict(version=1, id=str(self.counter), session=self.session,
                            method=method, body=body or {})
        self.write(self.request)

    def event(self):
        while True:
            message = self.events.get(timeout=60)
            require(message is not None, 'Unexpected process EOF during workflow')
            require(all(message.get(k) == self.request[k] for k in ('version', 'id', 'session')),
                    'Reply escaped its original request/session')
            if message['method'] == 'activity':
                require(message['body'] == {}, 'Activity contained payload')
                continue
            if message['method'] == 'prompt':
                self.prompt = message['body']
                require(re.fullmatch('[0-9a-f]{32}', self.prompt.get('id', '')) is not None,
                        'Invalid prompt capability')
                self.inspect_processes()
            return message

    def expect_prompt(self, kind):
        event = self.event()
        require(event['method'] == 'prompt' and self.prompt.get('kind') == kind,
                'Unexpected workflow prompt')
        return self.prompt

    def reply(self, values=None, accepted=True):
        require(self.prompt is not None, 'No live prompt')
        body = dict(promptId=self.prompt['id'], accepted=accepted)
        if values is not None:
            body['values'] = values
        self.write({**self.request, 'method': 'promptReply', 'body': body})
        self.prompt = None

    def finish(self):
        while True:
            event = self.event()
            if event['method'] == 'prompt':
                require(self.prompt['kind'] in ('notice', 'confirm'), 'Unanswered input prompt')
                self.reply()
                continue
            require(event['method'] == self.request['method'], 'Unexpected response method')
            return event

    def call(self, method, body=None):
        self.begin(method, body)
        event = self.finish()
        require(not event.get('error'), 'Operation failed unexpectedly: ' + method)
        return event['body']

    def discover_signers(self):
        descendants = set()
        for children in pathlib.Path('/proc', str(self.process.pid), 'task').glob('*/children'):
            try:
                descendants.update(int(p) for p in children.read_text().split())
            except FileNotFoundError:
                pass

        for pid in descendants:
            path = pathlib.Path('/proc', str(pid))
            try:
                if b'wallet-signer' not in (path / 'cmdline').read_bytes():
                    continue
                self.signers.add(pid)
            except FileNotFoundError:
                pass

    def inspect_processes(self):
        self.discover_signers()
        require(self.signers, 'No actual signer child observed')
        for pid in (self.process.pid, *self.signers):
            path = pathlib.Path('/proc', str(pid))
            require(path.exists(), 'Secret process disappeared before probe')
            for name in ('cmdline', 'environ'):
                try:
                    raw = (path / name).read_bytes()
                except PermissionError:
                    # Non-dumpability deliberately denies the parent access to
                    # /proc/<pid>/environ. Do not weaken it for test inspection.
                    require(name == 'environ', 'Unexpected proc inspection denial')
                    continue
                require(not contains_secret(raw, self.canaries), 'Secret reached argv/environment')
            limits = (path / 'limits').read_text()
            require(re.search(r'Max core file size\s+0\s+0\s+bytes', limits),
                    'Secret process core limits are not zero')

    def stop(self, sig=signal.SIGTERM):
        if self.stopped:
            return
        if self.process.poll() is None:
            self.discover_signers()
            os.kill(self.process.pid, sig)
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait(timeout=10)
                raise DrillFailure('Application did not stop within deadline')
        self.reader.join(timeout=5)
        for pid in self.signers:
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                # A zombie thread-group leader can coexist briefly with live
                # workers during SIGKILL teardown. Those workers still hold the
                # vault lock. Wait for the entire owned group before reopening.
                live = False
                for task in pathlib.Path('/proc', str(pid), 'task').glob('*/stat'):
                    try:
                        live |= task.read_text().split(') ', 1)[1][0] != 'Z'
                    except FileNotFoundError:
                        pass
                if not live:
                    break
                time.sleep(.05)
            else:
                raise DrillFailure('Signer remained live after parent death')
        lock = self.home / 'config/localwallet/mainnet/writer.lock'
        if lock.exists():
            deadline = time.monotonic() + 5
            while True:
                descriptor = os.open(lock, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
                try:
                    try:
                        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        fcntl.flock(descriptor, fcntl.LOCK_UN)
                        break
                    except BlockingIOError:
                        require(time.monotonic() < deadline, 'Writer lock remained held after process teardown')
                finally:
                    os.close(descriptor)
                time.sleep(.05)
        self.stopped = True


def contains_secret(raw, canaries):
    for secret in canaries:
        if any(value in raw for value in (secret, secret.hex().encode(), base64.b64encode(secret))):
            return True
    return False


def add_root_canaries(phrase, canaries, wordlist):
    words = phrase.decode('ascii').split()
    require(len(words) == 12, 'Generated word count changed')
    indexes = {word: i for i, word in enumerate(wordlist)}
    bits = ''.join(format(indexes[word], '011b') for word in words)
    entropy = int(bits[:128], 2).to_bytes(16, 'big')
    require(format(hashlib.sha256(entropy).digest()[0], '08b')[:4] == bits[128:],
            'Generated phrase checksum invalid')
    seed = hashlib.pbkdf2_hmac('sha512', phrase, b'mnemonic', 2048)
    canaries.extend([phrase, entropy, seed])
    canaries.extend(seed[i:i+16] for i in (0, 16, 32, 48))
    # Include meaningful phrase fragments without matching isolated common words.
    canaries.extend(' '.join(words[i:i+4]).encode() for i in (0, 4, 8))
    # Independent test-only SLIP-0010 hardened derivation of Ed25519 private seeds.
    # Production derivation/signing remains in Go; none of these values are output.
    for path in ((44, 397, 0), (44, 637, 0, 0, 0), (44, 607, 0)):
        material = hmac.digest(b'ed25519 seed', seed, 'sha512')
        for index in path:
            material = hmac.digest(material[32:], b'\0' + material[:32] +
                                   (index | 1 << 31).to_bytes(4, 'big'), 'sha512')
        canaries.append(material[:32])
        canaries.extend((material[:16], material[16:32]))


def main():
    REPORTS.mkdir(exist_ok=True)
    harness_hash = hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
    report = dict(status='RUNNING', scope=__doc__, checks=[],
        sourceSHA256={'scripts/crash-security.py': harness_hash},
        notRun=['Electron/Chromium crash collection and browser storage/clipboard/QR/network capture',
                'Live send-boundary crash acceptance', 'Physical OS lock/suspend/Wayland',
                'Independent review', 'Decred-specific work and public testnet tests excluded by owner'])
    (REPORTS / 'crash-security.json').write_text(json.dumps(report, indent=2) + '\n')
    clients, logs, public, canaries = [], [], [], []
    def passed(name):
        report['checks'].append(dict(name=name, status='PASS'))
        print('PASS ' + name, flush=True)
    try:
        module_cache = subprocess.check_output(['go', 'env', 'GOMODCACHE'], cwd=ROOT, text=True).strip()
        source = (pathlib.Path(module_cache) / 'github.com/tyler-smith/go-bip39@v1.1.0/wordlists/english.go').read_text()
        words = source.split('var english = `', 1)[1].split('`', 1)[0].strip().splitlines()
        require(len(words) == 2048 and len(set(words)) == 2048, 'Pinned wordlist unavailable')
        manifest = json.loads((ROOT / 'bin/manifest.json').read_text())
        for component, digest in manifest.items():
            require(hashlib.sha256((ROOT / 'bin' / component).read_bytes()).hexdigest() == digest,
                    'Build manifest differs from binaries')
        report['artifact'] = dict(binaryManifest=manifest)
        with tempfile.TemporaryDirectory(prefix='localwallet-crash-security-') as temporary:
            folder = pathlib.Path(temporary)
            install = folder / 'app'
            install.mkdir()
            for name in (*manifest, 'manifest.json'):
                shutil.copy2(ROOT / 'bin' / name, install / name)
            def client(home):
                c = Client(install, home, logs, public, canaries)
                clients.append(c)
                return c
            password = 'synthetic-' + secrets.token_hex(24)
            canaries.append(password.encode())
            for stage in ('password', 'recording', 'challenge'):
                report['lastCase'] = stage + ': initial'
                home = folder / stage
                c = client(home)
                require(c.call('accounts')['vaultState'] == 'missing', 'Profile was not fresh')
                c.begin('create', dict(words=12))
                c.expect_prompt('input')
                phrase = None
                if stage != 'password':
                    c.reply([password, password])
                    prompt = c.expect_prompt('recovery')
                    phrase = prompt['phrase'].encode()
                    add_root_canaries(phrase, canaries, words)
                    require((home / 'config/localwallet/mainnet/vault.lwv').is_file(),
                            'Recovery display preceded durable encrypted storage')
                    if stage == 'challenge':
                        c.reply()
                        c.expect_prompt('input')
                c.stop(signal.SIGKILL)
                report['lastCase'] = stage + ': reopen'
                reopened = client(home)
                snapshot = reopened.call('accounts')
                require(snapshot['locked'], 'Restart automatically unlocked')
                if phrase is None:
                    require(snapshot['vaultState'] == 'missing', 'Unentered password created a root')
                else:
                    require(snapshot['walletExists'] and not snapshot['recoveryConfirmed'],
                            'Crash lost pending state or fabricated confirmation')
                    reopened.begin('unlock')
                    reopened.expect_prompt('input')
                    reopened.reply([password])
                    require(not reopened.finish().get('error'), 'Pending root did not unlock')
                    reopened.begin('confirmRecovery')
                    reopened.expect_prompt('input')
                    reopened.reply([password])
                    require(reopened.expect_prompt('recovery')['phrase'].encode() == phrase,
                            'Restart replaced the pending root')
                    reopened.reply()
                    reopened.expect_prompt('input')
                    recorded = phrase.decode().split()
                    reopened.reply([recorded[1], recorded[5], recorded[-1]])
                    require(reopened.finish()['body']['recoveryConfirmed'],
                            'Correct words failed after crash recovery')
                    reopened.inspect_processes()
                    reopened.stop(signal.SIGABRT)
                    third = client(home)
                    require(third.call('accounts')['locked'], 'Aborted process restarted unlocked')
                    third.begin('unlock')
                    third.expect_prompt('input')
                    third.reply([password])
                    require(third.finish()['body']['recoveryConfirmed'],
                            'Abort lost durable confirmation')
                    third.stop()
                reopened.stop()
                passed('Actual core SIGKILL at ' + stage + '; restart preserves root/confirmation boundaries')
            for abrupt in (signal.SIGKILL, signal.SIGABRT):
                home = folder / ('signer-' + str(abrupt))
                c = client(home)
                c.begin('create', dict(words=12))
                c.expect_prompt('input')
                c.reply([password, password])
                phrase = c.expect_prompt('recovery')['phrase'].encode()
                add_root_canaries(phrase, canaries, words)
                for pid in c.signers:
                    os.kill(pid, abrupt)
                event = c.finish()
                require(event.get('error') in ('REQUEST_FAILED_STATE_UNKNOWN', 'REQUEST_CANCELLED'),
                        'Signer crash lacked sanitized failure')
                c.stop()
                reopened = client(home)
                snapshot = reopened.call('accounts')
                require(snapshot['locked'] and snapshot['walletExists'] and not snapshot['recoveryConfirmed'],
                        'Signer crash fabricated readiness or lost the wallet')
                reopened.stop()
                passed('Actual signer ' + signal.Signals(abrupt).name + ' revokes the pending prompt and preserves encrypted root')
            home = folder / 'cancel'
            c = client(home)
            c.begin('create', dict(words=12))
            c.expect_prompt('input')
            c.reply([password, password])
            phrase = c.expect_prompt('recovery')['phrase'].encode()
            add_root_canaries(phrase, canaries, words)
            c.reply(accepted=False)
            require(not c.finish()['body']['recoveryConfirmed'], 'Cancellation confirmed recovery')
            c.begin('confirmRecovery')
            c.expect_prompt('input')
            c.reply(['incorrect-synthetic-password'])
            require(c.finish().get('error'), 'Incorrect reauthentication accepted')
            c.stop()
            passed('Cancellation and failed reauthentication preserve pending state without secret error details')
            for data in public:
                require(not contains_secret(data, canaries), 'Secret crossed ordinary public IPC')
            for log in logs:
                log.seek(0)
                require(not contains_secret(log.read(), canaries), 'Secret reached process/crash logs')
            scanned = 0
            for home in folder.iterdir():
                if home == install:
                    continue
                for path in home.rglob('*'):
                    if path.is_file():
                        require(path.stat().st_size <= 32 << 20, 'Unbounded crash/persistence artifact')
                        require(not path.name.startswith('core.'), 'Core artifact unexpectedly created')
                        require(not contains_secret(path.read_bytes(), canaries),
                                'Secret reached profile/temp/crash files')
                        scanned += 1
            passed('Password/phrase/fragments/entropy/seed/Ed25519 canaries absent from tested public IPC, logs, argv/env and persisted crash/temp files')
            report.update(status='PASS', processes=len(clients), persistedFilesScanned=scanned,
                          publicFramesScanned=len(public),
                          promptException='Only exact active signer-authored prompt and its one-use input reply')
    except Exception as error:
        # Only our fixed messages are safe; parser/runtime exceptions can embed inputs.
        report.update(status='FAIL', errorClass=type(error).__name__, reason=str(error) if isinstance(error, DrillFailure) else
                      'Owned-process crash/canary drill failed; no secret diagnostic retained')
    finally:
        for c in clients:
            try:
                c.stop()
            except Exception:
                report.update(status='FAIL', reason='Process teardown failed')
        for log in logs:
            log.close()
        if hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest() != harness_hash:
            report.update(status='FAIL', reason='Crash drill source changed during execution; rerun required')
        (REPORTS / 'crash-security.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return report['status'] != 'PASS'


if __name__ == '__main__':
    raise SystemExit(main())
