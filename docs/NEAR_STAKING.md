# NEAR staking — implementation and evidence

Owner extension recorded 2026-10-28. This adds native Go staking-pool support to the existing NEAR account; it changes no recovery root, derivation path, vault format or other network. The Wails portfolio has **NEAR Wallet → Staking**, with AstroStakers `astro-stakers.poolv1.near` as the mainnet default. The private approval/password path remains separate from the public portfolio. No seed phrase enters a third-party wallet or staking website.

## Operations and exact units

| UI operation | Signed method | Arguments | Attached NEAR |
| --- | --- | --- | --- |
| Stake | `deposit_and_stake` | `{}` | Exact approved amount |
| Unstake amount | `unstake` | `{"amount":"<yoctoNEAR>"}` | Zero |
| Unstake all | `unstake_all` | `{}` | Zero |
| Withdraw all | `withdraw_all` | `{}` | Zero |

Input uses whole NEAR: `1` means `1000000000000000000000000` yoctoNEAR. Go validates up to 24 decimal places and bounded unsigned 128-bit integers; no floating point or silent rounding is used. Every operation signs exactly one Borsh FunctionCall action: variant 2, method string, argument bytes, little-endian u64 gas and little-endian u128 deposit. Native Transfer remains variant 3 with unchanged independent vectors.

All four methods use an explicitly signed **100 Tgas**. The fee reserve includes current protocol receipt/function-call/argument-byte costs, storage reserve and gas-price headroom. NEAR does not serialize a maximum gas-price/fee cap into this transaction: the displayed reserve is an estimate, and the adapter rechecks price, full-access authority and nonce immediately before broadcasting.

The signer validates the whole typed plan, authors exact contract/method/arguments/attached-NEAR/gas/effect details, requests confirmation and checks the current app password. Approval is one-use and tied to the stored digest. Both the captured and current journal plans are rechecked after the private prompt. No public arbitrary method, calldata, gas, deposit, raw signature or secret getter is added.

`unstake_all` and `withdraw_all` act on the contract's current entire balance. The amount shown while approving them is explicitly an observation, not a protocol-enforced limit. Each operation requires its own approval. Prepared IDs and explicit cancellation remain available across stale selection callbacks; signed/ambiguous operations cannot be cancelled as unsigned or silently replaced.

## State, withdrawal and trust limits

The read path calls `get_account` with `{"account_id":"<owned account>"}` at the same final block used for the account/access-key query. It requires exact account identity, block hash/height, canonical staked/unstaked integers and a Boolean `can_withdraw`. Missing/null/duplicate/unknown fields, overflow, foreign accounts/blocks and RPC failure cannot become verified zero. Withdraw is enabled only for an observed positive unstaked balance with `can_withdraw`; preparation obtains fresh state again.

The staked balance includes compounded rewards. This view does not distinguish principal, historical deposits/withdrawals and lifetime earnings, so **Rewards** is shown as unavailable separately. Share rounding may leave atomic dust in the unstaked balance. The standard contract applies a four-epoch withdrawal gate; additional unstaking can restart that wait. Neither an exact timestamp nor a fixed 2–3-day duration is promised. Withdrawal is a separate transaction and needs NEAR in the liquid wallet for fees.

Only direct children of mainnet `poolv1.near` / `pool.near`, testnet `pool.f863973.m0`, and the exact local fixture `pool.sandbox` are accepted. Testnet never inherits a mainnet address. Namespace membership and a compatible view are not a contract audit, proof of consensus, validator-election/performance evidence or a promise of rewards. Pool upgrades, commission and provider correctness remain external trust concerns. Live AstroStakers and testnet Aurora state/code were inspected read-only; no mainnet payment was made.

NEAR contracts can commit a root function call while a later asynchronous receipt fails. Staking confirmation therefore checks the complete bounded downstream outcome graph, exact identities and statuses. Root failure is FAILED. A successful root with failed, missing, disconnected or malformed child execution remains UNKNOWN with the original signed bytes and reservation. Matching an amount or a later `get_account` balance cannot clear it heuristically. Reconciliation uses the original transaction hash; automatic replacement is prohibited.

## Requirement-to-test checklist

