# EVM and HyperCore implementation evidence

This document describes implemented adapters and observed development evidence. It is not a production approval or an independent audit. Public testnet transfers remain **NOT_RUN**. Fresh HyperCore recipient activation remains **BLOCKED** as described below.

## Implemented boundaries

`internal/chains/evm` implements Ethereum ETH, HyperEVM HYPE, Base ETH/ZEN, and current Horizen ETH/ZEN, including their specified testing counterparts. It implements `model.Adapter` directly: public derivation descriptors, address validation, live balances, preparation, secure plan reconstruction, EIP-1559 serialization/signing, local sender recovery, exact-byte submission, and receipt/execution/finality reconciliation. The signer accepts precisely a 64-byte BIP39 seed and the immutable `evm-bip39-bip32-v1` path `m/44'/60'/0'/0/i`. Indices 0, 1 and 127 match an independent ethers implementation. It rejects unsupported profiles, hardened final account indices, EIP-7702/delegated source accounts, arbitrary calldata, approvals, bridges, foreign token contracts, changed amounts and recipients, incorrect fee arithmetic, and replay-domain changes.

`internal/chains/hypercore` implements distinct Core Mainnet/Testnet spot HYPE ledgers. It derives the same explicitly selected EVM owner key and accepts only owner-signed `spotSend`. The exact human-readable EIP-712 schema uses `HyperliquidTransaction:SpotSend`, domain `HyperliquidSignTransaction`, version 1, signing chain ID `0x66eee` (421614), zero verifying contract, and explicit Mainnet/Testnet action field. The signing chain is not HyperEVM 998/999. Destination strings are lowercase; eight-decimal HYPE atomic amounts are normalized exactly without floating point or rounding. Agent, vault, subaccount, unified-account and portfolio-margin sending authorities fail closed. Standard owner accounts without an existing HYPE balance can receive ordinary transfers after their activation has been established.

Both packages contain no persistent secrets, no credential UI, no arbitrary signing endpoint, and no transaction journal. Only the dedicated signer process may call their seed-bearing methods. The parent coordinator owns approval/session binding, durable nonce reservation, storage, locking and the journal-before-broadcast invariant. Each `Submit` sends the original signed artifact once. An ambiguous response keeps its original identity and requires reconciliation; no method signs a replacement automatically. Best-effort erasure clears mutable seed-derived buffers and `big.Int` scalar backing words; this does not guarantee erasure of copies retained by the Go runtime, HMAC internals, signing libraries, or the OS.

Constructors are `New()` and `NewWithEndpoints(map[string]string) (*Adapter,error)`. Endpoint keys are network IDs; Core additionally accepts `<network>:explorer`. Overrides are copied, TLS is required except for explicitly configured loopback HTTP nodes, redirects are refused, request timeouts are 15 seconds, response bodies are limited to 2 MiB, and JSON duplicate fields/nesting/trailing data fail closed. The exported `HTTPClient` permits a custom transport but must preserve bounded, nonlogging behavior. The RPC trust mode is a provider-observed chain-ID/token-identity consistency check; it is not a light client or proof of consensus.

## Registry and fee policy

| Network ID | Chain/ledger | Required assets |
| --- | --- | --- |
| `ethereum-mainnet`, `ethereum-sepolia` | 1, 11155111 | `<network>:native` ETH |
| `hyperevm-mainnet`, `hyperevm-testnet` | 999, 998 | `<network>:native` HYPE, 18 decimals |
| `base-mainnet`, `base-sepolia` | 8453, 84532 | Native ETH and the exact ZEN contracts below |
| `horizen-mainnet`, `horizen-testnet` | 26514, 2651420 | Native ETH and the exact ZEN contracts below |
| `hypercore-mainnet` | HyperCore Mainnet spot | `hypercore-mainnet:HYPE:0x0d01dc56dcaaca66ad901c959b4011ec`, index 150, 8 decimals |
| `hypercore-testnet` | HyperCore Testnet spot | `hypercore-testnet:HYPE:0x7317beb7cceed72ef0b346074cc8e7ab`, index 1105, 8 decimals |

