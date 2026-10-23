// Public deterministic test material only. Never run this script with a real
// phrase/seed: production wallet binaries do not import any JavaScript SDK.
'use strict';
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const { WalletContractV5R1 } = require('@ton/ton');
const { Cell, beginCell, storeStateInit, internal, external, storeMessage } = require('@ton/core');
const { keyPairFromSeed } = require('@ton/crypto');
const seed = crypto.pbkdf2Sync('abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about', 'mnemonic', 2048, 64, 'sha512');
function derive(i) {
  let I = crypto.createHmac('sha512','ed25519 seed').update(seed).digest();
  for (const n of [44,607,i]) { const b=Buffer.alloc(37); I.copy(b,1,0,32); b.writeUInt32BE(n+0x80000000,33); I=crypto.createHmac('sha512',I.subarray(32)).update(b).digest(); }
  return keyPairFromSeed(I.subarray(0,32));
}
function wallet(i,testnet) { return WalletContractV5R1.create({ publicKey:derive(i).publicKey, walletId:{networkGlobalId:testnet?-3:-239,context:{workchain:0,walletVersion:'v5r1',subwalletNumber:0}} }); }
function fixtures() {
  const out = {oracle:'@ton/ton 16.3.0; @ton/core 0.63.1; Node PBKDF2/HMAC SLIP-0010; public BIP39 abandon/about', seed:seed.toString('hex'), codeHash:'20834b7b72b112147e1b2fb457b84e74d1a30f04f737d4f62a668e9552d2b72f', accounts:[]};
  for (const testnet of [false,true]) for (const index of [0,1,17]) {
    const w=wallet(index,testnet); const key=derive(index);
    const state=beginCell().store(storeStateInit(w.init)).endCell();
    assert.equal(w.init.code.hash().toString('hex'),out.codeHash);
    const to=wallet(1,testnet).address;
    const body=w.createTransfer({seqno:7,timeout:2000000000,secretKey:key.secretKey,sendMode:3,messages:[internal({to,value:123456789n,bounce:true,body:'fixture'})]});
    const ext=beginCell().store(storeMessage(external({to:w.address,body}))).endCell();
    out.accounts.push({network:testnet?'ton-testnet':'ton-mainnet',index,publicKey:key.publicKey.toString('hex'),address:w.address.toString({bounceable:false,testOnly:testnet}),stateInit:state.toBoc().toString('base64'),stateHash:state.hash().toString('hex'),body:body.toBoc().toString('base64'),bodyHash:body.hash().toString('hex'),externalHash:ext.hash().toString('hex'),recipient:to.toString({bounceable:false,testOnly:testnet})});
  }
  return out;
}
const got=fixtures();
const target=__dirname+'/fixtures.json';
if (process.argv.includes('--generate')) {fs.writeFileSync(target,JSON.stringify(got,null,2)+'\n');console.log('generated six independent TON profile/signature fixtures');}
else {assert.deepEqual(got,JSON.parse(fs.readFileSync(target)));console.log('PASS: six TON address, StateInit, signature and external-message fixture rows');}
