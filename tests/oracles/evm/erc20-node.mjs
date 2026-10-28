// TEST ONLY. Own loopback Sepolia-shaped EVM and committed public synthetic
// keys. This fixture never reads user profiles, secrets or remote endpoints.
import ganache from 'ganache';
import solc from 'solc';
import { createInterface } from 'node:readline';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { ContractFactory, HDNodeWallet, Interface, JsonRpcProvider, NonceManager, Transaction, getAddress, keccak256, toBeHex, zeroPadValue } from 'ethers';

const packages = { ethers: '6.17.0', ganache: '7.9.2', solc: '0.8.30' };
for (const [name, version] of Object.entries(packages)) {
  const metadata = JSON.parse(readFileSync(new URL(`./node_modules/${name}/package.json`, import.meta.url)));
  if (metadata.version !== version) throw Error('Pinned local ERC20 dependency identity mismatch');
}
const seed = '0x' + Array.from({ length: 64 }, (_, i) => i.toString(16).padStart(2, '0')).join('');
const accounts = [0, 1, 2].map(index => HDNodeWallet.fromSeed(seed).derivePath(`m/44'/60'/0'/0/${index}`));
const server = ganache.server({
  chain: { chainId: 11155111, hardfork: 'shanghai' },
  wallet: { accounts: accounts.map(wallet => ({ secretKey: wallet.privateKey, balance: toBeHex(1000n * 10n ** 18n) })) },
  logging: { quiet: true },
});
const rpc = (method, params) => server.provider.request({ method, params });
const source = `pragma solidity 0.8.30;
contract PublicFixtureToken {
  string public name;
  string public symbol;
  uint8 public decimals;
  uint256 public totalSupply;
  mapping(address => uint256) public balanceOf;
  bool public rejectTransfer;
  event Transfer(address indexed from, address indexed to, uint256 value);
  constructor(string memory ticker, uint8 precision, address initialHolder) {
    name = string.concat("Public fixture ", ticker); symbol = ticker; decimals = precision;
    totalSupply = 1000 * 10 ** uint256(precision); balanceOf[initialHolder] = totalSupply;
    emit Transfer(address(0), initialHolder, totalSupply);
  }
  function transfer(address recipient, uint256 amount) external returns (bool) {
    if (rejectTransfer) return false;
    require(recipient != address(0) && balanceOf[msg.sender] >= amount, "fixture balance");
    balanceOf[msg.sender] -= amount; balanceOf[recipient] += amount;
    emit Transfer(msg.sender, recipient, amount); return true;
  }
}`;
const compiled = JSON.parse(solc.compile(JSON.stringify({ language: 'Solidity', sources: { 'PublicFixture.sol': { content: source } },
  settings: { evmVersion: 'shanghai', outputSelection: { '*': { '*': ['abi', 'storageLayout', 'evm.bytecode.object'] } } } })));
if (compiled.errors?.some(error => error.severity === 'error')) throw Error('Local ERC20 Solidity fixture compilation failed');
const artifact = compiled.contracts['PublicFixture.sol'].PublicFixtureToken;
const abi = new Interface(artifact.abi);
const slot = label => artifact.storageLayout.storage.find(item => item.label === label).slot;
const write = value => process.stdout.write(JSON.stringify(value) + '\n');
const equalAddress = (a, b) => getAddress(a) === getAddress(b);
const assert = (condition, message) => { if (!condition) throw Error(message); };
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
let provider;
let finished = false;
const close = async () => { if (finished) return; finished = true; provider?.destroy(); await server.close(); };
for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => { close().finally(() => process.exit(0)); });