ZEN identities are `<network>:erc20:<checksummed contract>`. Base mainnet uses `0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229`; Base Sepolia uses `0x107fdE93838e3404934877935993782F977324BB`; Horizen mainnet uses `0x57da2D504bf8b83Ef304759d9f2648522D7a9280`; Horizen testnet uses `0xb06EC4ce262D8dbDc24Fac87479A49A7DC4cFb87`. All four deployments were queried read-only on 2026-10-23: chain IDs, deployed runtime bytecode and 18 decimals matched. Their runtime Keccak hashes are frozen and enforced in the adapter. The observed EIP-1967 implementation slots were zero; this observation does not establish absence of every upgrade or administrative mechanism. Unexpected code changes require a reviewed registry update. Base's OFT adapter is never substituted for its ZEN ERC-20.

EVM preparation queries a fresh head, pending nonce, balance, fee market, source code, gas estimate and simulation. Token transfers must return ABI `true`. Gas is reserved at 120% of the estimate, with a 30 million upper limit. The EIP-1559 execution cap is `gasLimit * maxFeePerGas`. On Base and Horizen the official OP gas oracle must also answer `getL1FeeUpperBound(uint256)` and `getOperatorFee(uint256)`; missing trustworthy fee state blocks preparation. Separate data/operator reserves are twice their current estimates and are rechecked immediately before broadcast.

**Rollup limitation:** OP data/operator fees have no transaction-signed absolute ceiling. Their state can change after the final query. The plan's total `MaxFee` includes a reserve; only its EIP-1559 execution portion has a protocol-enforced cap. Native confirmation must identify these reserves as estimates. A claim of an absolute total maximum or a completed production fee-safety gate is blocked until this product limitation is resolved explicitly. A short approval expiry and prebroadcast recheck reduce exposure but do not eliminate the race.

## History and reconciliation

Both adapters additionally expose `History(context.Context,model.Account) ([]model.Receipt,error)` and `HistoryScope() string`. Core also implements the bounded `model.HistoryPager` continuation interface. The UI must show the scope even for an empty result.

EVM history scans exactly the latest 128 blocks plus registered ZEN Transfer events; it validates canonical inclusion, bound receipts and execution status and distinguishes finality. It does not discover older activity, internal contract transfers or arbitrary tokens. Core history scans a fixed window covering the latest 30 days and reports `OBSERVED`, preserving its provider-observed meaning. It emits at most 128 receipts per page, binds compact adapter cursors to the full account descriptor and original end time, and commits provider rereads with SHA256. The coordinator replaces these cursors with one-use session-bound capabilities. Neither an empty scan nor an unavailable provider proves an empty historical wallet.

The [official info pagination rule](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint) uses an inclusive last timestamp. A full 500-event response defers its entire last-timestamp cluster, then queries that timestamp again. This avoids both losing tied events and returning duplicates. Invalid ordering/ranges, changed committed pages, duplicate identities, resource exhaustion and an unpageable 500-event single timestamp fail explicitly; no timestamp is incremented to conceal saturation. `Complete` means only that the pinned provider window was traversed, never archival completeness. The legacy whole-history method has its own 32-page compatibility budget and errors instead of truncating.

For a signed Core operation, reconciliation requires one exact owner/action match, including the nonce, environment, full token ID and amount, from Explorer `userDetails`; an independently queried `txDetails` must agree. Both sender and recipient ledger queries must contain the same native hash and exact zero-fee HYPE transfer effect. Each ledger scan traverses at most 32 API pages with a fixed verified end time. Extra same-hash fee/foreign/contradictory effects cannot be ignored. The typed signing digest is labelled `hypercore-eip712:` and is never misrepresented as the native ledger hash. Incomplete pagination, disagreement, missing effects or execution errors retain `UNKNOWN` and the original reservation. Recent official ledgers use ticker `HYPE`, accepted only after the exact Explorer action binds the hash to the pinned token identity.

