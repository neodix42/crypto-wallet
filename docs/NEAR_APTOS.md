# NEAR and Aptos adapter evidence

Status recorded on 2026-10-23, Node 24.19.0, Linux amd64. Initial interoperability/fuzz/node runs used Go 1.26.8; final hermetic unit/race/vet checks and both real local-node harnesses were repeated after the parent upgraded to Go 1.27.3. These are development implementation and test results, not an independent security audit. All phrases and keys used by these tests are public synthetic fixtures. No mainnet transaction was submitted.

## Implemented behavior

Both adapters implement real account derivation, public balances, native transfer preparation, strict signer-side plan reconstruction, Ed25519 signing and local signature verification, submission, and reconciliation of the original transaction identity. There is no raw signing endpoint. HTTP transport rejects redirects, remote cleartext endpoints, oversized JSON, duplicate members and excessive nesting; numeric values are bounded canonical atomic integers. Every network operation validates the ledger identity; stale or malformed heads fail closed. Unknown or unavailable balances return errors rather than confirmed zeroes.

NEAR uses `near-bip39-slip10-v1`, `m/44'/397'/i'`, a full 64-byte BIP39 seed, and a lowercase hexadecimal Ed25519 implicit address. The transfer encoder emits native Borsh transaction V0 with exactly one `Transfer` action, hashes the transaction with SHA-256 and signs that hash. It validates full-access permission at a final block, per-key nonce, current protocol costs, storage reserve and recipient existence. A missing implicit account can be created by transfer; a missing named account is refused during preparation. `Associate` accepts a named account only after querying the derived key's full-access authority. It preserves the durable account ID. Account parameters retain the exact path, implicit key address and named-association flag. `ValidateRecovery` checks these fields offline; offline validity does not prove current on-chain authority.

NEAR transactions have **no signed gas-price or maximum-fee field**. The approval therefore explicitly labels its amount an estimated fee reserve, not a protocol-enforced ceiling. Preparation uses current runtime transfer/receipt costs, adds account/key creation costs for a new implicit recipient, honors the runtime minimum gas-purchase price, and reserves twice the estimate. Submission refreshes full-access authority, the exact nonce and gas price immediately before broadcast, and refuses a price exceeding the approved reserve. A provider or subsequent protocol price change can still invalidate an estimate; this must remain visible in the native confirmation. Plans expire locally after 60 seconds. NEAR's own block-reference validity remains a separate protocol rule.

Aptos uses `aptos-bip39-legacy-ed25519-v1`, `m/44'/637'/i'/0'/0'`, and explicitly hashes `public_key || 0x00` with SHA3-256 for **Legacy Ed25519** authentication. SingleKey is a different, rejected scheme. The official Go SDK builds BCS `0x1::aptos_account::transfer` with no type arguments, exact recipient/amount, chain ID, sequence, gas ceiling, unit price and expiration. The signer rebuilds those bytes and compares the full BCS transaction with the plan; it never decodes or signs an arbitrary supplied payload. Public preparation requires successful node simulation, applies bounded gas headroom, checks balance including maximum gas, and sets a 120-second expiration. Submission rechecks the account's current authentication key and sequence. Proven-rotation discovery uses the official `0x1::account::originating_address` view; explicit verified original-address association also exists. Unproven rotations need explicit metadata/association and are not claimed discoverable automatically.

NEAR distinguishes final execution failure from submission and final success. Aptos distinguishes pending, committed success and committed failure; a committed failure consumes sequence and may consume gas. Reconciliation never derives a new nonce or signs a replacement. Reservations, native approval, backup encryption, lock semantics and durable persistence belong to the signer/coordinator rather than these stateless adapters.

Both adapters also implement bounded public native incoming/outgoing history with explicit scope text. NEAR follows the last 12 final parent blocks and newly included chunks, recognizes exactly one native Transfer action, and queries execution independently by its original transaction identity. Reused chunks do not duplicate payments. Aptos scans the last 256 committed ledger versions in pages of at most 32, recognizing direct `aptos_account::transfer`, native `aptos_account::transfer_coins` and native `coin::transfer` entry functions. These scans show successful transfers and failed attempts distinctly. They intentionally identify older, contract-mediated and unrecognized effects as unresolved; no empty result proves zero funds or a complete account history. Genuine node tests verify that both sender and recipient histories include the independently accepted native transaction IDs.

## Pinned provenance and revalidation

