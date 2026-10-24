# Decred and TON implementation evidence

> Recovery-policy update: canonical DCR/TON keys restore from the recorded BIP39 phrase and any original optional BIP39 passphrase. The phrase does not encode DCR discovery counters or noncanonical contract metadata; use explicit bounded discovery and preserve known parameters. Encrypted portable-backup authentication mentioned in historical campaigns below is superseded as a product workflow. See [PHRASE_RECOVERY.md](PHRASE_RECOVERY.md).


This records adapter development evidence on Ubuntu 26.04 amd64, Go 1.27.3 and Node 24.19.0. Public deterministic fixtures are test material, never wallets for deposits. No mainnet funds were sent, and no real user secret was imported. These results do not satisfy the complete production release gates.

## Implemented boundaries

`internal/chains/decred` implements `model.Adapter`, actual Decred transaction encoding, signing with ECDSA and verification in the official script engine. The root is the complete 64-byte BIP39 seed. Every path level uses official `Child()`, with mainnet coin type 42 and testnet/simnet coin type 1. An independent generator retains nine network/account rows and a BIP39 passphrase fixture that actually diverges under strict BIP32 derivation. Account descriptors retain the account xpub, independently issued receive/change indices, birthday, gap and bounded discovery range. No seed is imported into dcrwallet and no second secret store exists.

Synchronization scans real dcrd blocks through authenticated trusted RPC, checks the network genesis, hash ancestry, block heights, transaction trees, creation transactions, output values/scripts, maturity, next-block stake approval, mempool spendability and canonical checkpoints. It handles chain advancement during a scan, rejects incomplete ranges and bounds allocations. Regular-tree disapproval stays nonterminal until the signed transaction's wire block expiry, preventing a still-valid payment from freeing reservations. Exact signed-byte retransmission is permitted after the UI approval deadline; creating a new signature is not. This is trusted-node synchronization, not SPV verification. Only ordinary ECDSA P2PKH outputs are spendable; staking, mixing and other output scripts are excluded from ordinary payment support. `estimatesmartfee(2, "conservative")` supplies a quote when available; the explicitly identified `relay-minimum-no-estimate` fallback is the node relay floor, not a prediction of confirmation time. Fees and atomic amounts use integers. Coinbase outputs and outputs created by any transaction with nonzero wire expiry require `CoinbaseMaturity`. Official validation rejects an input when candidate block height minus origin height is below that network parameter ([exact pinned source](https://github.com/decred/dcrd/blob/e07b56553d6d6af84aa139e33b4f6368c55c9d2d/internal/blockchain/validate.go#L3643)). Both synchronization and secure validation enforce it for either transaction tree. A confirmed receipt can therefore coexist with immature funds. The current adapter deliberately uses nonzero wire expiry on every payment, so recipient and change outputs require 256 confirmations on mainnet or 16 on testnet3/simnet. Native approval must disclose this delay; wire expiry is not just the local approval timer. `SyncReport.ImmatureOutputs` states the reason, required confirmations, remaining blocks and earliest candidate spend block. All public atomic quantities are decimal strings, including UTXO values, immature totals and received/spent history. Raw creation proofs stay inside Go rather than being repeated into desktop JSON. A maximal 10,000-record report can still exceed the 1 MiB desktop frame and requires explicit pagination; no silent truncation is performed. A wire preflight ties declared transaction/block vector and script lengths to the actual bounded input before official decoding. A measured 7-byte payload declaring 65,535 inputs allocated 6,291,507 bytes per upstream decode versus 52 bytes per guarded rejection (20 iterations on the recorded environment). `TestWireAllocationPreflightPreservesOfficialEncodings` checks all 3 serialization forms, both block trees and every-byte truncation; the preflight performs resource checks, while official decoding and script verification remain authoritative.

