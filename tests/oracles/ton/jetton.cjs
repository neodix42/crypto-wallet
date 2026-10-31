// Public synthetic fixture state, actual pinned Tether contracts and TVM.
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const crypto = require('node:crypto');
const { Blockchain, createShardAccount } = require('@ton/sandbox');
const { WalletContractV5R1 } = require('@ton/ton');
const { keyPairFromSeed } = require('@ton/crypto');
const { Address, Cell, Dictionary, beginCell, contractAddress, internal, external, storeMessage, storeTransaction, toNano } = require('@ton/core');
function fixtureKey(index) {
 const seed=crypto.pbkdf2Sync('abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about','mnemonic',2048,64,'sha512');
 let m=crypto.createHmac('sha512','ed25519 seed').update(seed).digest();for(const part of [44,607,index]){const d=Buffer.alloc(37);m.copy(d,1,0,32);d.writeUInt32BE(part+0x80000000,33);m=crypto.createHmac('sha512',m.subarray(32)).update(d).digest();}return keyPairFromSeed(m.subarray(0,32));
}
async function main(){
 const r=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));const artifacts=JSON.parse(fs.readFileSync(r.artifacts,'utf8'));const key=fixtureKey(r.index);
 const wallet=WalletContractV5R1.create({publicKey:key.publicKey,walletId:{networkGlobalId:-239,context:{workchain:0,walletVersion:'v5r1',subwalletNumber:r.subwallet}}});assert(wallet.address.equals(Address.parse(r.address)));
 const master=Address.parse(r.master),code=Cell.fromBase64(r.walletLibrary),fullCode=Cell.fromBase64(artifacts.walletCode),dest=Address.parse(r.recipient);
 assert.equal(fullCode.hash().toString('hex'),'8f452d7a4dfd74066b682365177259ed05734435be76b5fd4bd5d8af2b7c3d68');
 const config=(owner,balance)=>beginCell().storeUint(0,4).storeCoins(balance).storeAddress(owner).storeAddress(master).endCell();
 const source=contractAddress(0,{code,data:config(wallet.address,0n)});const destination=contractAddress(0,{code,data:config(dest,0n)});assert(source.equals(Address.parse(r.sourceJetton)));assert(destination.equals(Address.parse(r.recipientJetton)));
 const forward=beginCell().storeUint(0,32).storeStringTail(r.memo).endCell();const body=beginCell().storeUint(0x0f8a7ea5,32).storeUint(BigInt(r.queryId),64).storeCoins(BigInt(r.amount)).storeAddress(dest).storeAddress(wallet.address).storeBit(0).storeCoins(1n).storeBit(1).storeRef(forward).endCell();
 const signedBody=wallet.createTransfer({seqno:0,timeout:r.expires,secretKey:key.secretKey,sendMode:3,messages:[internal({to:source,value:toNano('0.1'),bounce:true,body})]});const expected=beginCell().store(storeMessage(external({to:wallet.address,init:wallet.init,body:signedBody}))).endCell();const signed=Cell.fromBase64(r.boc);assert(signed.equals(expected),'official SDK jetton bytes/signature mismatch');
 const chain=await Blockchain.create();chain.now=r.expires-30;chain.verbosity={print:false,blockchainLogs:false,vmLogs:'none',debugLogs:false};const libs=Dictionary.empty(Dictionary.Keys.Buffer(32),Dictionary.Values.Cell());libs.set(fullCode.hash(),fullCode);chain.libs=beginCell().storeDictDirect(libs).endCell();
 await chain.setShardAccount(master,createShardAccount({address:master,code:Cell.fromBase64(artifacts.masterCode),data:Cell.fromBase64(artifacts.masterData),balance:toNano('1')}));
 await chain.setShardAccount(source,createShardAccount({address:source,code,data:config(wallet.address,100000000n),balance:toNano('0.05')}));
 const treasury=await chain.treasury('public-usdt-prestate');const funding=await treasury.send({to:wallet.address,value:toNano('1'),bounce:false});const before=chain.snapshot();
 const result=await chain.sendMessage(signed);const sourceBalance=(await chain.runGetMethod(source,'get_wallet_data')).stackReader.readBigNumber();const destinationBalance=(await chain.runGetMethod(destination,'get_wallet_data')).stackReader.readBigNumber();assert.equal(sourceBalance,100000000n-BigInt(r.amount));assert.equal(destinationBalance,BigInt(r.amount));assert.equal((await chain.runGetMethod(wallet.address,'seqno')).stackReader.readNumber(),1);
 let replayRejected=false;try{await chain.sendMessage(signed)}catch{replayRejected=true}assert(replayRejected);
 const states={};for(const ad of [wallet.address,source,destination]){const c=await chain.getContract(ad);assert.equal(c.accountState.type,'active');states[ad.toRawString()]={state:'active',balance:c.balance.toString(),code:c.accountState.state.code.toBoc().toString('base64'),data:c.accountState.state.data.toBoc().toString('base64')};}
 const transactions={};for(const tx of [...funding.transactions,...result.transactions]){const ad=new Address(0,Buffer.from(tx.address.toString(16).padStart(64,'0'),'hex')).toRawString();(transactions[ad]??=[]).push({data:beginCell().store(storeTransaction(tx)).endCell().toBoc().toString('base64'),lt:tx.lt.toString()});}for(const rows of Object.values(transactions))rows.sort((a,b)=>BigInt(a.lt)>BigInt(b.lt)?-1:1);
 await chain.loadFrom(before);chain.now=r.expires+1;let expiryRejected=false;try{await chain.sendMessage(signed)}catch{expiryRejected=true}assert(expiryRejected);
 fs.writeFileSync(process.argv[3],JSON.stringify({status:'PASS',states,transactions,sourceBalance:sourceBalance.toString(),destinationBalance:destinationBalance.toString(),independentBytes:true,replayRejected,expiryRejected},null,2)+'\n');console.log('PASS actual pinned canonical USDT contracts: independent SDK BOC/signature; V5R1 deployment + transfer + recipient deployment/credit; balances; replay/expiry rejection');
}
main().catch(e=>{console.error(e);process.exit(1)});