| Component | Pin / provenance |
|---|---|
| Aptos production transaction primitives | `github.com/aptos-labs/aptos-go-sdk v1.14.0`, commit `2262084301e5bafc8468633a3825d2cebeca9aec`; module hashes in `go.sum` |
| Aptos independent oracle | `@aptos-labs/ts-sdk 7.5.0`, tag `ts-sdk-v7.5.0`, source commit `0794228163008ef4f367725c8bf9e6ab609ce162`; full transitive integrity lockfile under `tests/oracles/aptos` |
| NEAR independent derivation | `near-seed-phrase 0.2.1`, source tag `v0.2.1`, commit `e6cd4c1750f28bc4ee597d42daf454676f3c7c15` |
| NEAR independent transactions | `@near-js/transactions 2.5.1`, `@near-js/crypto 2.5.1`, source commit `345de7c326bc2da93dc0625ace8d123fc3042b17`; transitive integrity lockfile under `tests/oracles/near` |
| NEAR local node | `near-sandbox 2.13.4`, commit `44f7ae6cd7ef08bab604e20a473bf77e35d4c993`, protocol 86; JS lifecycle wrapper `near-sandbox 0.3.4` |
| Aptos local node | CLI `7.11.0`, source tag `aptos-cli-v7.11.0`, commit `c94ee27833fee84e58d57693a168ca512d4a150f`, dedicated chain ID 4 |