`internal/chains/ton` implements actual V5R1 external messages using the BIP39/SLIP-0010 `m/44'/607'/i'` key. It embeds the official V5R1 artifact, reconstructs StateInit, pins workchain 0, subwallet 0, mainnet wallet ID `0x7fffff11` and testnet wallet ID `0x7ffffffd`. Receiving uses non-bounceable, appropriately testnet-marked addresses. CRC, flags and workchain are checked. Outbound bounce behavior comes from queried recipient activation. Active source data must retain the expected public key, wallet ID, enabled signatures and canonical empty extensions dictionary. Changed code, extensions, frozen/suspended states and stale snapshots fail closed.

TON preparation estimates fees for the exact body and StateInit; submission rechecks authority, sequence, activation, balance and the signed body's fee estimate. TON V5R1 has no signed maximum-fee field. The displayed amount is an estimated fee reserve, with a separate storage reserve; the standard contract cannot make this a consensus-enforced fee ceiling. Reconciliation matches the signed incoming message, actual successful source action, exact outgoing transfer and recipient credit using decoded transaction BOCs. It does not infer a payment from seqno alone. Queries trust a TON Center-compatible provider with the official zerostate identity checked; they do not independently prove consensus/finality. Missing history in the bounded 100-transaction search returns `UNKNOWN`, never an invented failed payment.

Signed bytes and account/UTXO reservations are owned by the shared signer journal. TON `History`/`HistoryScope` return the latest 100 account transaction BOCs with native inbound/outbound amounts, recorded fees, local credit phase and source-action distinctions. Every entry remains `OBSERVED`; it never inherits `CONFIRMED` from a source action or a seqno. `Reconcile` separately proves the approved source effects and matching recipient credit. History rejects duplicate identities/logical times, foreign message account bindings and malformed output dictionaries. It bounds messages to 256 per transaction and 1000 per scan, failing explicitly on an incomplete range. Jettons and other currencies are not interpreted as native TON.

Both adapters construct a canonical Plan ID; a changed plan or signed artifact fails validation. Parent-process approval, durable reservation, backup authentication and process lifecycle evidence are documented separately.

## Dependency and artifact provenance

