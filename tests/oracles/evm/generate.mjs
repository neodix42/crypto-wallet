// TEST-ONLY PUBLIC SYNTHETIC VECTOR. Never run an oracle with a user's seed.
import { HDNodeWallet, Transaction, keccak256, Interface } from 'ethers';
import { writeFileSync } from 'node:fs';
const seed='0x'+Array.from({length:64},(_,i)=>i.toString(16).padStart(2,'0')).join('');
const fixtures=[];
const networkFixtures=[];
const tokenFixtures=[];
const arbitrumTokenFixtures=[];
for(const index of [0,1,127]){
 const wallet=HDNodeWallet.fromSeed(seed).derivePath(`m/44'/60'/0'/0/${index}`);
 const tx={type:2,chainId:11155111,nonce:7,to:'0x1111111111111111111111111111111111111111',value:123456789n,gasLimit:25200n,maxFeePerGas:2000000001n,maxPriorityFeePerGas:1n};
 const signed=await wallet.signTransaction(tx);
 fixtures.push({index,address:wallet.address,publicKey:wallet.signingKey.publicKey,unsigned:Transaction.from(tx).unsignedSerialized,signed,hash:keccak256(signed)});
 for(const [network,chainId] of [['ethereum-mainnet',1],['ethereum-sepolia',11155111],['hyperevm-mainnet',999],['hyperevm-testnet',998],['base-mainnet',8453],['base-sepolia',84532],['arbitrum-mainnet',42161],['arbitrum-sepolia',421614],['horizen-mainnet',26514],['horizen-testnet',2651420]]){
  const bytes=await wallet.signTransaction({...tx,chainId});networkFixtures.push({index,network,chainId,signed:bytes,hash:keccak256(bytes)});
 }
 for(const [network,chainId,contract] of [['base-mainnet',8453,'0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229'],['base-sepolia',84532,'0x107fdE93838e3404934877935993782F977324BB'],['horizen-mainnet',26514,'0x57da2D504bf8b83Ef304759d9f2648522D7a9280'],['horizen-testnet',2651420,'0xb06EC4ce262D8dbDc24Fac87479A49A7DC4cFb87']]){
  const data=new Interface(['function transfer(address,uint256) returns (bool)']).encodeFunctionData('transfer',[tx.to,tx.value]);const bytes=await wallet.signTransaction({...tx,chainId,to:contract,value:0n,gasLimit:60000n,data});tokenFixtures.push({index,network,chainId,contract,data,signed:bytes,hash:keccak256(bytes)});
 }
 for(const [network,chainId,contract] of [['arbitrum-mainnet',42161,'0x912CE59144191C1204E64559FE8253a0e49E6548'],['arbitrum-sepolia',421614,'0x1234567890123456789012345678901234567890']]){
  const data=new Interface(['function transfer(address,uint256) returns (bool)']).encodeFunctionData('transfer',[tx.to,tx.value]); const bytes=await wallet.signTransaction({...tx,chainId,to:contract,value:0n,gasLimit:60000n,data}); arbitrumTokenFixtures.push({index,network,chainId,contract,data,signed:bytes,hash:keccak256(bytes)});
 }
}
writeFileSync(new URL('./fixtures.json',import.meta.url),JSON.stringify({oracle:'ethers 6.17.0',seed,profile:'evm-bip39-bip32-v1',fixtures,networkFixtures,tokenFixtures,arbitrumTokenFixtures},null,2)+'\n');