## Fresh-recipient activation prerequisite

The [activation documentation](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/activation-gas-fee), checked 2026-10-24, requires one quote token for the first transaction to a new destination. The reviewed [spotSend/sendAsset schemas](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint) and pinned official signing types have no separate fee-token selector/ceiling; the [ledger schema](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions) does not establish its activation fee asset or debit source. These sources do not justify assuming that a funded USDC balance selects USDC for a HYPE transfer. Fresh recipients remain blocked until those semantics are established; existing standard-owner transfers remain available. No synthetic HTTP test is represented as accepted activation. The [research record](../tests/oracles/hypercore/activation-policy.json) identifies the precise missing protocol evidence.

Current local pagination/security evidence is [hypercore-pagination.json](../reports/hypercore-pagination.json): 1,205 history events preserve equal-time boundaries, and both 1,101-event ledgers reconcile through three API pages each. All HyperCore tests, race detection and vet pass; package statement coverage is 97.0%, ledger traversal and reconciliation 100%. A five-second cursor fuzz campaign completed 13,569 executions. Three isolated semantic mutants were killed: skipping an equal timestamp, ignoring a changed provider page and ignoring an extra same-hash fee effect. Reproduce with `./scripts/with-go.sh python3 tests/oracles/hypercore/mutation-pagination.py`. These are loopback simulations and public SDK fixtures, not validator consensus acceptance. Public testnet execution and all Decred work are excluded from this implementation turn by owner instruction.

## Reproducible tests and independent provenance

All fixture inputs are explicitly public synthetic values. Reference JavaScript/Python tools belong only in `tests/oracles`; they must not be bundled as production key-handling code or run against a user's secrets.

```sh
go test ./internal/chains/evm ./internal/chains/hypercore
go test -race ./internal/chains/evm ./internal/chains/hypercore
go vet ./internal/chains/evm ./internal/chains/hypercore
go test ./internal/chains/evm -coverprofile=/tmp/evm.cover
go test ./internal/chains/hypercore -coverprofile=/tmp/hypercore.cover
python3 tests/oracles/evm/mutation.py
GOMAXPROCS=2 go test ./internal/chains/evm -run '^$' -fuzz '^FuzzTP012AddressAndPlan$' -fuzztime=10s -parallel=2
GOMAXPROCS=2 go test ./internal/chains/hypercore -run '^$' -fuzz '^FuzzTP012CoreAction$' -fuzztime=10s -parallel=2
```

Regenerate independent fixtures explicitly:

```sh
npm ci --ignore-scripts --prefix tests/oracles/evm
node tests/oracles/evm/generate.mjs
python3 -m venv /tmp/localwallet-reference
/tmp/localwallet-reference/bin/pip install --require-hashes -r tests/oracles/hypercore/requirements.lock
/tmp/localwallet-reference/bin/python tests/oracles/hypercore/generate.py
/tmp/localwallet-reference/bin/python tests/oracles/evm/observe-networks.py
python3 tests/oracles/hypercore/observe-networks.py
```

Go signing/serialization uses official `github.com/ethereum/go-ethereum v1.17.9`. EVM reference bytes use ethers 6.17.0, pinned by `package-lock.json`. Core reference signatures use the official `hyperliquid-python-sdk 0.24.0`, tag commit `2fdb18f9517675ea03695a0962bd19eece9c83f0`, with eth-account 0.13.7 and the complete test dependency lock. Tests compare independent addresses, public keys, raw transactions, hashes, typed hashes and deterministic ECDSA signatures for indices 0, 1 and 127 on both Core environments. `fixture-checksums.json` records fixture hashes.

An actual official geth node can be reproduced in a separate terminal:

