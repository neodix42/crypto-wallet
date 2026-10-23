// PUBLIC SYNTHETIC VECTOR ONLY. This generator must never receive user material.
import {writeFileSync} from 'node:fs';
import {Account,AccountAddress,EntryFunction,TransactionPayloadEntryFunction,RawTransaction,ChainId,U64,SimpleTransaction,generateSigningMessageForTransaction,generateSignedTransaction,generateUserTransactionHash} from '@aptos-labs/ts-sdk';
const mnemonic='abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const vectors=[];
for(const index of [0,1,1234]){
 const path=`m/44'/637'/${index}'/0'/0'`;
 const account=Account.fromDerivationPath({path,mnemonic,legacy:true});
 const recipient=AccountAddress.from('0x'+'cafe'.padStart(64,'0'));
 const payload=new TransactionPayloadEntryFunction(EntryFunction.build('0x1::aptos_account','transfer',[],[recipient,new U64(123456789n)]));
 const raw=new RawTransaction(account.accountAddress,42n,payload,2000n,100n,1900000000n,new ChainId(2));
 const transaction=new SimpleTransaction(raw);
 const message=generateSigningMessageForTransaction(transaction);
 const auth=account.signTransactionWithAuthenticator(transaction);
 const signed=generateSignedTransaction({transaction,senderAuthenticator:auth});
 vectors.push({index,path,address:account.accountAddress.toStringLong(),publicKey:account.publicKey.toString().replace(/^0x/,''),recipient:recipient.toStringLong(),amount:'123456789',nonce:'42',maxGas:'2000',gasPrice:'100',expires:1900000000,chainId:2,raw:Buffer.from(raw.bcsToBytes()).toString('hex'),message:Buffer.from(message).toString('hex'),signature:auth.signature.toString().replace(/^0x/,''),signed:Buffer.from(signed).toString('hex'),hash:generateUserTransactionHash({transaction,senderAuthenticator:auth})});
}
writeFileSync(new URL('./fixtures.json',import.meta.url),JSON.stringify({oracle:{'@aptos-labs/ts-sdk':'7.5.0'},mnemonic,passphrase:'',vectors},null,2)+'\n');
console.log('Wrote 3 public Aptos fixtures.');
