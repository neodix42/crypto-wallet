"""Owned real geth/Engine-API chain with genesis-only public synthetic funds.

Must run inside an isolated loopback-only network namespace. No image pulls,
peer networking, unlock API or transaction mocks are used. Geth is extracted
only from the already cached pinned official image, before namespace entry.
"""
import base64
import hashlib
import hmac
import json
import pathlib
import signal
import subprocess
import threading
import time
import urllib.request

IMAGE = 'ethereum/client-go:v1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109'
GETH_SHA256 = '75da01386ed074c086f18e52e034b79cfc6974232f196ca1c72bb16ca169e3e2'
SOURCE = '0x9858EfFD232B4033E47d90003D41EC34EcaEda94'
RECIPIENT = '0x1111111111111111111111111111111111111111'
INITIAL_ATOMIC = 1000 * 10 ** 18


def cached_geth(root):
    target = pathlib.Path(root) / '.tools/geth-1.17.9/geth'
    if not target.is_file():
        subprocess.run(['docker', 'image', 'inspect', IMAGE], check=True, capture_output=True, timeout=20)
        target.parent.mkdir(parents=True, exist_ok=True)
        container = subprocess.check_output(['docker', 'create', '--pull=never', IMAGE], text=True, timeout=20).strip()
        try:
            subprocess.run(['docker', 'cp', container + ':/usr/local/bin/geth', str(target)], check=True, capture_output=True, timeout=30)
        finally:
            subprocess.run(['docker', 'rm', container], check=True, capture_output=True, timeout=20)
        target.chmod(0o755)
    if target.is_symlink() or hashlib.sha256(target.read_bytes()).hexdigest() != GETH_SHA256:
        raise RuntimeError('Pinned cached geth executable hash differs')
    return target


class LocalGeth:
    endpoint = 'http://127.0.0.1:18547'
    engine_endpoint = 'http://127.0.0.1:18548'

    def __init__(self, root, directory, binary):
        self.root, self.directory, self.binary = pathlib.Path(root), pathlib.Path(directory), pathlib.Path(binary)
        self.jwt = bytes(range(32))  # Public Engine-API test authentication only.
        self.stop = threading.Event()
        self.failure = None
        self.process = self.thread = self.log = None

    def rpc(self, method, params, engine=False):
        headers = {'Content-Type': 'application/json'}
        if engine:
            def encode(value):
                return base64.urlsafe_b64encode(json.dumps(value, separators=(',', ':')).encode()).rstrip(b'=')
            value = encode({'alg': 'HS256', 'typ': 'JWT'}) + b'.' + encode({'iat': int(time.time())})
            headers['Authorization'] = 'Bearer ' + (value + b'.' + base64.urlsafe_b64encode(hmac.new(self.jwt, value, hashlib.sha256).digest()).rstrip(b'=')).decode()
        request = urllib.request.Request(self.engine_endpoint if engine else self.endpoint,
            data=json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}).encode(), headers=headers)
        with urllib.request.urlopen(request, timeout=5) as response:
            raw = response.read(4 * 1024 * 1024 + 1)
        if len(raw) > 4 * 1024 * 1024:
            raise RuntimeError('Owned geth response exceeds test limit')
        result = json.loads(raw)
        if result.get('error') is not None:
            raise RuntimeError('Owned geth RPC rejected a test request')
        return result['result']

    def start(self):
        self.directory.mkdir(mode=0o700)
        genesis = json.loads((self.root / 'tests/oracles/evm/geth-genesis.json').read_text())
        for fork in ('osakaTime', 'bogotaTime'):
            genesis['config'].pop(fork, None)
        if genesis['config']['chainId'] != 11155111:
            raise RuntimeError('Local test genesis chain identity differs')
        genesis['alloc'][SOURCE[2:].lower()] = {'balance': hex(INITIAL_ATOMIC)}
        (self.directory / 'genesis.json').write_text(json.dumps(genesis))
        (self.directory / 'jwt.hex').write_text(self.jwt.hex())
        self.log = (self.directory / 'geth.log').open('w')
        self.process = subprocess.Popen([str(self.binary), '--datadir', str(self.directory / 'chain'),
            '--syncmode', 'full', '--override.genesis', str(self.directory / 'genesis.json'),
            '--authrpc.jwtsecret', str(self.directory / 'jwt.hex'), '--authrpc.addr', '127.0.0.1',
            '--authrpc.port', '18548', '--authrpc.vhosts', 'localhost,127.0.0.1',
            '--http', '--http.addr', '127.0.0.1', '--http.port', '18547', '--http.api', 'eth,net,web3',
            '--http.vhosts', 'localhost,127.0.0.1', '--nodiscover', '--maxpeers', '0',
            '--discovery.dns', '', '--nat', 'none'], stdout=self.log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise RuntimeError('Pinned owned geth failed startup')
            try:
                self.head = self.rpc('eth_getBlockByNumber', ['latest', False])
                if self.rpc('eth_chainId', []) == '0xaa36a7':
                    break
            except (OSError, ValueError, RuntimeError):
                pass
            time.sleep(.1)
        else:
            raise RuntimeError('Pinned owned geth readiness deadline exceeded')
        self.thread = threading.Thread(target=self.mine, daemon=True)
        self.thread.start()
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            self.healthy()
            if int(self.rpc('eth_blockNumber', []), 16) > 0:
                if int(self.rpc('eth_getBalance', [SOURCE, 'latest']), 16) != INITIAL_ATOMIC:
                    raise RuntimeError('Synthetic genesis funding differs')
                return
            time.sleep(.1)
        raise RuntimeError('Pinned owned geth did not produce a real block')

    def healthy(self):
        if self.failure or self.process.poll() is not None:
            raise RuntimeError('Pinned owned geth Engine driver failed')

    def mine(self):
        try:
            zero = '0x' + '00' * 32
            while not self.stop.is_set():
                timestamp = max(int(self.head['timestamp'], 16) + 1, int(time.time()))
                choice = dict(headBlockHash=self.head['hash'], safeBlockHash=self.head['hash'], finalizedBlockHash=self.head['hash'])
                attributes = dict(timestamp=hex(timestamp), prevRandao=zero, suggestedFeeRecipient='0x' + '00' * 20,
                                  withdrawals=[], parentBeaconBlockRoot=zero)
                built = self.rpc('engine_forkchoiceUpdatedV3', [choice, attributes], True)
                if built['payloadStatus']['status'] != 'VALID':
                    raise RuntimeError('Invalid real Engine fork choice')
                if self.stop.wait(1):
                    break
                full = self.rpc('engine_getPayloadV4', [built['payloadId']], True)
                payload = full['executionPayload']
                accepted = self.rpc('engine_newPayloadV4', [payload, [], zero, full['executionRequests']], True)
                if accepted['status'] != 'VALID':
                    raise RuntimeError('Real Engine payload rejected')
                choice = dict(headBlockHash=payload['blockHash'], safeBlockHash=payload['blockHash'], finalizedBlockHash=payload['blockHash'])
                if self.rpc('engine_forkchoiceUpdatedV3', [choice, None], True)['payloadStatus']['status'] != 'VALID':
                    raise RuntimeError('Real Engine canonical block rejected')
                self.head = self.rpc('eth_getBlockByNumber', ['latest', False])
        except Exception:
            self.failure = True

    def close(self):
        self.stop.set()
        if self.thread:
            self.thread.join(timeout=10)
        if self.process and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        if self.log:
            self.log.close()
