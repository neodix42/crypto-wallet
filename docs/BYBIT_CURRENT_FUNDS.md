# Bybit actions from current available funds

The 2026-11-03 owner update makes steps 2–5 independent of older exchange
operation outcomes. A recorded UNKNOWN transfer no longer greys out an
otherwise affordable Funding move, spot order, return to Funding or withdrawal.
The wizard shows available balances and input rules, without an original-deposit
status, old-operation cards, waiting-for-original message or manual Check button.

The earlier controller only polled operations created in the same UI session,
while its global pending check included restored records. That mismatch could
leave both buttons disabled after restart even when Bybit showed 5 GRAM in
Unified. The exact reported form (5 GRAM at 1.176 USDT, 0.5% slippage) passes the
current synthetic 0.01-lot/5-USDT-minimum fixture; stale/foreign quotes, invalid
precision, insufficient current funds and actual venue limits still refuse.

The coordinator reads fresh safe balances, permissions and applicable current
market/withdrawal rules before each new action. A private current-funds prepare
method binds its narrow Bybit/no-wizard mode into the encrypted operation digest.
Each action obtains a fresh ID, exact summary and current-password approval.
No earlier ID, signed request, approval, payload or outcome is replaced. An
UNKNOWN request may still settle later; another approved action is a new action,
not a retry or cancellation. Legacy preparation guards, active authorization
serialization and on-chain nonce/payment recovery rules remain.

Read-only tracking resumes persisted original IDs with profile/connection/state
guards and fair polling. It makes no funds-moving request and no longer controls
button eligibility. Refreshed observations update available balances without
renewing the market timestamp. Untouched zero suggestions follow new funds;
entered or cleared amounts remain unchanged by background reads. An expired
quote explains why market refresh is needed. Failed/partial balance observations
cannot retain unobserved spendability.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | Reported GRAM values reproduced with production JSON field names; Buy/Sell, precision/minimum/slippage, expiry/identity and actual spending-coin balance tests. `reports/exchange-disabled-gram-focused.log`. |
| PASS | Initial native controller suite after the visible current-funds change; automatic persisted-status tracking, no manual checks, zero-to-five suggestions and edited/cleared drafts. `reports/exchange-current-funds-native-third.log`. |
| PASS | Real signer tests allow distinct new GRAM actions while old records remain UNKNOWN/PENDING/SUBMITTING, without changing or replaying them. Fresh balance/venue refusals, exact passwords, current credentials/generations, policy/digest mutation, private-only dispatch and legacy digest compatibility all pass. `reports/exchange-current-funds-signer-focused.log`, `reports/exchange-current-funds-core-all.log`, `reports/exchange-current-funds-dispatch-vault.log`. |
| PASS | Complete local `make test-ci`, `make test-security-ci`, full affected-package races, vet and portable build; all 81 mutants in ten stable-source campaigns killed. Six actual stable-source X11 checks pass. `reports/exchange-current-funds-ci.log`, `reports/exchange-current-funds-security.log`, `reports/exchange-current-funds-complete-race.log`, `reports/exchange-current-funds-build.log`, `reports/exchange-current-funds-appearance.json`. |
| NOT_RUN | Exact-source hosted CI, publisher and downloaded-release checks until the next push is independently verified. |
| NOT_RUN | Authenticated funded Bybit actions, public funded transfers, physical installed OS matrix and independent external review. No real profile, credentials or automatic mainnet funds used. |