```sh
python3 tests/oracles/evm/geth-node.py
# While it runs, in another terminal:
LOCALWALLET_EVM_NODE=http://127.0.0.1:18547 \
  go test ./internal/chains/evm -run TestTP050LocalEVMProtocolAcceptance -v
```

The driver uses geth 1.17.9 image digest `sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109`, a synthetic locally funded genesis, local Engine API blocks, no peers and loopback-only published RPC. It removes its ephemeral container on termination. The test performs native receipt, signed send-back, independently queried recipient balance/receipts, and actual bounded incoming history. Its custom local chain ID deliberately matches Sepolia; it does not join or submit to Sepolia.

Local HyperEVM-compatible and exact deployed ZEN runtime tests use an independent Ganache 7.9.2 EVM. The OP fee oracle is a test fixture, not a real OP sequencer or public fee measurement. The public deployed Base/Horizen token runtime bytecodes are executed at their canonical addresses with synthetic local balances. Solidity 0.8.30 compiles only the fee fixture.

```sh
# Start one node per terminal; stop with Ctrl-C.
LOCAL_CHAIN_ID=84532 LOCAL_NODE_PORT=18546 node tests/oracles/evm/local-node.mjs
LOCAL_CHAIN_ID=2651420 LOCAL_NODE_PORT=18549 node tests/oracles/evm/local-node.mjs
LOCAL_CHAIN_ID=998 LOCAL_NODE_PORT=18550 node tests/oracles/evm/local-node.mjs
# Run the corresponding explicit test:
LOCALWALLET_EVM_NODE=http://127.0.0.1:18546 LOCALWALLET_EVM_NETWORK=base-sepolia go test ./internal/chains/evm -run TestTP050LocalEVMProtocolAcceptance -v
LOCALWALLET_EVM_NODE=http://127.0.0.1:18549 LOCALWALLET_EVM_NETWORK=horizen-testnet go test ./internal/chains/evm -run TestTP050LocalEVMProtocolAcceptance -v
LOCALWALLET_EVM_NODE=http://127.0.0.1:18550 LOCALWALLET_EVM_NETWORK=hyperevm-testnet go test ./internal/chains/evm -run TestTP050LocalEVMProtocolAcceptance -v
```

These tests transfer 0.1 local native units, send back 0.05, and, on token networks, transfer 0.1 local ZEN and send back 0.05. No mainnet action is performed. Ganache's optional native WebSocket extension is unavailable on this Node version; its documented JavaScript fallback was used. Its dependencies are test-only, not production signing dependencies.

## Evidence checklist