try {
  await server.listen(0, '127.0.0.1');
  const endpoint = `http://127.0.0.1:${server.address().port}`;
  provider = new JsonRpcProvider(endpoint, 11155111, { staticNetwork: true, cacheTimeout: -1, batchMaxCount: 1 });
  const deployer = new NonceManager(accounts[2].connect(provider));
  const factory = new ContractFactory(artifact.abi, artifact.evm.bytecode.object, deployer);
  const tokens = [];
  const deployments = [];
  for (const [symbol, decimals] of [['SIX', 6], ['EIGHTEEN', 18]]) {
    const contract = await factory.deploy(symbol, decimals, accounts[0].address);
    const deployment = await contract.deploymentTransaction().wait();
    assert(deployment.status === 1, 'Actual fixture deployment failed');
    const address = await contract.getAddress();
    const code = await rpc('eth_getCode', [address, 'latest']);
    tokens.push({ contract: address, symbol, decimals, codeHash: keccak256(code), code, initialAtomic: (1000n * 10n ** BigInt(decimals)).toString() });
    deployments.push(deployment.hash);
  }
  const balance = async (token, account) => abi.decodeFunctionResult('balanceOf', await rpc('eth_call', [{ to: token.contract, data: abi.encodeFunctionData('balanceOf', [account.address]) }, 'latest']))[0].toString();
  const snapshot = async () => ({
    native: await Promise.all(accounts.slice(0, 2).map(async account => BigInt(await rpc('eth_getBalance', [account.address, 'latest'])).toString())),
    nonces: await Promise.all(accounts.slice(0, 2).map(async account => BigInt(await rpc('eth_getTransactionCount', [account.address, 'latest'])).toString())),
    balances: await Promise.all(tokens.map(token => Promise.all(accounts.slice(0, 2).map(account => balance(token, account))))),
  });
  write({ status: 'READY', endpoint, chainId: '11155111', producerSHA256: sha(readFileSync(new URL(import.meta.url))),
    compilerVersion: solc.version(), packages, accounts: accounts.slice(0, 2).map(wallet => ({ address: wallet.address, publicKey: wallet.signingKey.publicKey })),
    tokens: tokens.map(({ code, ...token }) => token) });
  const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
  let before;
  const transfers = [];
  const faults = [];
  for await (const line of input) {
    assert(line.length <= 65536, 'Oversized local oracle command');
    const command = JSON.parse(line);
    if (command.op === 'observe') {
      before = await snapshot(); write({ status: 'OBSERVED', ...before }); continue;
    }
    if (command.op === 'fault') {
      assert(['none', 'decimals', 'false-return', 'missing-code'].includes(command.kind), 'Unrecognized local negative fixture');
      const token = tokens[0];
      await rpc('evm_setAccountCode', [token.contract, command.kind === 'missing-code' ? '0x00' : token.code]);
      await rpc('evm_setAccountStorageAt', [token.contract, zeroPadValue(toBeHex(BigInt(slot('decimals'))), 32), zeroPadValue(toBeHex(command.kind === 'decimals' ? 18 : 6), 32)]);
      await rpc('evm_setAccountStorageAt', [token.contract, zeroPadValue(toBeHex(BigInt(slot('rejectTransfer'))), 32), zeroPadValue(toBeHex(command.kind === 'false-return' ? 1 : 0), 32)]);
      await rpc('evm_mine', []);
      faults.push(command.kind); write({ status: 'FAULT_SET', kind: command.kind }); continue;
    }
    if (command.op === 'verify') {
      assert(before && [0, 1].includes(command.token) && [0, 1].includes(command.from) && command.to === 1 - command.from, 'Unbound actual-transfer assertion');
      const token = tokens[command.token], from = accounts[command.from], to = accounts[command.to];
      const decoded = Transaction.from(command.raw);
      const transaction = await rpc('eth_getTransactionByHash', [command.identity]);
      const receipt = await rpc('eth_getTransactionReceipt', [command.identity]);
      assert(decoded.hash === command.identity && decoded.chainId === 11155111n && decoded.type === 2 && decoded.value === 0n && equalAddress(decoded.from, from.address) && equalAddress(decoded.to, token.contract), 'Independent signed transaction identity/domain mismatch');
      const expectedData = abi.encodeFunctionData('transfer', [to.address, BigInt(command.amount)]);
      assert(decoded.data === expectedData && transaction?.input === expectedData && equalAddress(transaction.from, from.address) && equalAddress(transaction.to, token.contract), 'Independent token calldata/sender mismatch');
      assert(BigInt(transaction.nonce) === BigInt(command.nonce) && BigInt(command.nonce) === BigInt(before.nonces[command.from]), 'Independent nonce mismatch');
      assert(receipt?.status === '0x1' && receipt.transactionHash === command.identity && receipt.logs.length === 1, 'Actual ERC20 receipt failed or has unexpected effects');
      const event = abi.parseLog(receipt.logs[0]);
      assert(equalAddress(receipt.logs[0].address, token.contract) && event.name === 'Transfer' && equalAddress(event.args[0], from.address) && equalAddress(event.args[1], to.address) && event.args[2] === BigInt(command.amount), 'Independent Transfer event mismatch');
      const after = await snapshot();
      const fee = BigInt(receipt.gasUsed) * BigInt(receipt.effectiveGasPrice);
      assert(fee > 0n && fee <= BigInt(command.maxFeeAtomic), 'Actual gas exceeded the Go-approved reserve');
      for (let i = 0; i < 2; i++) {
        const expectedDebit = i === command.from ? fee : 0n;
        assert(BigInt(before.native[i]) - BigInt(after.native[i]) === expectedDebit, 'Native debit differs from gas-only cost');
        assert(BigInt(after.nonces[i]) === BigInt(before.nonces[i]) + (i === command.from ? 1n : 0n), 'Nonce changed without exactly one approved transfer');
        for (let j = 0; j < tokens.length; j++) {
          const delta = j !== command.token ? 0n : i === command.from ? -BigInt(command.amount) : BigInt(command.amount);
          assert(BigInt(after.balances[j][i]) - BigInt(before.balances[j][i]) === delta, 'Actual token balance effect differs from approval');
        }
      }
      const result = { status: 'PASS', identity: command.identity, contract: token.contract, symbol: token.symbol, decimals: token.decimals,
        source: from.address, recipient: to.address, amountAtomic: command.amount, nonce: command.nonce,
        gasChargeAtomic: fee.toString(), nativeDebitAtomic: (BigInt(before.native[command.from]) - BigInt(after.native[command.from])).toString(),
        receiptStatus: receipt.status, before, after };
      transfers.push(result); before = undefined; write(result); continue;
    }
    if (command.op === 'finish') {
      assert(transfers.length === 4 && tokens.every(token => transfers.filter(row => row.contract === token.contract).length === 2), 'Missing two-way execution for both tokens');
      const final = await snapshot();
      assert(final.nonces.every(nonce => nonce === '2') && BigInt(await rpc('eth_getTransactionCount', [accounts[2].address, 'latest'])) === 2n, 'Unexpected wallet/deployer transaction nonce');
      const mined = [];
      for (let number = 0n, latest = BigInt(await rpc('eth_blockNumber', [])); number <= latest; number++) {
        const block = await rpc('eth_getBlockByNumber', [toBeHex(number), false]); mined.push(...block.transactions);
      }
      const expected = new Set([...deployments, ...transfers.map(row => row.identity)]);
      assert(mined.length === 6 && new Set(mined).size === 6 && mined.every(hash => expected.has(hash)), 'Unexpected transaction beyond two deployments and four approved transfers');
      write({ status: 'PASS', chainId: '11155111', independentOracle: 'ethers 6.17.0 / Ganache 7.9.2 / solc 0.8.30 actual deployed ERC20 execution',
        scope: 'Owned loopback Sepolia-shaped EVM; synthetic public fixture keys/funds only; no public network, wallet GUI, independent consensus or real finality claim',
        producerSHA256: sha(readFileSync(new URL(import.meta.url))), packages, transfers, faults, minedTransactions: mined, final });
      input.close(); break;
    }
    throw Error('Unrecognized local oracle command');
  }
} catch {
  process.stderr.write('Local ERC20 execution oracle failed; no acceptance claim.\n');
  process.exitCode = 1;
} finally {
  await close();
}
