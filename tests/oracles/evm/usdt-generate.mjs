// TEST ONLY. Public synthetic seed and isolated in-process Ganache; no external RPC.
import ganache from 'ganache';
import { HDNodeWallet, Interface, AbiCoder, keccak256, toBeHex, zeroPadValue, Transaction } from 'ethers';
import { readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
const runtimePath = new URL('./usdt-runtime.json', import.meta.url);
const runtimeBytes = readFileSync(runtimePath);
const runtime = JSON.parse(runtimeBytes);
const contract = '0xdAC17F958D2ee523a2206206994597C13D831ec7';
const expectedCodeHash = '0xb44fb4e949d0f78f87f79ee46428f23a2a5713ce6fc6e0beb3dda78c2ac1ea55';
if (runtime.contract !== contract || runtime.decimals !== 6 || runtime.chainId !== '1' || keccak256(runtime.code) !== expectedCodeHash) throw Error('Pinned USDT runtime identity mismatch');
const seed = '0x' + Array.from({ length: 64 }, (_, i) => i.toString(16).padStart(2, '0')).join('');
const wallets = [0, 1, 127].map(index => ({ index, wallet: HDNodeWallet.fromSeed(seed).derivePath(`m/44'/60'/0'/0/${index}`) }));
const provider = ganache.provider({ chain: { chainId: 1, hardfork: 'shanghai' }, wallet: { accounts: wallets.map(({wallet}) => ({ secretKey: wallet.privateKey, balance: toBeHex(1000n * 10n ** 18n) })) }, logging: { quiet: true } });
const abi = new Interface(['function transfer(address,uint256)', 'function balanceOf(address) view returns(uint256)', 'function decimals() view returns(uint256)', 'event Transfer(address indexed from,address indexed to,uint256 value)']);
const coder = AbiCoder.defaultAbiCoder();
const storageWord = n => zeroPadValue(toBeHex(n), 32);
const slot = n => storageWord(BigInt(n));
const balanceSlot = address => keccak256(coder.encode(['address', 'uint256'], [address, 2]));
const request = (method, params) => provider.request({ method, params });
const setStorage = (key, value) => request('evm_setAccountStorageAt', [contract, key, storageWord(value)]);
const fixtures = [];
try {
  await request('evm_setAccountCode', [contract, runtime.code]);
  await setStorage(slot(9), 6n); // Pinned TetherToken's decimals storage slot.
  for (const { index, wallet } of wallets) {
    const recipient = '0x1111111111111111111111111111111111111111';
    const amount = 123456789n, initial = 1000000000n;
    await setStorage(slot(1), initial);
    await setStorage(balanceSlot(wallet.address), initial);
    await setStorage(balanceSlot(recipient), 0n);
    await request('evm_setAccountNonce', [wallet.address, '0x7']);
    const data = abi.encodeFunctionData('transfer', [recipient, amount]);
    const tx = { type: 2, chainId: 1, nonce: 7, to: contract, value: 0n, gasLimit: 100000n, maxFeePerGas: 2000000001n, maxPriorityFeePerGas: 1n, data };
    const signed = await wallet.signTransaction(tx);
    const simulation = await request('eth_call', [{ from: wallet.address, to: contract, data }, 'latest']);
    if (simulation !== '0x') throw Error('Pinned USDT transfer unexpectedly returns data');
    const identity = await request('eth_sendRawTransaction', [signed]);
    const receipt = await request('eth_getTransactionReceipt', [identity]);
    const readBalance = async address => BigInt(await request('eth_call', [{ to: contract, data: abi.encodeFunctionData('balanceOf', [address]) }, 'latest']));
    const sourceAfter = await readBalance(wallet.address), recipientAfter = await readBalance(recipient);
    if (identity !== keccak256(signed) || receipt.status !== '0x1' || sourceAfter !== initial - amount || recipientAfter !== amount || receipt.logs.length !== 1) throw Error('Actual pinned USDT execution mismatch ' + JSON.stringify({ index, identityMatches: identity === keccak256(signed), status: receipt.status, sourceAfter: sourceAfter.toString(), recipientAfter: recipientAfter.toString(), logs: receipt.logs.length }));
    const event = abi.parseLog(receipt.logs[0]);
    if (event.name !== 'Transfer' || event.args[0] !== wallet.address || event.args[1] !== recipient || event.args[2] !== amount) throw Error('USDT receipt effect mismatch');
    fixtures.push({ index, address: wallet.address, publicKey: wallet.signingKey.publicKey, chainId: '1', contract, recipient, decimals: 6, amountAtomic: amount.toString(), amountDecimal: '123.456789', data, unsigned: Transaction.from(tx).unsignedSerialized, signed, hash: identity, initialAtomic: initial.toString(), sourceAfterAtomic: sourceAfter.toString(), recipientAfterAtomic: recipientAfter.toString(), sourceStorageKey: balanceSlot(wallet.address), recipientStorageKey: balanceSlot(recipient), simulation, receipt: { status: receipt.status, gasUsed: receipt.gasUsed, log: { address: receipt.logs[0].address, topics: receipt.logs[0].topics, data: receipt.logs[0].data } } });
  }
} finally { await provider.disconnect(); }
const sha = data => createHash('sha256').update(data).digest('hex');
writeFileSync(new URL('./usdt-fixtures.json', import.meta.url), JSON.stringify({ schemaVersion: 1, oracle: 'ethers 6.17.0 + Ganache 7.9.2 actual pinned USDT runtime execution', scope: 'Isolated in-process synthetic Ethereum-shaped EVM; no Bybit, public RPC, mainnet funds or consensus/finality claim.', generator: 'tests/oracles/evm/usdt-generate.mjs', generatorSHA256: sha(readFileSync(new URL(import.meta.url))), runtimeSource: 'tests/oracles/evm/usdt-runtime.json', runtimeSourceSHA256: sha(runtimeBytes), runtimeKeccak256: expectedCodeHash, packageLockSHA256: sha(readFileSync(new URL('./package-lock.json', import.meta.url))), seed, profile: 'evm-bip39-bip32-v1', fixtures }, null, 2) + '\n');
console.log('PASS 3 independently signed USDT transactions executed against pinned deployed runtime on isolated Ganache; no external RPC.');
