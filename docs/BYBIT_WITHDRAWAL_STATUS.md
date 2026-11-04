# Bybit session approval and withdrawal visibility

The 2026-11-04 owner update removes the repeated Bybit operation password input.
An unlocked, recovery-confirmed session and an exact Confirm dialog authorize
each deposit, own-account transfer, spot order or owned-wallet withdrawal.
Lock, cancellation, expiry, changed credentials, modified plans and stale
sessions deny authorization. Every write retains a distinct durable identity
and one-use approval. Other providers, ordinary wallet sends, recovery and
Disconnect keep their existing authentication policies.

An on-chain Bybit deposit uses a private prepare/sign capability bound to its
exact journal digest, session owner, vault generation, selected Bybit endpoint
and credential. It is consumed before confirmation and is never persisted or
exposed as a caller-selected skip-password flag. The coordinator checks the
authenticated destination, network and memo again before preparation. Original
nonce reservations, signed bytes and submission/recovery rules still apply.

## Automatic observations

Changing the deposit network or asset invalidates its displayed destination and
loads the selected address automatically. Connection/status refresh is automatic.
The duplicate Funding/Unified refresh buttons and wizard Refresh Bybit status
button are removed; the common Refresh balances control remains. Reads do not
block navigation, renew user activity, prepare a payment or reset entered drafts.
Stale profile, credential, selection and generation replies are discarded.

## Recent withdrawals

Step 5 displays up to ten recent local attempts and authenticated Bybit on-chain
withdrawal records, including requests made directly in Bybit. Date/time, asset,
network, amount/fee basis, destination, status, safe reason, request ID and
transaction ID remain separate. Rows cannot confirm, retry or cancel a request.
The bounded provider scan covers its default recent 30-day scope; unavailable
and incomplete history stay explicit. Refreshes are read-only and automatic.

Local rows are combined with provider records only through an already stored
acknowledgment ID and exact coin/chain/address/tag. A lost acknowledgment is
never matched by amount, address or time. Missing records do not establish
failure. Provider-reported amounts are not invented recipient-credit equations.
A completed exchange request does not independently prove on-chain receipt.

Previously, a definitive refusal could return a normal result while the free
wizard hid its details. A withdrawal could therefore appear to finish without
showing the refusal or a useful current status. Foreground FAILED outcomes now
produce persistent safe-reason dialogs and remain in history. The subsequent
[acknowledgement correction](BYBIT_ACKNOWLEDGEMENTS_HISTORY.md) keeps known
accepted requests PENDING through unverifiable follow-up reads, without a false
failure dialog. A lost acknowledgement remains UNKNOWN with an explicit
Submission status unknown dialog.
Bybit codes 131093, 131098 and 131089 are terminal preacceptance refusals only
for withdrawal operations; transport failures, lost acknowledgments and other
unclassified codes remain UNKNOWN. No request is automatically resent.

## Address-book and withdrawal locks

