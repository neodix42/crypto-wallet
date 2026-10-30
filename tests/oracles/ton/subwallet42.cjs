// Independent official V5R1 wrapper fixtures. Public synthetic keys only.
'use strict';
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const { WalletContractV5R1 } = require('@ton/ton');
const { beginCell, external, storeMessage, storeStateInit } = require('@ton/core');
const { keyPairFromSeed } = require('@ton/crypto');
const seed = crypto.pbkdf2Sync('abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about', 'mnemonic', 2048, 64, 'sha512');
function derive(index) {
  let material = crypto.createHmac('sha512', 'ed25519 seed').update(seed).digest();
  for (const part of [44, 607, index]) {
    const data = Buffer.alloc(37); material.copy(data, 1, 0, 32); data.writeUInt32BE(part + 0x80000000, 33);
    material = crypto.createHmac('sha512', material.subarray(32)).update(data).digest();
  }
  return keyPairFromSeed(material.subarray(0, 32));
}
const output = {oracle: '@ton/ton 16.3.0; @ton/core 0.63.1; public BIP39 abandon/about', profile: 'ton-bip39-slip10-v5r1-subwallet42-v2', accounts: []};
for (const testnet of [false, true]) for (const index of [0, 1, 17]) {
  const key = derive(index);
  const wallet = WalletContractV5R1.create({publicKey: key.publicKey, walletId: {networkGlobalId: testnet ? -3 : -239, context: {workchain: 0, walletVersion: 'v5r1', subwalletNumber: 42}}});
  const state = beginCell().store(storeStateInit(wallet.init)).endCell();
  const body = wallet.createTransfer({seqno: 0, timeout: 2000000000, secretKey: key.secretKey, sendMode: 3, messages: []});
  const message = beginCell().store(storeMessage(external({to: wallet.address, init: wallet.init, body}))).endCell();
  output.accounts.push({network: testnet ? 'ton-testnet' : 'ton-mainnet', index, publicKey: key.publicKey.toString('hex'), address: wallet.address.toString({bounceable: false, testOnly: testnet}), stateHash: state.hash().toString('hex'), bodyHash: body.hash().toString('hex'), deploymentExternalHash: message.hash().toString('hex')});
}
const target = __dirname + '/subwallet42-fixtures.json';
if (process.argv.includes('--generate')) { fs.writeFileSync(target, JSON.stringify(output, null, 2) + '\n'); console.log('Generated six independent V5R1 subwallet42 profile/deployment fixture rows'); }
else { assert.deepEqual(output, JSON.parse(fs.readFileSync(target, 'utf8'))); console.log('PASS: six independent subwallet42 StateInit, address, deployment/signature fixture rows'); }
