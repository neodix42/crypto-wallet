# Ethereum and Base ERC-20 wallets

Ethereum and Base wallets display native ETH and all configured ERC-20 assets together.
Ethereum mainnet includes pinned USDT; Base includes canonical ZEN. Additional tokens are
explicitly tracked by their full contract address on Ethereum mainnet/Sepolia or Base mainnet/Sepolia;
Horizen networks are explicitly retired under the later owner request; the existing canonical Base ZEN identity remains pinned.

## Add and use a token

1. Open the Ethereum or Base wallet tab and find **Track an ERC-20 token**.
2. Enter its full contract address for that exact network, then choose
   **Verify and add token**. The contract is the asset identifier; it is not a
   recipient address.
3. Review the separate Go-authored confirmation: network, owned account,
   untrusted symbol, decimals and observed runtime code hash. Confirm with the
   current local app password. Adding a token sends no transaction or allowance.
4. Its **Token / Balance / Decimals** table row shows the exact token balance and
   decimal precision. Hover or keyboard-focus the token button to see its full
   contract and exact network; identities remain distinct even when symbols match. Select that
   row's asset button before **Send**. Amounts are whole token units: `1.25`
   means 1.25 of that token, using its verified decimal precision.
5. **Receive** uses the same owned address and QR code for all tokens
   on that network. Keep ETH available for transaction fees when sending.

Balances cover configured contracts, including zero balances. A failed query
remains **Unknown** for that asset; it does not replace another observed balance
with zero. Block/metadata observations come from the configured RPC provider,
not an independent consensus proof.

