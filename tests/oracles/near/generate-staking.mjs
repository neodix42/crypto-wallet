// PUBLIC SYNTHETIC VECTORS ONLY. No inputs, user phrases or remote requests.
import {createRequire} from 'node:module';
import {createHash} from 'node:crypto';
import {writeFileSync} from 'node:fs';
import {KeyPair, PublicKey} from '@near-js/crypto';
import {createTransaction, actionCreators, SignedTransaction, Signature} from '@near-js/transactions';
const require = createRequire(import.meta.url);
const {parseSeedPhrase} = require('near-seed-phrase');
const bs58 = require('bs58');
const mnemonic = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const vectors = [];
for (const index of [0, 1, 1234]) {
  const path = `m/44'/397'/${index}'`;
  const keys = parseSeedPhrase(mnemonic, path);
  const key = KeyPair.fromString(keys.secretKey);
  const publicKey = PublicKey.fromString(keys.publicKey);
  const address = Buffer.from(bs58.decode(keys.publicKey.slice(8))).toString('hex');
  for (const method of ['deposit_and_stake', 'unstake', 'unstake_all', 'withdraw_all']) {
    const amount = '123456789012345678901234';
    const args = method === 'unstake' ? {amount} : {};
    const deposit = method === 'deposit_and_stake' ? amount : '0';
    const gas = '100000000000000';
    const block = Uint8Array.from({length: 32}, (_, i) => i);
    const receiver = 'oracle.pool.f863973.m0';
    const transaction = createTransaction(address, publicKey, receiver, 42n,
      [actionCreators.functionCall(method, args, BigInt(gas), BigInt(deposit))], block);
    const raw = transaction.encode();
    const hash = createHash('sha256').update(raw).digest();
    const sig = key.sign(hash).signature;
    const signed = new SignedTransaction({transaction, signature: new Signature({keyType: 0, data: sig})});
    vectors.push({index, path, address, publicKey: keys.publicKey, recipient: receiver,
      method, args: JSON.stringify(args), amount, deposit, gas, nonce: '42', blockHash: bs58.encode(block),
      raw: Buffer.from(raw).toString('hex'), hash: bs58.encode(hash), signature: Buffer.from(sig).toString('hex'),
      signed: Buffer.from(signed.encode()).toString('hex')});
  }
}
writeFileSync(new URL('./staking-fixtures.json', import.meta.url), JSON.stringify({
  oracle: {'near-seed-phrase': '0.2.1', '@near-js/transactions': '2.5.1', '@near-js/crypto': '2.5.1',
    sourceCommit: '345de7c326bc2da93dc0625ace8d123fc3042b17'}, mnemonic, passphrase: '', vectors
}, null, 2) + '\n');
console.log('Wrote 12 public NEAR staking FunctionCall byte/signature fixtures.');
