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
showing the refusal or a useful current status. Foreground FAILED/UNKNOWN
outcomes now produce persistent safe-reason dialogs and remain in history.
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
| NOT_RUN | Exact-source hosted CI and publisher/downloaded-release verification for this amendment. |
| NOT_RUN | Authenticated funded Bybit acceptance, the reported account's private cooldown/rejection reason, physical installed OS matrix and independent external review. No real profile or credentials are accessed and no mainnet transaction is automatically sent. |

During development, the historical whole-signer-suite locked-signing mutant
selector exceeded its 110-second budget while sources were changing; that result
remains INVALID and is not counted as killed. Its replacement runs an explicit
unmodified passing locked-session control and requires that exact runtime
assertion to fail under the same guard mutation. The full signer suite remains
required separately. The final complete 81-mutant campaign passes with stable
sources; timeouts and compilation errors cannot count as kills.