| Requirement / evidence | Status | Observed result |
| --- | --- | --- |
| TP-002, TP-022 independent EVM and Core fixtures | PASS | ethers 6.17.0 and official Python SDK 0.24.0; indices 0/1/127; immutable profiles |
| TP-010 package coverage | PASS | Earlier EVM 96.5%; current HyperCore 97.0% statement coverage after bounded pagination, cursor commitment and same-hash effect checks; no code excluded |
| TP-011 controlled chain-critical mutations | PASS | Eight mutants killed: EVM chain ID, native effects, token approval substitution, fee ceiling and unavailable rollup fees; Core environment, nonce binding, signature owner |
| TP-012 deterministic corpus and short fuzz | PASS | EVM 63,939 executions; Core 47,499 executions; ten-second campaigns with two workers; 313 interesting inputs retained; no observed failure |
| TP-042/052 malicious plans, authority, fees, RPC | PASS | Changed effects/domains rejected; wrong chain/precision/contract, delegated source, false ERC-20 result, insufficient gas, malformed/duplicate/bounded JSON and unavailable rollup fees rejected |
| TP-050 real local Ethereum protocol | PASS | Official geth 1.17.9 accepted native receive/send-back; independent receipt/balance/history queries |
| TP-050 local HyperEVM-compatible EVM | PASS | Chain 998 native serialization/execution/send-back on independent local EVM; HyperBFT acceptance is a separate missing gate |
| TP-050 Base and Horizen token execution | PASS | Exact official deployed runtime code executed in local EVM; receipt/event and independent contract balance/send-back/history queries; OP consensus and actual rollup fee execution are not covered |
| TP-050 official network/token read-only observations | PASS | Four ZEN chain IDs/decimals/runtime hashes and both Core HYPE IDs/precision frozen in provenance files |
| TP-060 ambiguous submission and nonce races | PASS | Exact signed artifact retained; no automatic retry/re-sign; 100 concurrent Core preparations use distinct nonces; durable coordinator recovery tested separately |
| Go race detector and vet for both adapters | PASS | Executed successfully; a race in the synthetic HTTP fixture was fixed before the passing rerun |
| Public Sepolia, HyperEVM, Base, Horizen receipts | NOT_RUN | Dedicated capped test funds, public execution receipts and independently queried recipient state remain required |
| Accepted Core transfer and independent real ledgers | NOT_RUN | Official signature fixtures and hostile API simulations do not constitute real Core acceptance |
| Fresh Core recipient activation | BLOCKED | Reviewed official sources establish one quote token but not fee selection/debit/receipt semantics for this HYPE route. Even a funded quote balance cannot authorize an unspecified fee. Sending to `role=missing` explicitly fails closed |
| Core unified/portfolio collateral spendability | BLOCKED | Current implementation permits standard owner spot accounts only; unsupported modes are rejected explicitly |
| Full historical discovery / arbitrary external profiles | BLOCKED | Bounded scans are visible; archival recovery and unknown third-party derivation paths are not certified |
| Absolute OP data/operator fee ceiling | BLOCKED | The protocol does not sign such a ceiling; reserves and prebroadcast checks do not eliminate subsequent state changes |
| Independent security review | NOT_RUN | Every signing adapter and the integrated signer/core boundary still require external review |

Uncovered statement review: the remaining key-derivation branches defend against astronomically rare invalid BIP32 master/child scalars and library failures after validated fixed inputs. They reject rather than silently changing account indices. No test-only HMAC/RNG override was introduced into production code to fabricate those cases. Additional uncovered branches are defensive serializer/signature self-verification failure checks, strict-parser states precluded by the preceding complete parse, impossible HTTP construction errors after endpoint validation, explicit history saturation guards, and nil erasure guards. These remain in the production source; coverage is not branch coverage and cannot certify those paths or replace independent review.

## Primary references checked

- [Official geth 1.17.9 release](https://github.com/ethereum/go-ethereum/releases/tag/v1.17.9), [EIP-1559](https://eips.ethereum.org/EIPS/eip-1559), [EIP-55](https://eips.ethereum.org/EIPS/eip-55).
- [Base network identities](https://docs.base.org/get-started/connect-to-base), [Base fee oracle and components](https://docs.base.org/specifications/transactions/network-fees), [OP operator fee specification](https://specs.optimism.io/protocol/isthmus/exec-engine.html), [Jovian operator fee change](https://specs.optimism.io/protocol/jovian/exec-engine.html).
- [Horizen ZEN registry](https://docs.horizen.io/horizen-chain/tokens-and-gas/zen-token/), [Horizen mainnet](https://docs.horizen.io/horizen-chain/network/mainnet/), [Horizen testnet](https://docs.horizen.io/horizen-chain/network/testnet/), [Horizen fee components](https://docs.horizen.io/horizen-chain/tokens-and-gas/gas-on-horizen/).
- [HyperEVM identities](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/hyperevm), [Core spotSend fields](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint), [official pinned Python signing](https://github.com/hyperliquid-dex/hyperliquid-python-sdk/blob/2fdb18f9517675ea03695a0962bd19eece9c83f0/hyperliquid/utils/signing.py), [Core activation requirement](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/activation-gas-fee), [Core roles](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint), [ledger schemas](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions).