There is no automatic claim to discover every token already held by an address.
The [standard Ethereum JSON-RPC API](https://ethereum.org/developers/docs/apis/json-rpc/)
does not supply an address-wide token inventory. Ethereum uses explicit contract tracking. Base additionally uses the bounded
Blockscout candidate discovery described below, disclosing the public address
to that indexer; candidates still need separate exact-contract signing approval.

## Supported behavior and trust

The existing Go transfer engine builds, validates and signs only
`transfer(address,uint256)` for an imported token. Each spend retains exact
recipient/asset/amount/fee/nonce/expiry approval, password reauthentication and
the encrypted operation journal. Simulation must succeed, and confirmation
requires the matching unremoved `Transfer` effect. Failed or ambiguous submission
keeps its original operation identity; retries do not create replacement spends.
No `approve`, `transferFrom`, permit, arbitrary calldata or custom Bybit route is
enabled. The existing canonical USDT no-return/issuer checks remain narrowly
scoped to that pinned token.

The [ERC-20 standard](https://eips.ethereum.org/EIPS/eip-20) makes display metadata
optional and requires callers to handle `false` transfer results. This wallet
requires supported bounded ABI `symbol()` and `decimals()` metadata for import;
it does not guess 18 decimals. The amount engine supports 0–77 decimals and
256-bit atomic quantities. Symbols are untrusted, bounded display text, and can
collide. The full contract and network distinguish tokens. At most 32 additional
token policies are retained per profile.

Runtime bytecode and precision are checked against the approved policy. This is
not a token audit: a proxy's implementation or issuer-controlled storage can
change without changing the proxy bytecode. Fee-on-transfer, rebasing, missing
metadata and nonstandard transfer results are unsupported. Do not treat a
familiar symbol or successful import as proof of issuer legitimacy or value.
An arbitrary contract can return success or emit events without implementing the
standard balance effects; runtime pinning and receipt checks cannot prove its
complete behavior. The local integration proves the standard-token flows only.

## Persistence and recovery

The signer stores approved public token policies inside the existing encrypted
vault, alongside the unchanged wallet data. Lock/unlock and restart must reload
those policies before a token can be used. The recovery phrase still derives the
same addresses and keys on the selected EVM networks; tracking a token never creates a different
wallet or private key.

Phrase-only recovery on a new installation does not restore the watch list or
its previous runtime approvals. Import each token contract again and review its
current metadata. On-chain token ownership remains attached to the restored
address. App passwords protect each local installation and are not part of
Ethereum key derivation.

## Previous Ethereum-only checkpoint

These rows record the preceding Ethereum-only implementation before Appearance, Base imports/discovery and Horizen retirement. They do not certify the changed candidate. Current evidence is recorded in [STATUS.md](STATUS.md); the local execution gate now requires both Ethereum and Base.

| State | Required evidence |
| --- | --- |
| PASS | Nine controller tests: multiple-asset rows, full contracts/precision, exact selection, stale preview invalidation, missing/duplicate/failed observations, contract-only import dispatch and shared receive address. |
| PASS, scoped actual GUI | Separate ETH/USDT rows, complete contract, 18/6 decimal metadata, whole-USDT Send and the same Go-verified owned receive address on isolated X11. RPC observations intentionally remain unknown; no balance or payment success is mocked. |
| PASS | Six backend packages under race detection; metadata/policy, private confirmation/current password, cancellation/stale-generation/persistence failures, encrypted fresh-engine reload, malformed/null/duplicate/cross-profile vault and child-handler requests, and exact signed transfer bytes. |
| PASS | Two real locally deployed ERC-20 contracts with 6/18 decimals: four Go-signed transfers out/back; independent ethers receipt/log/balance/gas/nonce checks; exactly six transactions including two deployments. Actual precision drift, missing runtime and false-return faults reject safely. `reports/erc20-execution.json` records producer/source hashes and results. |
| PASS | All 22 semantic mutants killed, including token-import confirmation/password and shared runtime/false-result guards; all 18 bounded fuzz targets actually execute mutations. |
| PASS, local aggregate | Full unchanged-source Go/frontend coverage, all 33 package floors and four original/additional local execution gates; full unit/security/race/interoperability/integration/fuzz/dependency commands. Wails adapter 95.43%, controller 96.87%, EVM 97.05%; no floor is lowered. |
| NOT_RUN when this source checkpoint was recorded | Hosted CI runs after commit/push; inspect the exact commit's [Actions checks](https://github.com/neodix42/crypto-wallet/actions). Local results do not imply hosted success. |
| NOT_RUN | Funded public-network token transfers, clean supported OS/Wayland matrix and independent security review. No mainnet funds or user secrets are used in these tests. |

Run `make test-erc20` after provisioning the pinned test dependencies with
`./scripts/with-toolchain.sh python3 scripts/provision-tests.py`. The actual
two-token execution test can also be run explicitly:

```sh
WALLET_ERC20_EVM=1 ./scripts/with-toolchain.sh go test ./internal/chains/evm \
  -run '^TestTP050ImportedERC20GoSignedTwoWayExecutionOnIndependentGanache$' -v -count=1
```

`make test-integration` and `make coverage` require this real local execution in
addition to the original chain/emulator gates. The latter rejects a missing or
skipped token test. `make test-fuzz` includes the new token-policy/metadata fuzz
target. Unit fixtures are not counted as real EVM transaction acceptance.

## Base discovery and retired Horizen compatibility

Current local evidence: the unchanged-source two-chain execution gate passes on Ethereum Sepolia-shaped and Base Sepolia-shaped disposable Ganache nodes: eight Go-signed transfers and exactly twelve mined transactions (four token deployments plus eight transfers), all independently checked. Metadata/runtime/false-return faults remain rejected; Base missing/increased fixture fee-oracle observations deny broadcast. The canonical Base ZEN runtime is not deployed by this custom-token oracle and correctly stays unavailable without hiding native/imported balances. Actual Base fees/consensus and funded public-chain transfers remain NOT_RUN. `reports/erc20-execution.json` requires both profiles; `reports/base-erc20-execution.json` records the Base case separately.

Final local unit/security/race/interoperability/integration/fuzz, full 20-step normal/instrumented GUI and all 33 package coverage floors pass; EVM 96.81%, controller 97.04%, Wails 95.10%. All 24 semantic mutants are killed, with no lowered floor or deleted guard. These records certify only their stated local scopes; hosted tests for the new feature commit, clean OS/funded-network acceptance and independent review remain separate. See [STATUS.md](STATUS.md).

Base has ETH and canonical ZEN pinned; other standard ERC-20 tokens use the same owned Base address. Opening a Base wallet requests candidate contracts from `base.blockscout.com` (or `base-sepolia.blockscout.com` for testnet), using the documented [Blockscout token-balances schema](https://github.com/blockscout/blockscout-api-v2-swagger/blob/main/swagger.yaml). No API credential or secret is sent. This discloses the public address to that indexer in addition to the selected RPC provider.

The indexer is not trusted for symbols, decimals, quantities, logos, URLs or signing policy. Only candidate ERC-20 contract addresses are consumed. The response is bounded to 256 KiB/256 entries and at most 32 new contracts; redirects are rejected. Go checks the Base chain and a fresh block, reads standard symbol/decimals/runtime and rechecks runtime/precision before reading each balance at that same block. Only positive verified candidate balances appear in a separate **unapproved token** list. Existing pinned/tracked balances retain their own independent observations.

Choose **Verify and add token** to run the ordinary exact-contract private confirmation and current-password import. Successful discovery alone cannot select a new Send asset, persist a token policy or add a Bybit route. Symbols may be spam or collide. Ignore unsolicited tokens; the runtime hash does not prove issuer/proxy safety.

Indexer inventory is incomplete and provider availability is not guaranteed. Failure displays **inventory unknown**, partial verification has a separate warning, and an empty response never claims that the account holds no other tokens. **Refresh token discovery** retries explicitly; exact-contract import remains available. The live provider probe in this development environment returned HTTP403, so live indexer acceptance is **BLOCKED** here. Controlled provider/RPC tests and real local EVM token execution are different evidence.

Active Horizen mainnet/testnet networks, gas domains, endpoints and exchange routes have been removed. Existing vaults still authenticate their exact historical derivation descriptors, retain encrypted endpoints and journal, and unlock the unchanged active wallets. Historical records do not enable RPC/signing; their journal remains visible through the general Journal page. No legacy operation, descriptor or funds are silently deleted or reinterpreted as Base. At the same account index, the frozen EVM profile derives the same EOA on Base, but balances and transaction histories remain distinct ledgers. Fresh phrase restoration reconstructs the eight active mainnet networks (24 descriptors at indices0,1,127), not retired network tabs.

## Focused Base verification, 2026-10-30

Base token support already uses the same tracked-token transfer engine; it does
not require a separate wallet or seed. The new Base-specific regressions pass
for both [chain IDs](https://docs.base.org/base-chain/api-reference/ethereum-json-rpc-api/eth_chainId), mainnet `8453` and Sepolia `84532`, with two same-symbol
contracts using different 6/18-decimal precision. They verify independent
balances, exact signed contract/network/native-value identity, native ETH gas,
and rejection of chain/contract/fee substitutions. Inspection alone cannot
enable signing. Changed runtime/precision, a false transfer result, missing ETH
and a wrong-chain RPC all fail without replacing another verified token balance
with zero or broadcasting a payment.

The actual local execution gate also passed with `sourceStable: true`: two
deployed contracts on each owned Ethereum/Base Sepolia-shaped EVM, eight
Go-signed out/back transfers in total and exactly twelve mined transactions
including deployments. Independent ethers checks cover signatures, calldata,
receipts, balances, gas and nonces. The Base report additionally rejects missing
or increased fixture fee-oracle quotes and preserves absent pinned ZEN as
unavailable. See `reports/base-erc20-execution.json`,
`reports/erc20-execution.json` and `reports/base-two-chain-local-execution.log`.
This is local standard-token execution; public Base acceptance, live indexer
availability, actual rollup fees, GUI acceptance and independent review are
separate gates.

```sh
./scripts/with-go.sh go test ./internal/chains/evm -run '^TestBase' -count=1
WALLET_ERC20_EVM=1 ./scripts/with-toolchain.sh go test ./internal/chains/evm \
  -run '^TestTP050ImportedERC20GoSignedTwoWayExecutionOnIndependentGanache$' -v -count=1
```
