// TEST ONLY: independent in-process EVM node, loopback and public fixture keys.
import ganache from 'ganache';
import solc from 'solc';
import { readFileSync } from 'node:fs';
import { HDNodeWallet, AbiCoder, keccak256, toBeHex, zeroPadValue } from 'ethers';
const seed='0x'+Array.from({length:64},(_,i)=>i.toString(16).padStart(2,'0')).join('');
const wallet=HDNodeWallet.fromSeed(seed).derivePath("m/44'/60'/0'/0/0");
const chainId=Number(process.env.LOCAL_CHAIN_ID??11155111);
const port=Number(process.env.LOCAL_NODE_PORT??18545);
if(![11155111,84532,2651420,998].includes(chainId))throw Error('Testnets only');
const server=ganache.server({chain:{chainId,hardfork:'shanghai'},miner:{blockTime:1},wallet:{accounts:[{secretKey:wallet.privateKey,balance:toBeHex(1000n*10n**18n)}]},logging:{quiet:true}});
await server.listen(port,'127.0.0.1');
if([84532,2651420].includes(chainId)){
 const source=`pragma solidity ^0.8.30;
 contract Token { mapping(address=>uint256) public balanceOf; uint8 public constant decimals=18; event Transfer(address indexed from,address indexed to,uint256 value); function transfer(address to,uint256 n) external returns(bool){require(balanceOf[msg.sender]>=n,"balance");balanceOf[msg.sender]-=n;balanceOf[to]+=n;emit Transfer(msg.sender,to,n);return true;} }
 contract Oracle {function getL1FeeUpperBound(uint256) external pure returns(uint256){return 1000;} function getOperatorFee(uint256) external pure returns(uint256){return 100;} }
 `;
 const out=JSON.parse(solc.compile(JSON.stringify({language:'Solidity',sources:{'Fixture.sol':{content:source}},settings:{evmVersion:'shanghai',outputSelection:{'*':{'*':['evm.deployedBytecode.object']}}}})));
 if(out.errors?.some(x=>x.severity==='error'))throw Error('Solidity compile failed');
 const token=chainId===84532?'0x107fdE93838e3404934877935993782F977324BB':'0xb06EC4ce262D8dbDc24Fac87479A49A7DC4cFb87';
 const oracle='0x420000000000000000000000000000000000000F';
 const deployed=JSON.parse(readFileSync(new URL('./deployed-tokens.json',import.meta.url)));
 const officialCode=deployed.networks.find(n=>n.network===(chainId===84532?'base-sepolia':'horizen-testnet')).code;
 await server.provider.request({method:'evm_setAccountCode',params:[token,officialCode]});
 await server.provider.request({method:'evm_setAccountCode',params:[oracle,'0x'+out.contracts['Fixture.sol'].Oracle.evm.deployedBytecode.object]});
 const key=keccak256(AbiCoder.defaultAbiCoder().encode(['address','uint256'],[wallet.address,chainId===84532?0:5]));
 await server.provider.request({method:'evm_setAccountStorageAt',params:[token,key,zeroPadValue(toBeHex(1000n*10n**18n),32)]});
}
console.log(JSON.stringify({node:'ganache 7.9.2',chainId,port,account:wallet.address}));
for(const signal of ['SIGTERM','SIGINT'])process.on(signal,async()=>{await server.close();process.exit(0)});