| Artifact | Pin / identity |
| --- | --- |
| Official Decred HD | `github.com/decred/dcrd/hdkeychain/v3 v3.1.3`; source commit `b7cb1f2b4b384baffc03ce45375b24572bff3db7` |
| Official Decred parameters | `chaincfg/v3 v3.3.0` |
| Official Decred scripts | `txscript/v4 v4.1.2` |
| Official Decred wire | `wire v1.8.0` |
| Official Decred curve | `dcrec/secp256k1/v4 v4.4.1` |
| Integration node | Requested module `github.com/decred/dcrd v1.10.9`, module sum `h1:NnmBxAvXwzuzP6JGYDSNu1TBMIoLPp0YdVixfm9gLHs=` |
| Executed node binary | SHA256 `efebac4b062c3a31b4caeb3dc01d1840aaeced52d311b8b994dbd6fdc7576b11`; built by Go 1.27.3 using `go install`; binary self-reports `2.1.7+release.local`. Module download metadata records origin commit `e07b56553d6d6af84aa139e33b4f6368c55c9d2d`, ref `refs/tags/release-v2.1.7`; module pin and self-reported version are recorded separately. Temporary binary is not the wallet package |
| TON Go cells/TLB | `github.com/xssnick/tonutils-go v1.19.1`, source `219b7aed1887395577de44277917e1f51a53e177`; community implementation, no independent audit claim |
| Official V5R1 compiled artifact | [source commit ff2a9cdb](https://github.com/ton-blockchain/wallet-contract-v5/blob/ff2a9cdb5abe3efdba1e773e4d03a53dea1550b1/build/wallet_v5.compiled.json) |
| Embedded BOC file SHA256 | `142231250bd2dc479aa587c672ffae08ef606dfb001af05fb3746b1b9fc90b91` |
| V5R1 cell code hash | `20834b7b72b112147e1b2fb457b84e74d1a30f04f737d4f62a668e9552d2b72f` |
| Independent TON SDK / emulator | `@ton/ton 16.3.0`, `@ton/core 0.63.1`, `@ton/crypto 3.3.0`, `@ton/sandbox 0.45.0`, exact lockfile in `tests/oracles/ton` |

Primary network/profile sources checked during implementation: [Decred official HD package](https://pkg.go.dev/github.com/decred/dcrd/hdkeychain/v3), [dcrd command documentation](https://docs.decred.org/wallets/cli/dcrd-and-dcrwallet-cli-arguments/), [official V5R1 wrapper source](https://github.com/ton-org/ton/tree/311dc3313dd31e44781551b398b92256518b2c61/src/wallets/v5r1), [mainnet configuration](https://ton.org/global.config.json), [testnet configuration](https://ton.org/testnet-global.config.json). An actual read-only testnet `getMasterchainInfo` response matched testnet zerostate root `gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=` and file hash `Z+IKwYS54DmmJmesw/nAD5DzWadnOCMzee+kdgSYDOg=`. A connectivity query is not a transfer acceptance test.

## Reproduce

Run from the repository root. Dependency installation uses the pinned lockfiles; tests create temporary simnet data directories and cleanly stop their own node.

```sh
go run ./tests/oracles/decred
npm ci --ignore-scripts --prefix tests/oracles/ton
npm test --prefix tests/oracles/ton
go test ./internal/chains/decred ./internal/chains/ton
mkdir -p .tools
GOBIN="$PWD/.tools" go install github.com/decred/dcrd@v1.10.9
WALLET_DCRD="$PWD/.tools/dcrd" WALLET_TON_EMULATOR=1 go test -count=1 -v ./internal/chains/decred ./internal/chains/ton
WALLET_DCRD="$PWD/.tools/dcrd" WALLET_TON_EMULATOR=1 go test -count=1 -race ./internal/chains/decred ./internal/chains/ton
go vet ./internal/chains/decred ./internal/chains/ton
python3 tests/oracles/decred/mutation.py
go test ./internal/chains/decred -run '^$' -fuzz FuzzAddressesScriptsOutpoints -fuzztime=15s -parallel=4
go test ./internal/chains/ton -run '^$' -fuzz FuzzAddressAndBOC -fuzztime=15s -parallel=4
WALLET_DCRD="$PWD/.tools/dcrd" WALLET_TON_EMULATOR=1 go test -coverprofile=/tmp/dcr-ton.cover ./internal/chains/decred ./internal/chains/ton
go tool cover -func=/tmp/dcr-ton.cover
```

The integration tests explicitly skip, with a `NOT_RUN` reason, when `WALLET_DCRD` / `WALLET_TON_EMULATOR` are absent. A passing default unit-test invocation therefore does not establish node/emulator execution. Fixture regeneration is explicit: `go run ./tests/oracles/decred --generate` and `node tests/oracles/ton/oracle.cjs --generate`; review all changes before treating new outputs as compatibility evidence.

The coordinator may configure DCR scan settings through `SetDiscovery(account, birthday, gap, addressLimit)`, persist the returned descriptor, and select `SetScanBudget(blocks)` up to 2,000,000 blocks. `SetDiscoveryBudget(account,blocks)` persists an account-specific bound; zero preserves the adapter default. Legacy descriptors omit the new zero-valued field and retain their bytes. `ChainTip(ctx, network)` verifies the configured node's identity. `SyncWithProgress` reports the actual scanned height and target. A fresh generated wallet can deliberately initialize its birthday near the verified tip. Imported phrases require explicit historical scanning; a recent birthday must never be silently imposed. `AdvanceReceive` must be persisted before display; `AdvanceChange` must be committed with the signed operation. `ValidateRecovery` preserves these counters while checking the actual recovery root and issued address/public key.

## Executed checklist

| Requirement / evidence | Result |
| --- | --- |
| BC-07 / TP-022: nine official DCR derivation rows (indices 0, 1, 17; mainnet, testnet3, simnet); 64-byte BIP39 passphrase Child divergence | **PASS** — `tests/oracles/decred/fixtures.json`; `TestOfficialProfilesAndLeadingZero` |
| BC-07 / TP-051: both trees, duplicate/fake outpoints, fake values/scripts, balanced fake-value attack, foreign change, maturity, disapproval, gap 25, scan bounds, network identity | **PASS** — DCR unit/adverse tests; real node maturity evidence separately retained |
| BC-07 / TP-050: actual dcrd simnet fresh-recipient payment/send-back and independent final output query | **PASS** — [retained node result](../tests/oracles/decred/decred-simnet.json); recipient receives 10,000,000 atoms, expiry maturity is enforced, derived index 1 sends back 5,000,000 atoms, return has 7 confirmations and independent final unspent-output/change evidence |
| BC-08 / TP-022: six independent TON addresses, versioned StateInit and signed message/BOC hashes (indices 0, 1, 17, both networks) | **PASS** — `tests/oracles/ton/fixtures.json`; Go parity and independent JS fixture regeneration |
| BC-08 / TP-051: CRC/network flags, wrong code/wallet ID/key, disabled signatures/extensions, stale/frozen/suspended state, malformed BOCs/RPCs | **PASS** — TON unit/adverse tests |
| BC-08 / TP-050: Go-signed V5R1 first deployment, fresh-recipient index 1 deployment/send-back, credit, replay and expiry rejection in actual TVM emulator | **PASS** — [retained emulator transaction BOCs](../tests/oracles/ton/ton-emulator.json) |
| BC-08: native incoming/outgoing history from actual TVM BOCs | **PASS** — `History` uses bound 100, `OBSERVED` statuses and explicit credit distinctions; hostile binding/count/identity tests and live emulator source/recipient queries |
| TP-051: consumed TON seqno with skipped payment is never reported as paid | **PASS** — actual emulator insufficient-funds case and adapter reconciliation of that source BOC |
| TP-060: changed spend effects, signature tampering, accepted/failed/pending/unknown/reorged receipts and timeout after DCR acceptance | **PASS** — targeted adapter tests; whole-wallet crash/reservation campaign is separate |
| TP-011: ten targeted DCR/TON critical mutants | **PASS** — [mutation results](../tests/oracles/decred/mutation-results.json); unmutated baselines pass; all 10 compiled mutants killed, including secure/sync expiry maturity and invented TON history finality |
| TP-012: short parser/address/outpoint fuzz campaigns | **PASS** — DCR 216,714 executions in 16.060 s; TON 338,863 in 16.027 s; no discovered failure; three retained deterministic source seeds per target, plus Go-cache baselines documented in the report |
| TP-011: race detection and `go vet` for both adapters, including node/emulator scenarios | **PASS** — commands above executed; no race/vet report |
| TP-010: mandatory 95% critical-package statement coverage | **PASS** — DCR **96.4%**, TON **95.7%** with both real integrations enabled ([measured report](../tests/oracles/decred/coverage-results.json)). Added hostile node/receipt/authority/archived-artifact/monetary/resource rejection cases; all source guards remain. |
| DCR current public testnet acceptance, independent spendability query | **NOT_RUN** — no dedicated funded testnet account/faucet round-trip was performed |
| TON public testnet deployment/payment trace and independent balance query | **NOT_RUN** — only read-only provider identity plus local emulator execution was performed |
| Large live-chain DCR scan, long-history TON pagination/provider disagreement, 24-hour soak and packaged OS/display matrix | **NOT_RUN** for these adapter-specific scenarios; bounded local tests do not certify them |
| Independent external review of both signing adapters | **NOT_RUN** — implementation-author tests/review are not an external security audit |

## Remaining limitations

External testnet transfers, long-running synchronization/soak and external review remain release blockers. Both adapters meet the required 95% statement floor; this metric does not prove every branch or protocol behavior. DCR scanning is bounded and recomputed from the selected birthday rather than using an optimized persistent SPV wallet database; very large wallets hit a 10,000-record limit and require further incremental persistent synchronization work. Current ordinary-account results must be labeled with the chosen birthday/address range. Unknown or unsupported staking/mixing scripts must not be advertised as ordinary spendable balances. TON reconciliation searches a bounded 100-transaction window; missing older records stay unknown until a deliberate archival/history extension is implemented. Neither a trusted RPC response nor these local tests proves independent consensus validation, every external wallet version, or production readiness.
