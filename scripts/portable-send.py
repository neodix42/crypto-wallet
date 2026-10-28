#!/usr/bin/env python3
"""Execute a real portable Go payment on a private, genesis-funded geth chain.

This is headless protocol integration, not graphical UI acceptance. The actual
wallet and pinned geth run as an ordinary UID in a fresh loopback-only network
namespace. Only public synthetic recovery fixtures and local units are used.
"""
import argparse
import base64
import hashlib
import importlib.util
import json
import os
import pathlib
import queue
import re
import secrets
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / 'reports/portable-send.json'
PASSWORD = 'PUBLIC-portable-send-password-874193'
PHRASE = 'abandon ' * 11 + 'about'
DECIMAL = '0.012345678912345678'
ATOMIC = 12345678912345678
NETWORK = 'ethereum-sepolia'


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


node_fixture = load('portable_send_actual_geth', 'tests/e2e/local_geth.py')
binding = load('portable_send_binding', 'scripts/evidence_binding.py')


class Blocked(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sources():
    names = ['scripts/portable-send.py', 'scripts/evidence_binding.py',
             'tests/e2e/local_geth.py', 'tests/oracles/evm/geth-genesis.json']
    return {**binding.go_sources(ROOT), **binding.hash_names(ROOT, names)}


def save(report):
    REPORT.parent.mkdir(exist_ok=True)
    temporary = REPORT.with_suffix('.tmp')
    temporary.write_text(json.dumps(report, indent=2) + '\n')
    temporary.replace(REPORT)


def wait(predicate, seconds=30):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = predicate()
        if result:
            return result
        time.sleep(.15)
    raise RuntimeError('Owned local payment observation exceeded its deadline')


class Client:
    def __init__(self, executable, environment):
        self.log = tempfile.TemporaryFile()
        self.process = subprocess.Popen([str(executable), '--internal-core', '--environment=testnet'],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.log, env=environment)
        self.session = secrets.token_hex(16)
        self.counter = 0
        self.responses = queue.Queue()
        self.public_frames = []
        self.prompt_counts = {}
        threading.Thread(target=self.read, daemon=True).start()

    def read(self):
        try:
            while True:
                prefix = self.process.stdout.read(4)
                require(len(prefix) == 4, 'Wallet private IPC ended before its response')
                size = struct.unpack('>I', prefix)[0]
                require(0 < size <= 1 << 20, 'Wallet private frame exceeded its bound')
                raw = self.process.stdout.read(size)
                require(len(raw) == size, 'Wallet private response was truncated')
                message = json.loads(raw)
                if message.get('method') != 'prompt':
                    self.public_frames.append(raw)
                self.responses.put(message)
        except Exception:
            self.responses.put(None)

    def write(self, value):
        raw = json.dumps(value).encode()
        self.process.stdin.write(struct.pack('>I', len(raw)) + raw)
        self.process.stdin.flush()

    def call(self, method, body=None, plan=None):
        self.counter += 1
        request = dict(version=1, id=str(self.counter), session=self.session, method=method, body=body or {})
        self.write(request)
        approved = False
        for _ in range(65):
            message = self.responses.get(timeout=60)
            require(message is not None and all(message.get(key) == request[key] for key in ('version', 'id', 'session')),
                    'Wallet private response lost its request/session binding')
            if message['method'] == 'activity':
                require(message['body'] == {}, 'Wallet activity contained unexpected data')
                continue
            if message['method'] != 'prompt':
                require(message['method'] == method and not message.get('error'), 'Wallet rejected the local payment operation')
                return message['body']
            prompt = message['body']
            require(isinstance(prompt, dict) and re.fullmatch('[a-f0-9]{32}', prompt.get('id', '')),
                    'Wallet prompt identity was malformed')
            kind, title = prompt.get('kind'), prompt.get('title')
            self.prompt_counts[title] = self.prompt_counts.get(title, 0) + 1
            reply = dict(promptId=prompt['id'], accepted=True)
            if kind == 'input':
                if method == 'import' and title == 'Create local wallet — set app password':
                    reply['values'] = [PASSWORD, PASSWORD, PHRASE, '']
                elif method == 'configure' and title == 'Configure trusted RPC for ' + NETWORK:
                    reply['values'] = [node_fixture.LocalGeth.endpoint, '', '', '']
                elif method == 'send' and title == 'Reauthenticate this transfer' and approved:
                    reply['values'] = [PASSWORD]
                elif method == 'unlock' and title == 'Unlock local wallet':
                    reply['values'] = [PASSWORD]
                else:
                    raise RuntimeError('Unexpected private input during the bounded local payment')
                require(len(reply['values']) == len(prompt['labels']), 'Private input count did not match its prompt')
            elif kind == 'confirm':
                if method == 'import' and title == 'Imported account recovery limits':
                    pass
                elif method == 'configure' and title == 'Approve RPC privacy and trust':
                    require('Network: ' + NETWORK in prompt['summary'] and
                            'Endpoint: ' + node_fixture.LocalGeth.endpoint in prompt['summary'],
                            'Trusted-node approval did not bind the exact loopback network')
                elif method == 'send' and title == 'Authorize exact transfer' and plan is not None:
                    expected = ['Network: ' + NETWORK, 'Source: ' + node_fixture.SOURCE,
                                'Destination: ' + node_fixture.RECIPIENT, 'Asset identity: ' + plan['asset'],
                                'Atomic amount: ' + str(ATOMIC), 'Maximum network fee: ' + plan['maxFee'],
                                'Nonce/sequence: 0']
                    require(all(value in prompt['summary'] for value in expected),
                            'Go-authored approval did not bind every intended spend effect')
                    approved = True
                else:
                    raise RuntimeError('Unexpected confirmation during the bounded local payment')
            else:
                raise RuntimeError('Unexpected private prompt kind during the local payment')
            self.write({**request, 'method': 'promptReply', 'body': reply})
        raise RuntimeError('Wallet exceeded the bounded private prompt count')

    def close(self):
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)