Primary references checked during implementation: [NEAR account IDs](https://docs.near.org/protocol/accounts-contracts/account-id), [NEAR keys](https://docs.near.org/protocol/accounts-contracts/access-keys), [NEAR transactions](https://docs.near.org/api/rpc/transactions), [NEAR runtime protocol config](https://docs.near.org/api/rpc/protocol), [NEAR runtime fee model](https://github.com/near/nearcore/blob/master/docs/architecture/how/gas.md), [Aptos Go accounts](https://aptos.dev/build/sdks/go-sdk/account), [Aptos simulation](https://aptos.dev/rest-api/operations/simulate_transaction), [Aptos rotation](https://aptos.dev/build/guides/key-rotation), and [official Aptos framework originating-address semantics](https://github.com/aptos-labs/aptos-core/blob/main/aptos-move/framework/aptos-framework/sources/account/account.move). Floating documentation links are research provenance, not dependency pins.

Read-only live endpoint revalidation on 2026-10-23 observed Aptos mainnet chain 1 (`433c185c8e9c41205a83dfe289ac1072b7f00d1a`) and testnet chain 2 (`4c17e93210c07acf1abcf0ca36fc94d114d9e723`). NEAR mainnet reported protocol 86 / node 2.13.4; testnet reported protocol 88 / node 2.14.0-rc.5 (`b7b4737cab46f17a6daa2ed694d7aebbd204363a`). These queries establish provider-reported configuration only. No claim is made that the protocol-86 sandbox proves every protocol-88 public testnet behavior.

## Reproduction

Run from the repository root. Oracle generators must never receive user secrets. The committed fixture inputs are the standard public `abandon ... about` BIP39 vector with an empty BIP39 passphrase; indices 0, 1 and 1234 are tested.

```sh
npm ci --ignore-scripts --prefix tests/oracles/near
npm ci --ignore-scripts --prefix tests/oracles/aptos
node tests/oracles/near/generate.mjs
node tests/oracles/aptos/generate.mjs
go test ./internal/chains/near ./internal/chains/aptos ./internal/chains/chainutil
```

The expected fixture SHA-256 checksums are:

```text
337fd308b90cafeffac0c9b838a3e1f83340e8a7b5b7c439ef62f8d315b97f35  tests/oracles/near/fixtures.json
2bfbbe83977c50de12e7603d7ed5ca4cba83a0cd3d1f4148e855e01debbdc69a  tests/oracles/aptos/fixtures.json
```

Install the exact native local testing tools, then run the real local protocols:

```sh
tests/oracles/near/install-sandbox.sh
tests/oracles/aptos/install-cli.sh
node tests/oracles/near/run-local.mjs
node tests/oracles/aptos/run-local.mjs
```

Install scripts check pinned archive and executable SHA-256 values. Harnesses check executable identity before running, use isolated temporary state containing only synthetic test material, and stop their node processes at exit. The Aptos harness refuses to reuse an unknown service on port 8080. Both integration tests reject remote endpoint selection. The NEAR wrapper uses a workspace temporary directory to avoid an upstream cross-filesystem rename failure. Native test binaries and transient states are ignored under `.tools/`; they are not wallet production assets.

## Evidence checklist

| Requirement / gate | Result | Evidence |
|---|---|---|
| TP-002 / TP-022 derivation interoperability | PASS | Both independent fixture generators; exact public keys/addresses for indices 0, 1 and 1234 |
| TP-002 byte/signature interoperability | PASS | NEAR Borsh, SHA-256 identity, signature and signed bytes; Aptos BCS, signing message, signature, signed bytes and hash |
| TP-032 descriptor restoration | PASS locally | Canonical immutable metadata, named/original-address associations preserved; path/scheme/ID substitution rejected by `ValidateRecovery` |
| NEAR actual native acceptance | PASS locally | Sandbox accepted implicit and named-account transfers, created a fresh implicit account; independent raw RPC queries verified exact recipient balance changes |
| NEAR adverse authority/execution | PASS locally | Independent JS SDK deleted a real access key and installed a function-call-only key; Go refused both. Missing named recipient refused. Accepted-but-failed action receipt remained FAILED on reconciliation |
| Aptos actual native acceptance | PASS locally | Validator committed signed BCS transfer; independent raw REST recipient balance increased by exactly 123456789 octas |
| Aptos actual rotation recovery | PASS locally | Independent TS SDK committed proven rotation; Go discovered original address, rejected old authority, restored and sent with the new derived key |
| Aptos committed failure | PASS locally | Deliberate synthetic insufficient-funds transfer committed as failure; Go reported FAILED and independent sequence query advanced exactly once |
| TP-042 intent/artifact changes | PASS | Mutated amount, recipient, chain, sequence, expiry, asset, fee, scheme, profile, descriptor and signed artifacts rejected |
| TP-051 / TP-060 hostile RPC and uncertainty | PASS locally | Wrong/stale ledger, missing authority, malformed amounts/gas/costs, failed simulation, fee spikes, overflow, response/identity mismatch and unavailable status return errors/UNKNOWN, never invented payment success |
| TP-010 statement floors | PASS for these packages | Latest hermetic run: NEAR 99.0%, Aptos 96.6%, chainutil 98.9%. This is statement coverage, not branch coverage |
| Race / vet | PASS | `go test -race` and `go vet` for these three packages |
| TP-012 short fuzz campaign | PASS | Two workers, 5-second request each: NEAR address 142158 executions; Aptos address 151437; JSON 47008. No failure found; this is a short campaign |
| TP-071 bounded parser stress | PASS for this workload | 10000 duplicate/nested JSON and bounded-integer cases in `TestTP071MalformedStress`; this does not replace the complete wallet scheduler/KDF stress gate |
| Public testnet funded receive/send | NOT_RUN | No public faucet balances, testnet transfers or public transaction receipts were used. Local acceptance is separate evidence |
| Third-party wallet application UI import | NOT_RUN | SDK parity is tested; no claim of an identified wallet application's import UI behavior |
| Packaged clean OS recovery / security review | NOT_RUN here | Parent release checklist owns OS/display/package/recovery gates and external review |

Remaining uncovered local statements were reviewed rather than excluded. NEAR's retained branches are error returns after already validated transaction reconstruction or an unexpected local Ed25519 verification failure, plus defensive history-envelope checks. Aptos's remaining branches are defensive SDK serialization/signature error returns after bounded typed construction and a redundant gas bound following a tighter preceding limit. Transport's remaining statement is the JSON-token switch default for a closing delimiter that the decoder rejects before delivering it. No code or checks were removed to increase coverage.

## Signer/coordinator review regression evidence

An implementation-team review identified rejected-receipt state mutation, undurable signed-artifact exposure after persistence/change-sequence failure, malformed association descriptors, and lock/query context races. These are development review findings, not an external audit. Parent production fixes and focused tests now cover receipt identity/state validation before mutation, persistence quarantine, DCR change exhaustion, immutable association metadata, and cancellation of live network queries on lock.

`internal/signer/review_test.go` and `internal/signer/adversarial_test.go` exercise real encrypted IO, password/backup/restore failures, native approval cancellation, credential containment, QR decode/copy/save boundaries, explicit discovery limits, fee-reserve wording, durable signed artifacts and DCR range exhaustion. The observed signer statement coverage was 97.0%; signer race checks passed. Short-write tests reduce only the test process's file-size limit temporarily and verify that the prior encrypted vault generation remains recoverable.

`cmd/wallet-core/main_test.go` uses a manifest-checked inherited-pipe public fake child and public synthetic adapters to test the actual event loop, typed handlers, atomic precision, maximum-amount fee convergence, endpoint policy, incomplete history labeling, lock cancellation, retry/reconciliation and the requirement to record SUBMITTING before broadcast. This is coordinator fault evidence, separate from genuine node acceptance. Coordinator tests and race checks pass; the latest measured statement coverage is 94.1%, including the public history/Max paths.
