// TEST ONLY. Actual pinned proxy/implementation runtime on owned isolated EVMs.
// Public synthetic keys/storage only; no public RPC, bridge, issuer or consensus.
import ganache from 'ganache';
import solc from 'solc';
import {readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {createInterface} from 'node:readline';
import {HDNodeWallet,Interface,AbiCoder,Transaction,toBeHex,zeroPadValue,keccak256,id,getAddress} from 'ethers';
for(const [name,version] of Object.entries({ethers:'6.17.0',ganache:'7.9.2',solc:'0.8.30'}))if(JSON.parse(readFileSync(new URL(`./node_modules/${name}/package.json`,import.meta.url))).version!==version)throw Error('Pinned oracle dependency required');
const networks={'ethereum-mainnet':'1','base-mainnet':'8453','arbitrum-mainnet':'42161','hyperevm-mainnet':'999'},network=process.argv[2];
if(process.argv.length!==3 || !networks[network])throw Error('Exact owned local profile required');
const chainId=networks[network],all=JSON.parse(readFileSync(new URL('../../../internal/chains/evm/testdata/stablecoin-runtime.json',import.meta.url))),tokens=all.tokens.filter(token=>token.network===network);
const seed='0x'+Array.from({length:64},(_,i)=>i.toString(16).padStart(2,'0')).join(''),accounts=[0,1,2].map(index=>HDNodeWallet.fromSeed(seed).derivePath(`m/44'/60'/0'/0/${index}`));
const server=ganache.server({chain:{chainId:Number(chainId),hardfork:'shanghai'},wallet:{accounts:accounts.map(wallet=>({secretKey:wallet.privateKey,balance:toBeHex(1000n*10n**18n)}))},logging:{quiet:true}});
const rpc=(method,params)=>server.provider.request({method,params}),word=value=>zeroPadValue(toBeHex(value),32),coder=AbiCoder.defaultAbiCoder();
const abi=new Interface(['function initialize(string,string,string,uint8,address,address,address,address)','function configureMinter(address,uint256)','function mint(address,uint256)','function transfer(address,uint256) returns(bool)','function balanceOf(address) view returns(uint256)','function symbol() view returns(string)','function decimals() view returns(uint8)','event Transfer(address indexed from,address indexed to,uint256 value)']);
const call=async(token,method,args=[])=>abi.decodeFunctionResult(method,await rpc('eth_call',[{to:token.contract,data:abi.encodeFunctionData(method,args)},'latest']))[0];
const set=async(token,slot,value)=>rpc('evm_setAccountStorageAt',[token.contract,typeof slot==='number'?word(slot):slot,value]);
const shortString=value=>{const bytes=Buffer.from(value);if(bytes.length>31)throw Error('Bounded synthetic short string required');const out=Buffer.alloc(32);bytes.copy(out);out[31]=bytes.length*2;return '0x'+out.toString('hex')};
const write=value=>process.stdout.write(JSON.stringify(value)+'\n'),assert=(ok,message)=>{if(!ok)throw Error(message)},same=(a,b)=>getAddress(a)===getAddress(b);
let closed=false;const close=async()=>{if(closed)return;closed=true;await server.close()};for(const signal of ['SIGINT','SIGTERM'])process.on(signal,()=>close().finally(()=>process.exit(0)));
try{
 await server.listen(0,'127.0.0.1');
 for(const token of tokens){assert(keccak256(token.code)===token.codeHash,'Public runtime fixture hash mismatch');await rpc('evm_setAccountCode',[token.contract,token.code]);
  for(const proxy of token.proxies){assert(keccak256(proxy.code)===proxy.codeHash,'Implementation fixture hash mismatch');await rpc('evm_setAccountCode',[proxy.implementation,proxy.code]);await set(token,proxy.slot,proxy.value)}
  // Synthetic nonzero proxy admins keep ordinary zero-from read calls out of
  // transparent-proxy admin dispatch; no production authority is reproduced.
  await set(token,id('org.zeppelinos.proxy.admin'),zeroPadValue('0x9999999999999999999999999999999999999999',32));
  await set(token,'0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103',zeroPadValue('0x9999999999999999999999999999999999999999',32));
  if(token.contract==='0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9'){
   // Public synthetic OpenZeppelin-upgradeable ERC20 storage, independently
   // verified through actual deployed implementation calls before acceptance.
   await set(token,54,shortString('USD₮0'));await set(token,55,shortString('USD₮0'));await set(token,256,word(6));await set(token,53,word(1000000000));
   await set(token,keccak256(coder.encode(['address','uint256'],[accounts[0].address,51])),word(1000000000));
  }else if(token.contract==='0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2'){
   for(const [slot,value]of [[2,word(1000000000)],[3,shortString('Tether USD')],[4,shortString('USDT')],[5,word((BigInt('0xdAC17F958D2ee523a2206206994597C13D831ec7')<<8n)|6n)],[6,zeroPadValue('0x4200000000000000000000000000000000000010',32)]])await set(token,slot,value);
   await set(token,keccak256(coder.encode(['address','uint256'],[accounts[0].address,0])),word(1000000000));
  }else{
   for(const [method,args]of [['initialize',['USD Coin','USDC','USD',6,accounts[2].address,accounts[2].address,accounts[2].address,accounts[2].address]],['configureMinter',[accounts[2].address,1000000000]],['mint',[accounts[0].address,1000000000]]]){
    const hash=await rpc('eth_sendTransaction',[{from:accounts[2].address,to:token.contract,data:abi.encodeFunctionData(method,args),gas:'0x200000'}]);assert((await rpc('eth_getTransactionReceipt',[hash])).status==='0x1','Synthetic issuer fixture setup failed');
   }
  }
  if(token.contract==='0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2')for(const [signature,address]of [['l1Token()','0xdac17f958d2ee523a2206206994597c13d831ec7'],['l2Bridge()','0x4200000000000000000000000000000000000010']])assert(await rpc('eth_call',[{to:token.contract,data:id(signature).slice(0,10)},'latest'])===zeroPadValue(address,32),'Synthetic bridge identity differs from production canonical pair');
  assert((await call(token,'decimals'))===6n && (await call(token,'symbol'))===(token.contract==='0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9'?'USD₮0':token.contract==='0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2'?'USDT':'USDC'),'Actual token metadata differs from pinned identity');
  assert((await call(token,'balanceOf',[accounts[0].address]))===1000000000n && (await call(token,'balanceOf',[accounts[1].address]))===0n,'Synthetic fixture balances not independently verified');
 }
 if(network==='base-mainnet'){
  const source='pragma solidity 0.8.30; contract FeeFixture { function getL1FeeUpperBound(uint256 n) external pure returns(uint256){return 1000+n*7;} function getOperatorFee(uint256 n) external pure returns(uint256){return 50+n*3;} }';
  const build=JSON.parse(solc.compile(JSON.stringify({language:'Solidity',sources:{'Fee.sol':{content:source}},settings:{evmVersion:'shanghai',outputSelection:{'*':{'*':['evm.deployedBytecode.object']}}}})));
  await rpc('evm_setAccountCode',['0x420000000000000000000000000000000000000F','0x'+build.contracts['Fee.sol'].FeeFixture.evm.deployedBytecode.object]);
 }
 const snapshot=async()=>({native:await Promise.all(accounts.slice(0,2).map(async a=>BigInt(await rpc('eth_getBalance',[a.address,'latest'])).toString())),nonces:await Promise.all(accounts.slice(0,2).map(async a=>BigInt(await rpc('eth_getTransactionCount',[a.address,'latest'])).toString())),balances:await Promise.all(tokens.map(token=>Promise.all(accounts.slice(0,2).map(async a=>(await call(token,'balanceOf',[a.address])).toString()))))});
 write({status:'READY',endpoint:`http://127.0.0.1:${server.address().port}`,network,chainId,tokens:tokens.map(t=>t.contract),accounts:accounts.slice(0,2).map(a=>a.address),producerSHA256:createHash('sha256').update(readFileSync(new URL(import.meta.url))).digest('hex')});
 let before;const transfers=[];const input=createInterface({input:process.stdin,crlfDelay:Infinity});for await(const line of input){assert(line.length<=65536,'Bounded command required');const command=JSON.parse(line);
  if(command.op==='observe'){before=await snapshot();write({status:'OBSERVED',...before});continue}
  if(command.op==='verify'){
   assert(before && Number.isInteger(command.token) && tokens[command.token] && [0,1].includes(command.from),'Bound owned transfer required');const token=tokens[command.token],from=accounts[command.from],to=accounts[1-command.from],tx=Transaction.from(command.raw),receipt=await rpc('eth_getTransactionReceipt',[command.identity]);
   assert(tx.chainId===BigInt(chainId) && tx.type===2 && tx.hash===command.identity && same(tx.from,from.address) && same(tx.to,token.contract) && tx.value===0n && tx.data===abi.encodeFunctionData('transfer',[to.address,BigInt(command.amount)]) && tx.nonce===Number(before.nonces[command.from]),'Independent bytes/domain/nonce check failed');
   assert(receipt?.status==='0x1' && receipt.logs.length===1,'Actual transfer execution/effects failed');const event=abi.parseLog(receipt.logs[0]);assert(same(receipt.logs[0].address,token.contract) && event.name==='Transfer' && same(event.args[0],from.address) && same(event.args[1],to.address) && event.args[2]===BigInt(command.amount),'Independent receipt effect mismatch');
   const after=await snapshot(),fee=BigInt(receipt.gasUsed)*BigInt(receipt.effectiveGasPrice);assert(fee>0n && fee<=BigInt(command.maxFee),'Native fee exceeds approved reserve');
   for(let a=0;a<2;a++){assert(BigInt(before.native[a])-BigInt(after.native[a])===(a===command.from?fee:0n),'Wrong native gas debit');for(let j=0;j<tokens.length;j++)assert(BigInt(after.balances[j][a])-BigInt(before.balances[j][a])===(j!==command.token?0n:a===command.from?-BigInt(command.amount):BigInt(command.amount)),'Unexpected token delta');}
   transfers.push(command.identity);write({status:'PASS',identity:tx.hash,contract:token.contract,amount:command.amount,fee:fee.toString(),source:from.address,destination:to.address});continue
  }
  if(command.op==='finish'){assert(transfers.length===tokens.length*2 && new Set(transfers).size===transfers.length,'Missing/duplicate approved transfers');const final=await snapshot();for(let j=0;j<tokens.length;j++)assert(final.balances[j][0]==='916543212' && final.balances[j][1]==='83456788','Out/back balance mismatch');for(const nonce of final.nonces)assert(nonce===String(tokens.length),'Unexpected additional payment');write({status:'PASS',transfers:transfers.length,network,chainId,rollupFees:network==='base-mainnet'?'DETERMINISTIC_FIXTURE_NOT_CHARGED':'NOT_EMULATED_NETWORK_ECONOMICS',...final});input.close();break}
  throw Error('Unknown command');
 }
}catch{process.stderr.write('Canonical stablecoin local execution failed; no acceptance claim.\n');process.exitCode=1}finally{await close()}
