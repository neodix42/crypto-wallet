// Owned real local chain with public BIP39 fixture keys only. No live endpoints.
import {Sandbox,GenesisAccount} from 'near-sandbox';
import {createRequire} from 'node:module';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdirSync,readFileSync,readdirSync,writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {KeyPair,PublicKey} from '@near-js/crypto';
import {createTransaction,actionCreators,SignedTransaction,Signature} from '@near-js/transactions';
const require=createRequire(import.meta.url),{parseSeedPhrase}=require('near-seed-phrase'),bs58=require('bs58');
const mnemonic='abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const keys=[0,1].map(index=>parseSeedPhrase(mnemonic,`m/44'/397'/${index}'`));
const owners=keys.map(k=>Buffer.from(bs58.decode(k.publicKey.slice(8))).toString('hex'));
const contracts=['first.sandbox','second.sandbox'],decimals=[6,18];
const checksum=p=>createHash('sha256').update(readFileSync(p)).digest('hex');
const binary=resolve('.tools/near/near-sandbox'),wasm=resolve('tests/oracles/near/contracts/fungible_token.wasm');
const producer=checksum(new URL(import.meta.url));
const sourceDirs=['internal/chains/near','internal/chains/chainutil','internal/amount','internal/derivation','internal/journal','internal/model','internal/signer','internal/strictjson','internal/vault'];
const sourceFiles=sourceDirs.flatMap(dir=>readdirSync(resolve(dir)).filter(name=>name.endsWith('.go')&&!name.endsWith('_test.go')).map(name=>resolve(dir,name))).concat([resolve('tests/tokens/near_local_test.go'),resolve('go.mod'),resolve('go.sum')]).sort();
const sourceIdentity=()=>Object.fromEntries(sourceFiles.map(path=>[path.split('/crypto-wallet/').at(-1),checksum(path)]));
const source=sourceIdentity();
assert.equal(checksum(binary),'4e89fbc0f2958634e84af44634f90e87c246288fa11b067d2121ce1394cba2c2');
assert.equal(checksum(wasm),'024570509fa82cf25ee2448ab63a8343b3c7ced16b68109d27448beaf4326bbb');
mkdirSync(resolve('.tools/tmp'),{recursive:true});mkdirSync(resolve('reports'),{recursive:true});
process.env.TMPDIR=resolve('.tools/tmp');process.env.NEAR_SANDBOX_BIN_PATH=binary;
let sandbox,child,status='FAIL',transactions=0;const started=Date.now();
const stop=()=>child?.kill('SIGINT');process.once('SIGINT',stop);process.once('SIGTERM',stop);
try {
 sandbox=await Sandbox.start({version:'2.13.4',config:{additionalGenesis:{chain_id:'localnet',epoch_length:64,transaction_validity_period:128},additionalConfig:{consensus:{min_block_production_delay:{secs:0,nanos:500000000},max_block_production_delay:{secs:0,nanos:500000000}}},additionalAccounts:[...owners.map((owner,i)=>new GenesisAccount(owner,keys[i].publicKey,keys[i].secretKey,100n*10n**24n)),...contracts.map(contract=>new GenesisAccount(contract,keys[0].publicKey,keys[0].secretKey,20n*10n**24n))]}});
 const rpc=async(method,params)=>{
  const response=await fetch(sandbox.rpcUrl,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:'fixture',method,params}),signal:AbortSignal.timeout(20000)});
  const out=await response.json();if(!response.ok||out.error)throw new Error(`Owned token fixture RPC rejected ${method}: ${out.error?.cause?.name||out.error?.name||'unknown'}`);return out.result;
 };
 for(const [i,contract]of contracts.entries()){
  const state=await rpc('query',{request_type:'view_access_key',finality:'final',account_id:contract,public_key:keys[0].publicKey});
  const metadata={spec:'ft-1.0.0',name:`Synthetic Token ${decimals[i]}`,symbol:`TEST${decimals[i]}`,decimals:decimals[i],icon:null,reference:null,reference_hash:null};
  const transaction=createTransaction(contract,PublicKey.fromString(keys[0].publicKey),contract,BigInt(state.nonce)+1n,[actionCreators.deployContract(readFileSync(wasm)),actionCreators.functionCall('new',{owner_id:owners[0],total_supply:(1000000n*10n**BigInt(decimals[i])).toString(),metadata},100000000000000n,0n)],bs58.decode(state.block_hash));
  const sig=KeyPair.fromString(keys[0].secretKey).sign(createHash('sha256').update(transaction.encode()).digest()).signature;
  const signed=new SignedTransaction({transaction,signature:new Signature({keyType:0,data:sig})});
  const result=await rpc('send_tx',{signed_tx_base64:Buffer.from(signed.encode()).toString('base64'),wait_until:'FINAL'});
  assert.ok('SuccessValue'in result.status,'Official token WASM deploy/init failed');
 }
 const goArgs=['test','./tests/tokens','-run','^TestLocalNEP141TokenCycle$','-count=1','-v'];
 if(process.env.WALLET_NEAR_TOKEN_COVERAGE==='1') goArgs.push('-covermode=atomic','-coverpkg=localwallet/internal/chains/near,localwallet/internal/model,localwallet/internal/signer,localwallet/internal/vault,localwallet/internal/journal','-coverprofile='+resolve('reports/near-token-local.cover'));
 child=spawn('go',goArgs,{stdio:'inherit',env:{...process.env,WALLET_NEAR_TOKEN_LOCAL_RPC:sandbox.rpcUrl}});
 const exit=await new Promise(resolve=>{child.once('exit',resolve);child.once('error',()=>resolve(1));});assert.equal(exit,0,'Go token execution gate failed');
 const report=JSON.parse(readFileSync(resolve('reports/near-token-transactions.json'),'utf8'));assert.equal(report.status,'PASS');assert.equal(report.transactions.length,7);assert.equal(report.realSigner,true);assert.equal(report.passwordApprovedCalls,7);assert.equal(report.durableConfirmed,7);assert.equal(report.restartedUnknown,true);
 for(const record of report.transactions){
  const p=record.plan,call=p.payload.token,bytes=Buffer.from(record.signed.bytes,'base64');
  const signed=SignedTransaction.decode(bytes),transaction=signed.transaction,index=Number(p.account.index),contract=call.policy.asset.contract;
  assert.equal(transaction.signerId,owners[index]);assert.equal(transaction.receiverId,contract);assert.equal(transaction.actions.length,1);
  const expected=createTransaction(owners[index],PublicKey.fromString(keys[index].publicKey),contract,BigInt(p.nonce),[actionCreators.functionCall(call.method,Buffer.from(call.args),30000000000000n,BigInt(call.deposit))],bs58.decode(p.payload.blockHash));
  const hash=createHash('sha256').update(expected.encode()).digest(),sig=KeyPair.fromString(keys[index].secretKey).sign(hash).signature;
  const expectedSigned=new SignedTransaction({transaction:expected,signature:new Signature({keyType:0,data:sig})});
  assert.deepEqual(bytes,Buffer.from(expectedSigned.encode()));assert.equal(record.signed.identity,bs58.encode(hash));
  assert.equal(call.gas,'30000000000000');assert.equal(call.policy.asset.decimals,decimals[contracts.indexOf(contract)]);assert.equal(call.policy.codeHash,bs58.encode(createHash('sha256').update(readFileSync(wasm)).digest()));
  if(call.method==='ft_transfer') { assert.equal(call.deposit,'1');assert.deepEqual(JSON.parse(call.args),{receiver_id:p.recipient,amount:p.amount}); }
  else { assert.equal(call.method,'storage_deposit');assert.equal(call.deposit,p.amount);assert.deepEqual(JSON.parse(call.args),{account_id:owners[index],registration_only:true}); }
  const observed=await rpc('tx',{tx_hash:record.signed.identity,sender_account_id:owners[index],wait_until:'FINAL'});assert.equal(observed.final_execution_status,'FINAL');assert.ok('SuccessValue'in observed.status);
  assert.equal(observed.transaction.receiver_id,contract);assert.ok(observed.receipts_outcome.every(r=>'SuccessValue'in r.outcome.status||'SuccessReceiptId'in r.outcome.status));transactions++;
 }
 for(const contract of contracts)for(const [i,owner]of owners.entries()){
  const out=await rpc('query',{request_type:'call_function',finality:'final',account_id:contract,method_name:'ft_balance_of',args_base64:Buffer.from(JSON.stringify({account_id:owner})).toString('base64')});
  assert.equal(JSON.parse(Buffer.from(out.result).toString()),report.expected[contract][i]);
  const storage=await rpc('query',{request_type:'call_function',finality:'final',account_id:contract,method_name:'storage_balance_of',args_base64:Buffer.from(JSON.stringify({account_id:owner})).toString('base64')});assert.notEqual(JSON.parse(Buffer.from(storage.result).toString()),null);
 }
 assert.deepEqual(sourceIdentity(),source,'Product/test source changed during actual execution');assert.equal(checksum(new URL(import.meta.url)),producer,'Oracle producer changed during execution');
 status='PASS';console.log('PASS independent official SDK byte/signature reconstruction, seven mined Go token calls, two decimal precisions and exact final balances/registrations.');
}finally{
 if(child&&child.exitCode===null)child.kill('SIGKILL');if(sandbox)await sandbox.tearDown();process.removeListener('SIGINT',stop);process.removeListener('SIGTERM',stop);
 writeFileSync(resolve('reports/near-tokens-local.json'),JSON.stringify({status,producerSHA256:producer,sourceSHA256:source,sourceStable:JSON.stringify(sourceIdentity())===JSON.stringify(source)&&checksum(new URL(import.meta.url))===producer,network:'localnet',sandboxVersion:'2.13.4',contractSource:'near-examples/FT@6a96a120ad8537d292fd946b6bda6b139904c2c3',contractSHA256:checksum(wasm),decimals,transactions,seconds:(Date.now()-started)/1000,scope:'Actual official FT WASM: real Engine phrase import, password-approved policy imports and all seven calls, encrypted journal and fresh-signer UNKNOWN/reservation recovery; exact self storage registration, two-way 6/18 decimal token sends, exact events/all receipts, lost-ack original-only reconciliation, independent official SDK bytes/signatures and exact balances. Public synthetic fixture keys/passwords and loopback funds only.',notRun:['funded public network token transfers','mainnet spending','issuer/proxy audit','graphical token workflow']},null,2)+'\n');
}
