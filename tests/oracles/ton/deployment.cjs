// Independent actual TVM execution of a Go-signed deployment-only message.
// Fixed public BIP39 fixture only; this helper never accepts user secrets.
'use strict';
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const { Blockchain } = require('@ton/sandbox');
const { WalletContractV5R1 } = require('@ton/ton');
const { keyPairFromSeed } = require('@ton/crypto');
const { Address, Cell, beginCell, external, storeMessage, storeTransaction, toNano } = require('@ton/core');

function fixtureKey(index) {
  const seed = crypto.pbkdf2Sync('abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about', 'mnemonic', 2048, 64, 'sha512');
  let material = crypto.createHmac('sha512', 'ed25519 seed').update(seed).digest();
  for (const part of [44, 607, index]) {
    const data = Buffer.alloc(37); material.copy(data, 1, 0, 32); data.writeUInt32BE(part + 0x80000000, 33);
    material = crypto.createHmac('sha512', material.subarray(32)).update(data).digest();
  }
  return keyPairFromSeed(material.subarray(0, 32));
}

async function main() {
  const request = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
  const key = fixtureKey(request.index);
  const wallet = WalletContractV5R1.create({publicKey: key.publicKey, walletId: {networkGlobalId: request.globalId, context: {workchain: 0, walletVersion: 'v5r1', subwalletNumber: request.subwallet}}});
  assert(wallet.address.equals(Address.parse(request.address)), 'independent SDK StateInit address differs');
  const body = wallet.createTransfer({seqno: 0, timeout: request.expires, secretKey: key.secretKey, sendMode: 3, messages: []});
  const expected = beginCell().store(storeMessage(external({to: wallet.address, init: wallet.init, body}))).endCell();
  const signed = Cell.fromBase64(request.boc);
  assert(signed.equals(expected), 'Go deployment BOC/signature differs from official SDK');

  const chain = await Blockchain.create();
  chain.now = request.expires - 30;
  chain.verbosity = {print: false, blockchainLogs: false, vmLogs: 'none', debugLogs: false};
  const treasury = await chain.treasury('deployment-only-public-fixture');
  await treasury.send({to: wallet.address, value: toNano('1'), bounce: false});
  assert.equal((await chain.getContract(wallet.address)).accountState.type, 'uninit');
  const funded = chain.snapshot();
  const before = (await chain.getContract(wallet.address)).balance;
  const execution = await chain.sendMessage(signed);
  const transaction = execution.transactions.find(tx => tx.inMessage?.info.type === 'external-in');
  assert(transaction, 'deployment execution absent');
  assert.equal(transaction.description.type, 'generic');
  assert.equal(transaction.description.aborted, false);
  assert.equal(transaction.description.computePhase.type, 'vm');
  assert.equal(transaction.description.computePhase.success, true);
  assert.equal(transaction.outMessagesCount, 0);
  assert.equal((await chain.getContract(wallet.address)).accountState.type, 'active');
  assert.equal((await chain.runGetMethod(wallet.address, 'seqno')).stackReader.readNumber(), 1);
  assert.equal((await chain.runGetMethod(wallet.address, 'get_subwallet_id')).stackReader.readBigNumber(), BigInt(request.walletId));
  const after = (await chain.getContract(wallet.address)).balance;
  assert(after > 0n && after < before, 'deployment did not preserve funds less fees');
  // Even with the same signing key, a request carrying the other network's
  // wallet ID cannot control this deployed contract.
  const otherNetwork = WalletContractV5R1.create({publicKey: key.publicKey, walletId: {networkGlobalId: request.globalId === -3 ? -239 : -3, context: {workchain: 0, walletVersion: 'v5r1', subwalletNumber: request.subwallet}}});
  const wrongIdBody = otherNetwork.createTransfer({seqno: 1, timeout: request.expires, secretKey: key.secretKey, sendMode: 3, messages: []});
  let wrongWalletIdRejected = false;
  try { await chain.sendMessage(external({to: wallet.address, body: wrongIdBody})); } catch { wrongWalletIdRejected = true; }
  assert(wrongWalletIdRejected, 'foreign network wallet ID was accepted');
  assert.equal((await chain.runGetMethod(wallet.address, 'seqno')).stackReader.readNumber(), 1);
  let replayRejected = false;
  try { await chain.sendMessage(signed); } catch { replayRejected = true; }
  assert(replayRejected, 'deployment replay was accepted');
  await chain.loadFrom(funded);
  chain.now = request.expires + 1;
  let expiredRejected = false;
  try { await chain.sendMessage(signed); } catch { expiredRejected = true; }
  assert(expiredRejected, 'expired deployment was accepted');

  // Read the successful state from the saved post-deployment transaction's
  // independently executed snapshot, rather than the expiry-rejection state.
  await chain.loadFrom(funded); chain.now = request.expires - 30;
  await chain.sendMessage(signed);
  const active = (await chain.getContract(wallet.address)).accountState;
  const report = {
    status: 'PASS', oracle: '@ton/ton 16.3.0; @ton/sandbox 0.45.0',
    independentBytesAndSignature: true, deployed: true, outgoingMessages: 0,
    replayRejected, expiredRejected, wrongWalletIdRejected, balanceBefore: before.toString(), balanceAfter: after.toString(),
    code: active.state.code.toBoc().toString('base64'), data: active.state.data.toBoc().toString('base64'),
    transactions: execution.transactions.map(tx => ({data: beginCell().store(storeTransaction(tx)).endCell().toBoc().toString('base64')})),
  };
  fs.writeFileSync(process.argv[3], JSON.stringify(report, null, 2) + '\n');
  console.log('PASS: independent V5R1 deployment-only BOC/signature, funded uninit→active, zero outgoing messages, fee-only debit, foreign-wallet-ID, replay and expiry rejection');
}
main().catch(error => { console.error(error); process.exit(1); });