def inside(parent_namespace, directory):
    started = time.monotonic()
    report = dict(status='RUNNING', scope=__doc__, sourceSHA256=sources(), steps=[],
                  publicNetworkTests='NOT_RUN', mainnetSpending='NOT_APPLICABLE: no outside network route',
                  notRun=['Graphical UI interaction', 'Clean Ubuntu X11/Wayland OS matrix',
                          'Public funded testnets', 'Public-network consensus acceptance'])
    save(report)
    client = node = None
    clients = []

    def passed(name):
        report['steps'].append(dict(name=name, status='PASS'))
        save(report)
        print('PASS ' + name, flush=True)

    try:
        require(os.getuid() != 0 and os.readlink('/proc/self/ns/net') != parent_namespace,
                'Owned test did not enter an ordinary-user private network namespace')
        subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True, capture_output=True, timeout=5)
        require({name for _, name in socket.if_nameindex()} == {'lo'}, 'Network namespace contains an outside interface')
        routes = []
        for family in ('-4', '-6'):
            routes.extend(json.loads(subprocess.check_output(['ip', '-json', family, 'route', 'show', 'table', 'all'], timeout=5)))
        require(all(route.get('dev') == 'lo' and route.get('dst') != 'default' for route in routes),
                'Network namespace contains an outside route')
        report['networkIsolation'] = dict(uid=os.getuid(), interfaces=['lo'], externalRoutes=False,
                                          networkNamespace=os.readlink('/proc/self/ns/net'))
        binary = ROOT / 'bin/cryptowallet'
        metadata = json.loads((ROOT / 'bin/portable-build-manifest.json').read_text())
        binary_hash = hashlib.sha256(binary.read_bytes()).hexdigest()
        require(metadata.get('kind') == 'portable-go-wallet' and metadata.get('sandbox') == 'none' and
                metadata.get('sha256') == binary_hash, 'Portable build metadata and executable differ')
        directory = pathlib.Path(directory)
        app_directory = directory / 'app'
        app_directory.mkdir(mode=0o700)
        executable = app_directory / 'cryptowallet'
        shutil.copy2(binary, executable)
        require(hashlib.sha256(executable.read_bytes()).hexdigest() == binary_hash, 'Pinned portable application copy differs')
        version = subprocess.check_output([str(executable), '--version'], text=True, timeout=10).strip()
        require(version == metadata['version'], 'Portable application version differs from its build identity')
        report['artifact'] = dict(version=version, binaryManifest={'cryptowallet': binary_hash}, sandbox='none')
        home, config, runtime = (directory / name for name in ('home', 'config', 'run'))
        for path in (home, config, runtime):
            path.mkdir(mode=0o700)
        environment = dict(PATH='/usr/bin:/bin', HOME=str(home), USER='public-wallet-fixture',
                           XDG_CONFIG_HOME=str(config), XDG_RUNTIME_DIR=str(runtime),
                           LANG='C.UTF-8', LC_ALL='C.UTF-8', GOTRACEBACK='none')
        os.environ.clear()
        os.environ.update(environment)
        geth = node_fixture.cached_geth(ROOT)
        node = node_fixture.LocalGeth(ROOT, directory / 'geth', geth)
        node.start()
        report['node'] = dict(image=node_fixture.IMAGE, binarySHA256=node_fixture.GETH_SHA256,
                              chainId=11155111, consensusScope='Actual geth execution with private Engine-API block production')
        passed('Pinned actual geth executes real blocks with public genesis-only funding in loopback isolation')
        client = Client(executable, environment)
        clients.append(client)
        initial = client.call('accounts')
        require(initial['vaultState'] == 'missing' and initial['environment'] == 'testnet', 'Disposable app inherited a wallet or wrong profile')
        restored = client.call('import', {'words': 12})
        require(restored['recoveryConfirmed'] and not restored['locked'], 'Public phrase import did not complete recovery')
        accounts = restored['accounts']
        ethereum = next(account for account in accounts if account['network'] == NETWORK and account['index'] == 0)
        require(ethereum['address'] == node_fixture.SOURCE and (config / 'cryptowallet/testnet/vault.lwv').is_file(),
                'Restored public fixture identity or encrypted profile differs')
        passed('Fresh single-file Go installation restores the public fixture through private typed prompts')
        report['phase'] = 'configure-loopback'
        client.call('configure', {'network': NETWORK})
        report['phase'] = 'read-public-network-registry'
        registry = client.call('networks')
        network = next(item for item in registry if item['id'] == NETWORK)
        asset = next(item for item in network['assets'] if item['symbol'] == 'ETH' and not item.get('contract'))
        report['phase'] = 'query-actual-balance'
        balance = client.call('balances', {'accountId': ethereum['id']})
        require(any(item['asset'] == asset['id'] and item['atomic'] == str(node_fixture.INITIAL_ATOMIC) for item in balance),
                'Actual configured node balance did not reach the wallet')
        passed('Signer approves the exact trusted loopback RPC and the wallet observes real native funding')
        recipient_before = int(node.rpc('eth_getBalance', [node_fixture.RECIPIENT, 'latest']), 16)
        source_before = int(node.rpc('eth_getBalance', [node_fixture.SOURCE, 'latest']), 16)
        require(recipient_before == 0 and source_before == node_fixture.INITIAL_ATOMIC and
                int(node.rpc('eth_getTransactionCount', [node_fixture.SOURCE, 'pending']), 16) == 0,
                'Real node funding/nonce preconditions differ')
        first_block = int(node.rpc('eth_blockNumber', []), 16)
        plan = client.call('prepare', dict(accountId=ethereum['id'], asset=asset['id'],
                                         recipient=node_fixture.RECIPIENT, amount=DECIMAL, memo=''))
        require(plan['amount'] == str(ATOMIC) and plan['recipient'] == node_fixture.RECIPIENT and
                plan['account']['id'] == ethereum['id'] and plan['nonce'] == '0' and int(plan['maxFee']) > 0,
                'Prepared real transaction lost exact whole-asset amount or spend identity')
        passed('Real node-backed preparation converts exact whole-asset decimal units and reserves nonce zero')
        sent = client.call('send', {'id': plan['id']}, plan=plan)
        require(sent['state'] == 'SUBMITTED' and re.fullmatch('0x[0-9a-f]{64}', sent['identity']),
                'Signed real transaction was not accepted by actual geth')
        require(client.prompt_counts.get('Authorize exact transfer') == 1 and
                client.prompt_counts.get('Reauthenticate this transfer') == 1,
                'Spend approval/password prompts were omitted or duplicated')
        report['approval'] = dict(network=NETWORK, asset=asset['id'], recipient=node_fixture.RECIPIENT,
                                  atomicAmount=str(ATOMIC), maximumFee=plan['maxFee'], passwordPromptCount=1)
        passed('Exact Go-authored spend approval and password reauthentication precede real signed broadcast')
        receipt = wait(lambda: node.rpc('eth_getTransactionReceipt', [sent['identity']]))
        require(receipt['status'] == '0x1' and receipt['transactionHash'] == sent['identity'], 'Independent real geth receipt does not establish success')
        transaction = node.rpc('eth_getTransactionByHash', [sent['identity']])
        require(transaction['from'].lower() == node_fixture.SOURCE.lower() and
                transaction['to'].lower() == node_fixture.RECIPIENT.lower() and
                int(transaction['value'], 16) == ATOMIC and int(transaction['nonce'], 16) == 0 and
                int(transaction['chainId'], 16) == 11155111 and transaction['input'] == '0x' and transaction['type'] == '0x2',
                'Independently queried geth transaction differs from the exact approved payment')
        fee = int(receipt['gasUsed'], 16) * int(receipt['effectiveGasPrice'], 16)
        source_after = int(node.rpc('eth_getBalance', [node_fixture.SOURCE, 'latest']), 16)
        recipient_after = int(node.rpc('eth_getBalance', [node_fixture.RECIPIENT, 'latest']), 16)
        require(int(receipt['gasUsed'], 16) == 21000 and 0 < fee <= int(plan['maxFee']) and
                source_before - source_after == ATOMIC + fee and recipient_after - recipient_before == ATOMIC,
                'Independent actual balance deltas or execution fee differ')
        observed_height = int(node.rpc('eth_blockNumber', []), 16)
        wait(lambda: int(node.rpc('eth_blockNumber', []), 16) >= observed_height + 3, 15)
        require(int(node.rpc('eth_getTransactionCount', [node_fixture.SOURCE, 'pending']), 16) == 1 and
                int(node.rpc('eth_getBalance', [node_fixture.RECIPIENT, 'latest']), 16) == recipient_after,
                'Delayed independent observation found duplicate or missing payment')
        transactions = []
        for height in range(first_block, int(node.rpc('eth_blockNumber', []), 16) + 1):
            transactions.extend(tx for tx in node.rpc('eth_getBlockByNumber', [hex(height), True])['transactions']
                                if tx['from'].lower() == node_fixture.SOURCE.lower())
        require(len(transactions) == 1 and transactions[0]['hash'] == sent['identity'], 'Actual chain contains an unexpected additional source payment')
        report['transfer'] = dict(identity=sent['identity'], source=node_fixture.SOURCE, recipient=node_fixture.RECIPIENT,
            chainId=11155111, decimalAmount=DECIMAL, atomicAmount=str(ATOMIC), sourceBefore=str(source_before),
            sourceAfter=str(source_after), recipientBefore=str(recipient_before), recipientAfter=str(recipient_after),
            actualFee=str(fee), receipt=receipt, nonceAfter=1, sourceTransactionCount=1)
        passed('Independent real receipt, transaction, exact balances, fee and delayed nonce prove one executed payment')
        reconciled = client.call('reconcile', {'id': plan['id']})
        require(reconciled['identity'] == sent['identity'] and reconciled['state'] == 'CONFIRMED',
                'Wallet did not reconcile the original signed identity to confirmed execution')
        journal = client.call('history', {'accountId': ethereum['id'], 'asset': asset['id']})
        require(journal['complete'] and len(journal['operations']) == 1 and
                journal['operations'][0]['plan']['id'] == plan['id'] and
                journal['operations'][0]['state'] == 'CONFIRMED' and
                journal['operations'][0]['receipt']['identity'] == sent['identity'] and
                'signed' not in journal['operations'][0], 'Durable public journal differs from independently established execution')
        report['journal'] = dict(operationID=plan['id'], identity=sent['identity'], state='CONFIRMED', count=1)
        client.close()
        client = Client(executable, environment)
        clients.append(client)
        require(client.call('accounts')['locked'], 'Restarted portable application did not start locked')
        client.call('unlock')
        recovered = client.call('history', {'accountId': ethereum['id'], 'asset': asset['id']})
        require(recovered['complete'] and len(recovered['operations']) == 1 and
                recovered['operations'][0]['plan']['id'] == plan['id'] and
                recovered['operations'][0]['state'] == 'CONFIRMED' and
                recovered['operations'][0]['receipt']['identity'] == sent['identity'] and
                'signed' not in recovered['operations'][0] and
                int(node.rpc('eth_getTransactionCount', [node_fixture.SOURCE, 'pending']), 16) == 1,
                'Restarted encrypted journal lost confirmation or duplicated the payment')
        report['journal']['restartVerified'] = True
        passed('Wallet reconciles the original identity and restores one confirmed public journal row after locked restart')
        client.close()
        needles = [value for secret in (PASSWORD.encode(), PHRASE.encode())
                   for value in (secret, secret.hex().encode(), base64.b64encode(secret))]
        require(not any(needle in raw for observed in clients for raw in observed.public_frames for needle in needles),
                'Public wallet responses contain a synthetic credential canary')
        for folder in (home, config):
            for path in folder.rglob('*'):
                if path.is_file() and path.stat().st_size < 32 * 1024 * 1024:
                    require(not any(needle in path.read_bytes() for needle in needles), 'Disposable app files contain a synthetic credential canary')
        for observed in clients:
            observed.log.seek(0)
            log_data = observed.log.read()
            require(not any(needle in log_data for needle in needles), 'Captured application log contains a synthetic credential canary')
        passed('Ordinary public frames, encrypted-profile files and application logs exclude tested credential canaries')
        after = sources()
        report['sourceStable'] = after == report['sourceSHA256']
        if not report['sourceStable']:
            report['changedSources'] = [name for name in sorted(after.keys() | report['sourceSHA256'].keys())
                                        if after.get(name) != report['sourceSHA256'].get(name)]
        require(report['sourceStable'], 'Go or test source changed during real payment integration')
        report['status'] = 'PASS'
    except Blocked as error:
        report.update(status='BLOCKED', reason=str(error))
    except RuntimeError as error:
        report.update(status='FAIL', reason=str(error))
    except Exception as error:
        report.update(status='FAIL', failureType=type(error).__name__, reason='Portable local transaction prerequisite/protocol failed; no secret diagnostic retained')
    finally:
        for observed in clients:
            observed.close()
            observed.log.close()
        if node is not None:
            node.close()
        report['seconds'] = round(time.monotonic() - started, 3)
        save(report)
    print(json.dumps({key: value for key, value in report.items() if key != 'sourceSHA256'}, indent=2))
    return report['status'] != 'PASS'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--isolated-network', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--parent-network-namespace', help=argparse.SUPPRESS)
    parser.add_argument('--owned-directory', type=pathlib.Path, help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.isolated_network:
        require(args.parent_network_namespace and re.fullmatch(r'net:\[[0-9]+\]', args.parent_network_namespace) and
                args.owned_directory and args.owned_directory.is_dir(), 'Missing owned network isolation proof')
        return inside(args.parent_network_namespace, args.owned_directory)
    report = dict(status='BLOCKED', scope=__doc__, sourceSHA256=sources(), steps=[],
                  notRun=['Graphical UI interaction', 'Public funded testnets', 'Clean Ubuntu X11/Wayland OS matrix'])
    process = None
    try:
        if os.getuid() == 0:
            raise Blocked('Portable transaction test requires an ordinary host UID')
        node_fixture.cached_geth(ROOT)  # Already cached official image; never pull.
        save({**report, 'status': 'RUNNING'})
        with tempfile.TemporaryDirectory(prefix='cryptowallet-portable-send-') as directory:
            process = subprocess.Popen(['unshare', '--user', '--map-current-user', '--net', '--keep-caps',
                sys.executable, str(pathlib.Path(__file__).resolve()), '--isolated-network',
                '--parent-network-namespace', os.readlink('/proc/self/ns/net'), '--owned-directory', directory],
                cwd=ROOT, start_new_session=True)
            try:
                code = process.wait(timeout=180)
            finally:
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                    time.sleep(.1)
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                if process.poll() is None:
                    process.wait(timeout=5)
            report = json.loads(REPORT.read_text())
            if report.get('status') == 'RUNNING':
                report.update(status='BLOCKED', reason='Owned namespace did not complete; no host-network fallback attempted')
                save(report)
            return bool(code) or report.get('status') != 'PASS'
    except Blocked as error:
        report.update(status='BLOCKED', reason=str(error))
    except Exception:
        report.update(status='FAIL' if process else 'BLOCKED',
                      reason='Owned portable transaction prerequisite/deadline failed; no network fallback attempted')
    save(report)
    print(json.dumps({key: value for key, value in report.items() if key != 'sourceSHA256'}, indent=2))
    return 1


if __name__ == '__main__':
    raise SystemExit(main())
