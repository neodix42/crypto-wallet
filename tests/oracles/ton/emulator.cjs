'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { Blockchain } = require('@ton/sandbox');
const { Address, Cell, beginCell, internal, toNano, storeTransaction } = require('@ton/core');
// The request contains public descriptors, an already-signed external message
// and synthetic test amounts. This independently executes the Go-produced BOC.
async function main() {
  const req=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
  const chain=await Blockchain.create();
  chain.now=req.expires-30;
  chain.verbosity={print:false,blockchainLogs:false,vmLogs:'none',debugLogs:false};
  const treasury=await chain.treasury('synthetic-funder');
  const source=Address.parse(req.address), destination=Address.parse(req.recipient);
  await treasury.send({to:source,value:toNano('10'),bounce:false});
  const funded=chain.snapshot();
  const before=(await chain.getContract(destination)).balance;
  const signed=Cell.fromBase64(req.boc);
  const result=await chain.sendMessage(signed);
  const senderTx=result.transactions.find(t=>t.inMessage?.info.type==='external-in'&&t.inMessage.info.dest.equals(source));
  assert(senderTx,'source external-in transaction absent');
  assert.equal(senderTx.description.type,'generic');
  assert.equal(senderTx.description.aborted,false);
  assert.equal(senderTx.description.computePhase.type,'vm');
  assert.equal(senderTx.description.computePhase.success,true);
  assert.equal(senderTx.description.actionPhase.success,true);
  assert.equal(senderTx.outMessagesCount,1);
  const out=senderTx.outMessages.values()[0];
  assert(out.info.dest.equals(destination));
  assert.equal(out.info.value.coins,BigInt(req.amount));
  const recipientTx=result.transactions.find(t=>t.inMessage?.info.type==='internal'&&t.inMessage.info.dest.equals(destination));
  assert(recipientTx,'recipient transaction absent');
  assert.equal(recipientTx.inMessage.info.value.coins,BigInt(req.amount));
  const after=(await chain.getContract(destination)).balance;
  assert(after>before,'recipient balance did not increase');
  const sourceContract=await chain.getContract(source);
  assert.equal(sourceContract.accountState.type,'active');
  const seq=await chain.runGetMethod(source,'seqno');
  assert.equal(seq.stackReader.readNumber(),1);
  // Replay must not create a second payment.
  let replayRejected=false;try{await chain.sendMessage(signed);}catch{replayRejected=true;}
  assert.equal(replayRejected,true,'duplicate external message was accepted');
  assert.equal((await chain.getContract(destination)).balance,after);
  // The fresh index1 recipient deploys its own V5R1 wallet and returns real
  // native value using the second Go-signed BOC in this same live TVM state.
  const receiverBeforeReturn=(await chain.getContract(source)).balance;
  const returned=await chain.sendMessage(Cell.fromBase64(req.returnBoc));
  const returnSource=returned.transactions.find(t=>t.inMessage?.info.type==='external-in'&&t.inMessage.info.dest.equals(destination));
  assert(returnSource,'return source transaction absent');
  assert.equal(returnSource.description.computePhase.type,'vm');
  assert.equal(returnSource.description.computePhase.success,true);
  assert.equal(returnSource.description.actionPhase.success,true);
  assert.equal(returnSource.description.aborted,false);
  assert.equal(returnSource.outMessagesCount,1);
  assert.equal(returnSource.outMessages.values()[0].info.value.coins,BigInt(req.returnAmount));
  assert(returnSource.outMessages.values()[0].info.dest.equals(source));
  const returnRecipient=returned.transactions.find(t=>t.inMessage?.info.type==='internal'&&t.inMessage.info.dest.equals(source));
  assert(returnRecipient,'final return recipient transaction absent');
  assert.equal(returnRecipient.description.creditPhase.credit.coins,BigInt(req.returnAmount));
  const receiverAfterReturn=(await chain.getContract(source)).balance;
  assert(receiverAfterReturn>receiverBeforeReturn,'final receiver balance did not increase');
  assert.equal((await chain.getContract(destination)).accountState.type,'active');
  assert.equal((await chain.runGetMethod(destination,'seqno')).stackReader.readNumber(),1);
  const returnSenderFinal=(await chain.getContract(destination)).balance;
  assert(returnSenderFinal>0n&&returnSenderFinal<after,'return sender balance did not reflect spend and fees');
  await chain.loadFrom(funded);
  chain.now=req.expires+1;
  let expiredRejected=false;try{await chain.sendMessage(signed);}catch{expiredRejected=true;}
  assert.equal(expiredRejected,true,'expired external message was accepted');
  // IGNORE_ERRORS can consume seqno without paying anyone. The adapter must
  // report this action outcome as failure, never infer success from seqno.
  await chain.loadFrom(funded);
  chain.now=req.expires-30;
  const failed=await chain.sendMessage(Cell.fromBase64(req.insufficientBoc));
  const failedSource=failed.transactions.find(t=>t.inMessage?.info.type==='external-in');
  assert(failedSource);
  assert.equal(failedSource.outMessagesCount,0);
  const consumed=await chain.runGetMethod(source,'seqno');
  assert.equal(consumed.stackReader.readNumber(),1);
  assert.equal((await chain.getContract(destination)).balance,0n);
  const txs=result.transactions.map(t=>({address:t.inMessage?.info.dest?.toRawString(),data:beginCell().store(storeTransaction(t)).endCell().toBoc().toString('base64')}));
  const failedTxs=failed.transactions.map(t=>({address:t.inMessage?.info.dest?.toRawString(),data:beginCell().store(storeTransaction(t)).endCell().toBoc().toString('base64')}));
  const returnTxs=returned.transactions.map(t=>({address:t.inMessage?.info.dest?.toRawString(),data:beginCell().store(storeTransaction(t)).endCell().toBoc().toString('base64')}));
  const report={status:'PASS',oracle:'@ton/sandbox 0.45.0',deployment:true,sourceActionSuccess:true,recipientCredit:true,replayRejected:true,expiredRejected:true,seqnoAdvancedWithoutPaymentDetected:true,recipientBalanceDelta:(after-before).toString(),roundtrip:true,returnDeployment:true,returnedAtoms:req.returnAmount,returnRecipientBalanceDelta:(receiverAfterReturn-receiverBeforeReturn).toString(),returnSenderFinalBalance:returnSenderFinal.toString(),transactions:txs,returnTransactions:returnTxs,failedTransactions:failedTxs};
  fs.writeFileSync(process.argv[3],JSON.stringify(report,null,2)+'\n');
  console.log('PASS: Go-signed V5R1 deployment/payment and index1 deploy/send-back, independent final credit/balances, replay/expiry rejection, consumed-seqno action failure');
}
main().catch(e=>{console.error(e);process.exit(1);});
