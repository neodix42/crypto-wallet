// Public BIP39 vectors only; no input, user material or network requests.
import {createRequire} from 'node:module';
import {createHash} from 'node:crypto';
import {writeFileSync} from 'node:fs';
import {KeyPair,PublicKey} from '@near-js/crypto';
import {createTransaction,actionCreators,SignedTransaction,Signature} from '@near-js/transactions';
const require=createRequire(import.meta.url),{parseSeedPhrase}=require('near-seed-phrase'),bs58=require('bs58');
const mnemonic='abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about',vectors=[];
for(const index of [0,1,1234]) {
 const path=`m/44'/397'/${index}'`,keys=parseSeedPhrase(mnemonic,path),key=KeyPair.fromString(keys.secretKey);
 const address=Buffer.from(bs58.decode(keys.publicKey.slice(8))).toString('hex');
 for(const decimals of [0,6,18]) for(const method of ['ft_transfer','storage_deposit']) {
  const contract=`oracle${decimals}.testnet`,recipient=method==='ft_transfer'?'receiver.testnet':address;
  const amount=method==='ft_transfer'?'123456789012345678901234':'1250000000000000000000';
  const args=method==='ft_transfer'?{receiver_id:recipient,amount}:{account_id:address,registration_only:true};
  const deposit=method==='ft_transfer'?'1':amount,gas='30000000000000',block=Uint8Array.from({length:32},(_,i)=>i);
  const transaction=createTransaction(address,PublicKey.fromString(keys.publicKey),contract,42n,[actionCreators.functionCall(method,args,BigInt(gas),BigInt(deposit))],block);
  const raw=transaction.encode(),hash=createHash('sha256').update(raw).digest(),signature=key.sign(hash).signature;
  const signed=new SignedTransaction({transaction,signature:new Signature({keyType:0,data:signature})});
  vectors.push({index,path,address,publicKey:keys.publicKey,contract,recipient,decimals,method,args:JSON.stringify(args),amount,deposit,gas,nonce:'42',blockHash:bs58.encode(block),codeHash:bs58.encode(Buffer.alloc(32,7)),raw:Buffer.from(raw).toString('hex'),hash:bs58.encode(hash),signature:Buffer.from(signature).toString('hex'),signed:Buffer.from(signed.encode()).toString('hex')});
 }
}
writeFileSync(new URL('./token-fixtures.json',import.meta.url),JSON.stringify({oracle:{'@near-js/transactions':'2.5.1','@near-js/crypto':'2.5.1','near-seed-phrase':'0.2.1',sourceCommit:'345de7c326bc2da93dc0625ace8d123fc3042b17'},mnemonic,passphrase:'',vectors},null,2)+'\n');
console.log('Wrote18 public standard-token FunctionCall Borsh/signature vectors.');
