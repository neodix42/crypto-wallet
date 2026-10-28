// PUBLIC SYNTHETIC LOCALNET ONLY. Never use a remote node or user keys here.
import { Sandbox, GenesisAccount } from 'near-sandbox';
import { createRequire } from 'node:module';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, existsSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { KeyPair, PublicKey } from '@near-js/crypto';
import { createTransaction, actionCreators, SignedTransaction, Signature } from '@near-js/transactions';

mkdirSync(resolve('.tools/tmp'), { recursive: true });
mkdirSync(resolve('reports'), { recursive: true });
process.env.TMPDIR = resolve('.tools/tmp');
const binary = resolve('.tools/near/near-sandbox');
const wasmFile = resolve('tests/oracles/near/contracts/staking_pool.wasm');
const checksum = path => createHash('sha256').update(readFileSync(path)).digest('hex');
if (!existsSync(binary) || checksum(binary) !== '4e89fbc0f2958634e84af44634f90e87c246288fa11b067d2121ce1394cba2c2') {
  throw new Error('BLOCKED: install the pinned local node using tests/oracles/near/install-sandbox.sh');
}
if (checksum(wasmFile) !== '454cf4a54ff49a1b144e2591eb4ec833bf3ab5fa4792128beb6d9d0cc8d76e58') {
  throw new Error('Staking-pool fixture differs from its pinned upstream bytecode');
}
process.env.NEAR_SANDBOX_BIN_PATH = binary;
const require = createRequire(import.meta.url);
const { parseSeedPhrase } = require('near-seed-phrase');
const bs58 = require('bs58');
const keys = parseSeedPhrase('abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about');
const owner = Buffer.from(bs58.decode(keys.publicKey.slice(8))).toString('hex');
const pool = 'pool.sandbox';
let sandbox, child;
const stop = () => child?.kill('SIGINT');
process.once('SIGINT', stop);
process.once('SIGTERM', stop);
const started = Date.now();
let status = 'FAIL';
try {
  sandbox = await Sandbox.start({ version: '2.13.4', config: {
    // Short real epochs keep sandbox_fast_forward below the production
    // maximum-future-head tolerance. The wallet's freshness checks stay intact.
    additionalGenesis: { chain_id: 'localnet', epoch_length: 10, transaction_validity_period: 20 },
    additionalConfig: { consensus: { min_block_production_delay: { secs: 0, nanos: 500000000 },
      max_block_production_delay: { secs: 0, nanos: 800000000 } } },
    additionalAccounts: [
      new GenesisAccount(owner, keys.publicKey, keys.secretKey, 1000n * 10n ** 24n),
      new GenesisAccount(pool, keys.publicKey, keys.secretKey, 50n * 10n ** 24n),
      new GenesisAccount('operator.sandbox', keys.publicKey, keys.secretKey, 10n * 10n ** 24n),
    ],
  } });
  const rpc = async (method, params) => {
    const response = await fetch(sandbox.rpcUrl, { method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 'fixture', method, params }),
      signal: AbortSignal.timeout(20000) });
    const value = await response.json();
    if (!response.ok || value.error) throw new Error('Local fixture RPC rejected ' + method);
    return value.result;
  };
  const state = await rpc('query', { request_type: 'view_access_key', finality: 'final', account_id: pool, public_key: keys.publicKey });
  const transaction = createTransaction(pool, PublicKey.fromString(keys.publicKey), pool,
    BigInt(state.nonce) + 1n, [
      actionCreators.deployContract(readFileSync(wasmFile)),
      actionCreators.functionCall('new', Buffer.from(JSON.stringify({ owner_id: 'operator.sandbox',
        stake_public_key: keys.publicKey, reward_fee_fraction: { numerator: 5, denominator: 100 } })),
        100000000000000n, 0n),
    ], bs58.decode(state.block_hash));
  const signature = KeyPair.fromString(keys.secretKey).sign(createHash('sha256').update(transaction.encode()).digest()).signature;
  const signed = new SignedTransaction({ transaction, signature: new Signature({ keyType: 0, data: signature }) });
  const deployed = await rpc('send_tx', { signed_tx_base64: Buffer.from(signed.encode()).toString('base64'), wait_until: 'FINAL' });
  if (!('SuccessValue' in deployed.status)) throw new Error('Official staking-pool deployment/initialization failed');
  // The fixture tests real contract delegation/unstaking/transfers, not an
  // elected consensus validator. Pause its validator proposals through the
  // upstream owner's method so below-seat-price Stake failures cannot be
  // mistaken for successful validation. No contract state is patched.
  const operator = await rpc('query', { request_type: 'view_access_key', finality: 'final', account_id: 'operator.sandbox', public_key: keys.publicKey });
  const pause = createTransaction('operator.sandbox', PublicKey.fromString(keys.publicKey), pool,
    BigInt(operator.nonce) + 1n, [actionCreators.functionCall('pause_staking', Buffer.from('{}'), 100000000000000n, 0n)], bs58.decode(operator.block_hash));
  const pauseSignature = KeyPair.fromString(keys.secretKey).sign(createHash('sha256').update(pause.encode()).digest()).signature;
  const paused = await rpc('send_tx', { signed_tx_base64: Buffer.from(new SignedTransaction({ transaction: pause, signature: new Signature({ keyType: 0, data: pauseSignature }) }).encode()).toString('base64'), wait_until: 'FINAL' });
  if (!('SuccessValue' in paused.status)) throw new Error('Official fixture-pool pause failed');
  child = spawn('go', ['test', './tests/staking', '-run', 'TestLocalStakingCycle', '-v', '-count=1'], {
    stdio: 'inherit', env: { ...process.env, WALLET_NEAR_STAKING_LOCAL_RPC: sandbox.rpcUrl },
  });
  const code = await new Promise(resolve => { child.once('exit', resolve); child.once('error', () => resolve(1)); });
  process.exitCode = code ?? 1;
  status = code === 0 ? 'PASS' : 'FAIL';
} finally {
  if (child && child.exitCode === null) child.kill('SIGKILL');
  if (sandbox) await sandbox.tearDown();
  process.removeListener('SIGINT', stop);
  process.removeListener('SIGTERM', stop);
  writeFileSync(resolve('reports/near-staking-local.json'), JSON.stringify({
    status, network: 'localnet', sandboxVersion: '2.13.4',
    contractSource: 'near/core-contracts@1b0436c96379c63aee7d2429c6d5d24019031ab8',
    contractSHA256: checksum(wasmFile), seconds: (Date.now() - started) / 1000,
    scope: 'Actual upstream staking-pool WASM on pinned local NEAR, validator proposals paused through upstream owner method; real Go signer/password approvals, durable encrypted journal, independent state and original-transaction reconciliation; no contract state patched',
    notRun: ['funded public-testnet cycle', 'mainnet spending', 'validator consensus/reward earnings', 'graphical staking approval'],
  }, null, 2) + '\n');
}
