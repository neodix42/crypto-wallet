// TEST ONLY: accept a public Go-signed synthetic transaction on an isolated
// in-process chain. No HTTP listener, public RPC, account API key or user secret.
import ganache from 'ganache';
import { readFileSync } from 'node:fs';
import { Interface, Transaction, AbiCoder, keccak256, toBeHex, zeroPadValue } from 'ethers';
const raw = readFileSync(0, 'utf8');
if (raw.length > 65536) throw Error('Bounded public fixture input required');
const body = JSON.parse(raw);
if (!body || Object.keys(body).sort().join(',') !== 'amount,recipient,signed,source' || Object.values(body).some(value => typeof value !== 'string')) throw Error('Exact public fixture schema required');
const contract = '0xdAC17F958D2ee523a2206206994597C13D831ec7';
const runtime = JSON.parse(readFileSync(new URL('./usdt-runtime.json', import.meta.url)));
if (runtime.contract !== contract || keccak256(runtime.code) !== '0xb44fb4e949d0f78f87f79ee46428f23a2a5713ce6fc6e0beb3dda78c2ac1ea55') throw Error('Pinned runtime mismatch');
const abi = new Interface(['function transfer(address,uint256)', 'function balanceOf(address) view returns(uint256)', 'event Transfer(address indexed from,address indexed to,uint256 value)']);
const tx = Transaction.from(body.signed), decoded = abi.decodeFunctionData('transfer', tx.data);
if (tx.type !== 2 || tx.chainId !== 1n || tx.nonce !== 7 || tx.to !== contract || tx.value !== 0n || tx.from !== body.source || tx.gasLimit !== 100000n || tx.maxFeePerGas !== 2000000001n || tx.maxPriorityFeePerGas !== 1n || decoded[0] !== body.recipient || decoded[1].toString() !== body.amount || body.amount !== '123456789') throw Error('Unexpected synthetic transaction effects');
const coder = AbiCoder.defaultAbiCoder();
const word = value => zeroPadValue(toBeHex(value), 32);
const balanceSlot = address => keccak256(coder.encode(['address', 'uint256'], [address, 2]));
const provider = ganache.provider({ chain: { chainId: 1, hardfork: 'shanghai' }, wallet: { totalAccounts: 0, deterministic: true }, logging: { quiet: true } });
const call = (method, params) => provider.request({ method, params });
const setStorage = (key, value) => call('evm_setAccountStorageAt', [contract, key, word(value)]);
const balance = async address => BigInt(await call('eth_call', [{ to: contract, data: abi.encodeFunctionData('balanceOf', [address]) }, 'latest']));
let result;
try {
  const nativeBefore = 1000n * 10n ** 18n, sourceBefore = 1000000000n;
  await call('evm_setAccountCode', [contract, runtime.code]);
  await call('evm_setAccountBalance', [body.source, toBeHex(nativeBefore)]);
  await call('evm_setAccountNonce', [body.source, '0x7']);
  await setStorage(word(9n), 6n); await setStorage(word(1n), sourceBefore); await setStorage(balanceSlot(body.source), sourceBefore);
  const simulation = await call('eth_call', [{ from: body.source, to: contract, data: tx.data }, 'latest']);
  if (simulation !== '0x') throw Error('Unexpected token simulation return');
  const identity = await call('eth_sendRawTransaction', [body.signed]);
  const receipt = await call('eth_getTransactionReceipt', [identity]);
  const sourceAfter = await balance(body.source), recipientAfter = await balance(body.recipient);
  const nativeAfter = BigInt(await call('eth_getBalance', [body.source, 'latest']));
  const nativeDebit = nativeBefore - nativeAfter, gasCharge = BigInt(receipt.gasUsed) * BigInt(receipt.effectiveGasPrice);
  if (identity !== keccak256(body.signed) || receipt.status !== '0x1' || sourceAfter !== sourceBefore - BigInt(body.amount) || recipientAfter !== BigInt(body.amount) || nativeDebit !== gasCharge || gasCharge <= 0n || gasCharge > tx.gasLimit * tx.maxFeePerGas || receipt.logs.length !== 1) throw Error('Actual Go-signed token execution mismatch');
  const event = abi.parseLog(receipt.logs[0]);
  if (event.args[0] !== body.source || event.args[1] !== body.recipient || event.args[2].toString() !== body.amount || receipt.logs[0].address.toLowerCase() !== contract.toLowerCase()) throw Error('Token event differs from Go-approved effects');
  await call('evm_mine', []); await call('evm_mine', []);
  const nonceAfter = BigInt(await call('eth_getTransactionCount', [body.source, 'latest']));
  if (await balance(body.recipient) !== recipientAfter || nonceAfter !== 8n) throw Error('A second payment appeared without authorization');
  result = { status: 'PASS', identity, source: body.source, recipient: body.recipient, amountAtomic: body.amount, sourceAfterAtomic: sourceAfter.toString(), recipientAfterAtomic: recipientAfter.toString(), nativeDebitAtomic: nativeDebit.toString(), gasChargeAtomic: gasCharge.toString(), gasUsed: BigInt(receipt.gasUsed).toString(), nonceAfter: nonceAfter.toString(), simulation, receiptStatus: receipt.status, transactionType: tx.type, chainId: tx.chainId.toString() };
} finally { await provider.disconnect(); }
console.log(JSON.stringify(result));
