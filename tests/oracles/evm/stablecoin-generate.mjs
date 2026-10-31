// TEST ONLY: independent ethers vectors; documented public synthetic seed only.
import {readFileSync,writeFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {HDNodeWallet,Interface} from 'ethers';
const pkg=JSON.parse(readFileSync(new URL('./node_modules/ethers/package.json',import.meta.url)));
if(pkg.version!=='6.17.0')throw Error('Pinned independent oracle required');
const seed='0x'+Array.from({length:64},(_,i)=>i.toString(16).padStart(2,'0')).join('');
const tokens=[['ethereum-mainnet','1','0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48'],['arbitrum-mainnet','42161','0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9'],['arbitrum-mainnet','42161','0xaf88d065e77c8cC2239327C5EDb3A432268e5831'],['base-mainnet','8453','0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2'],['base-mainnet','8453','0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913'],['hyperevm-mainnet','999','0xb88339CB7199b77E23DB6E890353E22632Ba630f']];
const abi=new Interface(['function transfer(address,uint256) returns(bool)']);
const recipient='0x1111111111111111111111111111111111111111',amount='1234567',fixtures=[];
for(const [network,chainId,contract] of tokens)for(const index of [0,1,127]){
 const wallet=HDNodeWallet.fromSeed(seed).derivePath(`m/44'/60'/0'/0/${index}`),data=abi.encodeFunctionData('transfer',[recipient,amount]);
 const signed=await wallet.signTransaction({type:2,chainId:BigInt(chainId),nonce:7,to:contract,value:0,data,gasLimit:100000,maxFeePerGas:2000000001,maxPriorityFeePerGas:1});
 fixtures.push({network,chainId,contract,index,address:wallet.address,recipient,amount,decimals:6,signed});
}
const sha=x=>createHash('sha256').update(x).digest('hex');
writeFileSync(new URL('../../../internal/chains/evm/testdata/stablecoin-fixtures.json',import.meta.url),JSON.stringify({oracle:'ethers 6.17.0',profile:'evm-bip39-bip32-v1',producerSHA256:sha(readFileSync(new URL(import.meta.url))),lockSHA256:sha(readFileSync(new URL('./package-lock.json',import.meta.url))),fixtures},null,2)+'\n');