The authenticated [address-list API](https://bybit-exchange.github.io/docs/v5/asset/withdraw/withdraw-address)
exposes the selected entry's presence, verification and new-address lock. It
requires withdrawal permission. The application checks the exact address,
chain and tag, including universal entries and bounded pagination. It exposes
no unrelated remarks or address-book data. A failed or truncated scan cannot
establish that an address is absent.

The API's status 1 identifies a new-address 24-hour restriction; verified is a
separate 0/1 field. Its response does not document the account's global whitelist
switch or a reliable remaining countdown. The UI therefore distinguishes
listed/verified/locked/not listed/unavailable without inventing those properties.
Bybit requires the same address spelling as the saved entry; equivalent TON
friendly-address encodings cannot silently replace that exact string.

A newly added destination may be restricted when the account's New Address
Withdrawal Lock is enabled. This is a possible explanation for a new-address
failure, not proof about the reported account. Only its authenticated address
status, withdrawal record or exact API refusal can establish that condition.
[Official security guidance](https://www.bybit.com/en/help-center/article/How-to-Manage-Your-Withdrawal-Security),
[withdrawal records](https://bybit-exchange.github.io/docs/v5/asset/withdraw/withdraw-record)
and [error definitions](https://bybit-exchange.github.io/docs/v5/error).

## Evidence checklist

| State | Scope |
|---|---|
| PASS | Focused actual signer/session/deposit tests, including real EVM/TON signatures, exact memo, one-use approval, lock/cancel/expiry/credential/owner/generation/plan failures and private-only dispatch. Normal wallet Send still requires its password. |
| PASS | Focused client/coordinator history/whitelist tests and races: newest ten, safe pagination, local rejection retention, exact acknowledgment joins, no lost-ack guessing, manual requests, universal entries, case/tag identity, locked/unverified/unknown and failed/truncated scans. |
| PASS | Focused native tests and 166 frontend tests: refusal dialogs, original IDs, inert bounded history, planned versus reported values, exact destination status and stale read rejection. |
| PASS | Complete local `make test-ci`, `make test-security-ci`, portable build and vet. All 81 compiling semantic mutants in ten stable-source campaigns are killed. 166 frontend checks pass. `reports/bybit-withdraw-ci-final.log`, `reports/bybit-withdraw-security-final.log`, `reports/bybit-withdraw-build-final.log`, `reports/bybit-withdraw-vet.log`, `reports/bybit-withdraw-frontend-final.log`. |
| PASS | Focused actual offline GUI and six-check appearance/restart campaign verify automatic deposit guidance, removed refresh controls, all five balance summaries, stablecoin selectors and the recent-request section without fabricated credentials or provider records. The final presentation rerun is recorded separately. `reports/bybit-withdraw-gui.json`, `reports/bybit-withdraw-appearance.json`. |
| PASS | Complete final races across native controller, coordinator, signer, signer dispatch, vault, Bybit client and IPC, plus six final actual GUI/appearance checks. The GUI fingerprint and executable hash match the current built candidate; all 7,680 mutation source-hash comparisons match 1,684 current source files. `reports/bybit-withdraw-race-final.log`, `reports/bybit-withdraw-appearance-final.json`, `reports/bybit-withdraw-local-security-summary.json`. |
| PASS | Exact-source Wails job 126411912195 in [hosted tests 41780885454](https://github.com/neodix42/crypto-wallet/actions/runs/41780885454), with independently verified nine-report artifact 13299679065: 38 full GUI, five TON, seven recovery and nine actual private-geth checks. |
| FAIL | Backend job 126411912433 in that run: ten-minute cumulative signer race timeout during production Argon2 work. No data race/assertion failure was reported. Partial artifact 13300381749 verifies 81 mutants and 1,717 exact-Git source bindings; it is not full-backend PASS evidence. Later interop/integration/fuzz/dependency/recovery/crash gates were NOT_RUN. `reports/bybit-withdraw-hosted-checks/partial-verification.json`, `failed-race.log`. |
| PASS | [Build/release 41780885403](https://github.com/neodix42/crypto-wallet/actions/runs/41780885403) and normal [release 0.1.54](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.54) for `a6760cd480ed6bef6f58f092af6b19970bc26dbc`: publisher, repository/source/tag/app version, four-file portable archive and published bytes verified. Archive SHA256 `42577c2f13c02ebe6a8d3b8965e85e6a3a28c17b643757865dfa031487c26971`. `reports/bybit-withdraw-release/summary.json`. |
| PASS | Three actual offline GUI checks on the authenticated downloaded executable: stable sources, all five stablecoin tables and six selectors, removed refresh controls, automatic deposit guidance and recent-withdrawals section. `reports/bybit-withdraw-release/published-summary.json`, `reports/bybit-withdraw-published.json`. No successful authenticated Bybit response or funded action is fabricated. |
| NOT_RUN | Authenticated funded Bybit acceptance, the reported account's private cooldown/rejection reason, physical installed OS matrix and independent external review. No real profile or credentials are accessed and no mainnet transaction is automatically sent. |

During development, the historical whole-signer-suite locked-signing mutant
selector exceeded its 110-second budget while sources were changing; that result
remains INVALID and is not counted as killed. Its replacement runs an explicit
unmodified passing locked-session control and requires that exact runtime
assertion to fail under the same guard mutation. The full signer suite remains
required separately. The final complete 81-mutant campaign passes with stable
sources; timeouts and compilation errors cannot count as kills.
