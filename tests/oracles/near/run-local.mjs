// PUBLIC SYNTHETIC LOCALNET ONLY. Never connect this harness to a remote network.
import {Sandbox,GenesisAccount} from 'near-sandbox';
import {createRequire} from 'node:module';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdirSync,existsSync,readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {KeyPair,PublicKey} from '@near-js/crypto';
import {createTransaction,actionCreators,SignedTransaction,Signature} from '@near-js/transactions';
// The upstream binary downloader renames its temporary file; use the same filesystem.
mkdirSync(resolve('.tools/tmp'),{recursive:true});process.env.TMPDIR=resolve('.tools/tmp');
const binary=resolve('.tools/near/near-sandbox');
if(!existsSync(binary)||createHash('sha256').update(readFileSync(binary)).digest('hex')!=='4e89fbc0f2958634e84af44634f90e87c246288fa11b067d2121ce1394cba2c2')throw new Error('Install pinned NEAR sandbox using tests/oracles/near/install-sandbox.sh');
process.env.NEAR_SANDBOX_BIN_PATH=binary;
const require=createRequire(import.meta.url);
const {parseSeedPhrase}=require('near-seed-phrase');
const bs58=require('bs58');
const phrase='abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const keys=parseSeedPhrase(phrase);
const account=Buffer.from(bs58.decode(keys.publicKey.slice(8))).toString('hex');
let sandbox,child;
const stop=()=>{child?.kill('SIGINT');};
process.once('SIGINT',stop);process.once('SIGTERM',stop);
try{
 sandbox=await Sandbox.start({version:'2.13.4',config:{additionalGenesis:{chain_id:'localnet'},additionalAccounts:[new GenesisAccount(account,keys.publicKey,keys.secretKey,1000000000000000000000000000n),new GenesisAccount('receiver.testnet',keys.publicKey,keys.secretKey,10000000000000000000000000n),new GenesisAccount('revoked.testnet',keys.publicKey,keys.secretKey,10000000000000000000000000n),new GenesisAccount('restricted.testnet',keys.publicKey,keys.secretKey,10000000000000000000000000n)]}});
 const rpc=async(method,params)=>{const response=await fetch(sandbox.rpcUrl,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:'oracle',method,params})});const value=await response.json();if(value.error)throw new Error('Local NEAR setup RPC rejected');return value.result};
 const changeKeys=async(signerId,actions)=>{const state=await rpc('query',{request_type:'view_access_key',finality:'final',account_id:signerId,public_key:keys.publicKey});const transaction=createTransaction(signerId,PublicKey.fromString(keys.publicKey),signerId,BigInt(state.nonce)+1n,actions,bs58.decode(state.block_hash));const hash=createHash('sha256').update(transaction.encode()).digest();const sig=KeyPair.fromString(keys.secretKey).sign(hash).signature;const signed=new SignedTransaction({transaction,signature:new Signature({keyType:0,data:sig})});const result=await rpc('send_tx',{signed_tx_base64:Buffer.from(signed.encode()).toString('base64'),wait_until:'FINAL'});if(!('SuccessValue' in result.status))throw new Error('Local NEAR setup execution failed')};
 await changeKeys('revoked.testnet',[actionCreators.deleteKey(PublicKey.fromString(keys.publicKey))]);
 const restricted=parseSeedPhrase(phrase,"m/44'/397'/1'");
 await changeKeys('restricted.testnet',[actionCreators.addKey(PublicKey.fromString(restricted.publicKey),actionCreators.functionCallAccessKey('receiver.testnet',['allowed'])),actionCreators.deleteKey(PublicKey.fromString(keys.publicKey))]);
 child=spawn('go' ,['test','./internal/chains/near','-run','TestLocalNode','-v','-count=1'],{stdio:'inherit',env:{...process.env,WALLET_NEAR_LOCAL_RPC:sandbox.rpcUrl}});
 const code=await new Promise(resolve=>child.on('exit',resolve));
 process.exitCode=code??1;
} finally {if(child&&child.exitCode===null)child.kill('SIGKILL');if(sandbox)await sandbox.tearDown();process.removeListener('SIGINT',stop);process.removeListener('SIGTERM',stop);}
