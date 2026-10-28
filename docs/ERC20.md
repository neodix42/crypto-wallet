# Ethereum ERC-20 wallets

Ethereum wallets display native ETH and all configured ERC-20 assets together.
ETH and pinned USDT remain present by default on mainnet. Additional tokens are
explicitly tracked by their full contract address on Ethereum mainnet or Sepolia;
this does not remove any existing chain or canonical asset.

## Add and use a token

1. Open the Ethereum wallet tab and find **Track an ERC-20 token**.
2. Enter its full contract address for that exact Ethereum network, then choose
   **Verify and add token**. The contract is the asset identifier; it is not a
   recipient address.
3. Review the separate Go-authored confirmation: network, owned account,
   untrusted symbol, decimals and observed runtime code hash. Confirm with the
   current local app password. Adding a token sends no transaction or allowance.
4. Its asset row shows the token balance, full contract and decimals. Select that
   row's asset button before **Send**. Amounts are whole token units: `1.25`
   means 1.25 of that token, using its verified decimal precision.
5. **Receive** uses the same owned Ethereum address and QR code for all tokens
   on that network. Keep ETH available for transaction fees when sending.

Balances cover configured contracts, including zero balances. A failed query
remains **Unknown** for that asset; it does not replace another observed balance
with zero. Block/metadata observations come from the configured RPC provider,
not an independent consensus proof.

There is no automatic claim to discover every token already held by an address.
The [standard Ethereum JSON-RPC API](https://ethereum.org/developers/docs/apis/json-rpc/)
does not supply an address-wide token inventory. Import each additional contract
you want displayed. No third-party token indexer or address-disclosure service is
enabled by this feature.

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
same Ethereum addresses and keys; tracking a token never creates a different
wallet or private key.

Phrase-only recovery on a new installation does not restore the watch list or
its previous runtime approvals. Import each token contract again and review its
current metadata. On-chain token ownership remains attached to the restored
address. App passwords protect each local installation and are not part of
Ethereum key derivation.

## Evidence checklist

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