| Requirement | Status | Evidence and scope |
| --- | --- | --- |
| TP-100 independent serialization/signature parity | PASS | Twelve official near-js 2.5.1 vectors: three account indices × four methods; unchanged native transfer fixtures. `tests/oracles/near/STAKING.md` records generator pins and checksums. |
| TP-100/102 malformed plans/state | PASS | Pool/network/action/deposit/gas/JSON/amount/authority/nonce/fee cases; 10,000 bounded malformed staking-call stress cases. |
| TP-101 private authorization and durability | PASS | Real encrypted signer tests: every allowed method, zero attached unstake/withdraw, password/cancel/replay/digest mutation, exact signed bytes, lost acknowledgment/restart and unresolved reservations. Public coordinator tests cover strict schemas and exact 24-decimal conversion. |
| TP-102 submission/reconciliation | PASS | Four methods × nine original-identity submission/reconciliation cases, plus 104 downstream receipt checks, including root-success/child-failure and incomplete graphs. Provider fixtures are distinct from node acceptance. |
| TP-103 actual local contract cycle | PASS | `reports/near-staking-local.json`: pinned sandbox 2.13.4 plus official staking-pool WASM; stake, partial/all unstake, early-withdrawal refusal, actual epoch advancement, withdraw, independent balance/state, four durable confirmations and original-hash reconciliation. |
| TP-103 actual Wails page | PASS, recorded candidate | `reports/wails-near-staking.json`: actual disposable-profile import, full AstroStakers address, whole-NEAR units, genuine isolated/offline RPC failure, unknown state, inert disabled Withdraw, navigation and clean exit. No RPC server or mocked transaction. This presentation drill does not approve a funded payment. |
| TP-104 critical package coverage | PASS, scoped | Fresh statements: NEAR 96.8%, signer 96.72%, coordinator 96.47%, signer dispatcher 97.48%, native controller 96.4%. These scoped passes do not certify the outstanding full Wails/OS coverage gate. |
| TP-104 semantic mutations | PASS | Eight exact runtime assertions kill method, attached-deposit, network, password, immutable-approval, withdrawal-unlock, downstream-receipt and cyclic-receipt mutants. Baselines pass; compilation failure/timeouts/skips never count as kills. `reports/near-staking-mutation.json` binds source hashes. |
| TP-104 race/vet/fuzz | PASS, bounded | Focused and full affected backend race suites, vet, React/TypeScript checks; staking-policy fuzz ran 10 seconds / 10,984 executions with two workers and no failure. Full soak remains separate. |
| Public-testnet read/funding/stake/unstake | PASS, partial-cycle scope | Actual fresh CSPRNG-root faucet account, verified testnet identity/authority, confirmed 0.01-NEAR stake and unstake_all, and independent pool state. `reports/near-staking-testnet-readonly.json` and `reports/near-staking-testnet.json`. |
| Full funded public-testnet cycle / withdrawal | PARTIAL / withdraw NOT_RUN | The real pool reports `can_withdraw=false`; epoch-gated withdrawal remains outstanding. The same encrypted fixture and original transaction IDs are retained for an explicit later rerun. Local acceptance cannot replace this wait. |
| Actual graphical approval of a funded staking cycle | NOT_RUN | The actual GUI presentation drill and headless real-node signing cycle are separate evidence. |
| Production gates | NOT_RUN / unchanged | Mainnet funded acceptance, clean Ubuntu 24/26 X11/Wayland/lifecycle matrix, complete Wails coverage, 24-hour soak and independent security/legal review. No production-readiness claim. |

## Reproduction

```sh
make test-near-staking
./scripts/with-toolchain.sh npm --prefix tests/oracles/near ci --ignore-scripts
tests/oracles/near/install-sandbox.sh
make test-near-staking-local
make test-near-staking-gui
# Explicit public-testnet-only campaign, separate from ordinary tests/CI:
make test-near-staking-testnet
```

The real local fixture deploys upstream staking-pool 0.4.1 bytecode from `near/core-contracts@1b0436c96379c63aee7d2429c6d5d24019031ab8`; SHA-256 is `454cf4a54ff49a1b144e2591eb4ec833bf3ab5fa4792128beb6d9d0cc8d76e58`. Upstream MIT/Apache license texts accompany the test-only fixture. Validator proposals are paused using the upstream owner's method. This proves real contract accounting and transfers, not election, consensus reward earnings or AstroStakers' exact deployed build. No contract storage/balance is patched. Short local epochs use the node's valid matching transaction-validity setting; production freshness checks remain intact.

The opt-in public campaign uses only the fixed testnet RPC and `aurora.pool.f863973.m0`, verifies the ledger before funding or signing, and creates a fresh CSPRNG root in an owner-marked encrypted disposable profile under `.tools/near-staking-testnet/`. It stakes exactly 0.01 testnet NEAR, limits operation effect amounts to 0.1 and cumulative fee reserves to less than 1 testnet NEAR. Reserves are not actual charged fees. Repeated runs reuse the same wallet, original signed operation identities and encrypted pre-withdrawal balance checkpoint; they never repeat an already confirmed stake. A PARTIAL/BLOCKED report makes the Make target fail rather than declaring full-cycle acceptance. No production profile, supplied phrase or private key can be selected by this harness.

Actual public-testnet observations on 2026-10-28: account `cw-stake-46631b36e27286f56b34.testnet`, stake transaction `CWmYegFzitKnC1QkroaCk6SEZLio2Mz1xeJJjwvtcNTG`, unstake transaction `7pPfBA2VfiEftzcyLNmTWU4Tx9DzHwJY41hjvv6pmnsP`. Both original transactions are CONFIRMED after downstream receipt validation. Final independent pool block `275105175` reports staked `0`, unstaked `10000000000000000000001` yoctoNEAR and `can_withdraw=false`. The extra atomic unit follows contract share rounding. This is **PARTIAL**, not a completed withdrawal or mainnet acceptance. Initial 0.1-NEAR fee-reserve budgeting correctly blocked before signing; the explicit final test budget was raised to less than 1 testnet NEAR while retaining the exact 0.01 stake and separate 0.1 effect limit.

Earlier fixture runs failed on future-head rejection, an invalid short-epoch/validity combination and assumptions about pool-owner fees/share-rounding dust. Those failures were investigated and the fixture corrected; they are not counted as passing acceptance. The successful cycle uses a separate operator and checks conserved delegated value rather than assuming every atomic unit becomes a stake share.

Primary protocol references: [pinned official staking-pool](https://github.com/near/core-contracts/blob/1b0436c96379c63aee7d2429c6d5d24019031ab8/staking-pool/src/lib.rs), [official Action codec](https://github.com/near/nearcore/blob/master/core/primitives/src/action/mod.rs), [official transaction RPC](https://docs.near.org/api/rpc/transactions). The ordinary profile remains `~/.config/cryptowallet/mainnet`; tests never open it.
