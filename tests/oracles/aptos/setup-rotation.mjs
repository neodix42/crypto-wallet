// PUBLIC SYNTHETIC LOCALNET ONLY. Independent SDK builds a proven rotation.
import {Aptos,AptosConfig,Network,Account} from '@aptos-labs/ts-sdk';
const phrase='abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const from=Account.fromDerivationPath({path:"m/44'/637'/10'/0'/0'",mnemonic:phrase,legacy:true});
const to=Account.fromDerivationPath({path:"m/44'/637'/3'/0'/0'",mnemonic:phrase,legacy:true});
const aptos=new Aptos(new AptosConfig({network:Network.LOCAL,fullnode:'http://127.0.0.1:8080/v1',faucet:'http://127.0.0.1:8081'}));
const result=await fetch(`http://127.0.0.1:8081/mint?address=${from.accountAddress.toStringLong()}&amount=1000000000`,{method:'POST'});
if(!result.ok)throw new Error('Local faucet unavailable');
for(const hash of await result.json())await aptos.waitForTransaction({transactionHash:hash});
const transaction=await aptos.rotateAuthKey({fromAccount:from,toAccount:to});
const pending=await aptos.signAndSubmitTransaction({signer:from,transaction});
await aptos.waitForTransaction({transactionHash:pending.hash});
console.log('Local validator committed proven Legacy Ed25519 rotation using independent TS SDK.');
