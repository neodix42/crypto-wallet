# Bybit acknowledgements and compact transaction history

The reported transfers and swap had provider acknowledgement IDs, but the
coordinator replaced PENDING with UNKNOWN whenever an immediate read could not
establish the exact result. The foreground UI then mislabeled that uncertainty
as Operation failed. A successful create response and a verified completed
transfer/order are separate observations; Bybit's order creation is asynchronous.

A second path promoted a failed wizard-state reload into the operation's error,
even after the action had returned an acknowledged or confirmed result. That
reload now produces a sanitized read warning while retaining the returned result
and original ID. Real action errors, lost acknowledgements and lock/session
failures keep their error paths; stale callbacks still cannot apply.

The original acknowledged request now remains PENDING through missing, delayed,
unavailable, malformed or mismatched follow-up records. Its ID and immutable
payload remain stored. Background reads check that same identity; they do not
resubmit or replace it, and independent current-funds wizard actions remain
available. Older UNKNOWN records with an acknowledged ID can recover to PENDING
through a read-only check. A fixed public read-stage code retains the diagnostic
without exposing provider bodies or credentials.

Completion still requires exact identity, direction, quantity/unit and, for spot
orders, executed amounts and fee checks. A 5 USDT Buy budget remains a quote-coin
quantity, while gross received GRAM and the GRAM trading fee are separate values.
An inconsistent record never certifies success. Balances alone never certify an
operation. An acknowledgement lost before its ID was stored remains UNKNOWN;
the UI calls that Submission status unknown. Genuine explicit refusals still
open their reason dialog, including a rejection after initial acknowledgement.

See Bybit's primary contracts for [asynchronous order creation](https://bybit-exchange.github.io/docs/v5/order/create-order),
[order records](https://bybit-exchange.github.io/docs/v5/order/realtime),
[executions](https://bybit-exchange.github.io/docs/v5/order/execution) and
[internal transfer records](https://bybit-exchange.github.io/docs/v5/asset/transfer/query-inter-transfer).
No authenticated response from the reported account was inspected, so this
diagnoses the proven acknowledgement-loss bug without claiming its private
follow-up response shape or cause.

Recent withdrawals show one canonical lowercase status word. The adjacent Info
icon exposes the complete safe reason, code, raw status, source, original IDs
and timestamps on hover, keyboard focus or click. Unknown values stay unknown.
These disclosures cannot authorize a transfer or retry.

Every wallet's Transaction history uses an external-link icon in Explorer cells,
with the URL on hover and the existing fixed-network URL checks before opening.
Source/destination addresses longer than twelve characters show their first six,
` . . . ` and last six characters. Full addresses remain available on hover and
to assistive technology. Unknown peer labels and short named accounts remain
intact. Actual blockchain comments preserve their exact text but occupy one
visual line, with ellipsis and complete text on hover. Backend records, exact
approval addresses and receive-address copying are unchanged.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | Coordinator matrix for Funding→Unified, Unified→Funding, spot and withdrawal: missing/transient/malformed/foreign follow-up, durable PENDING/ID, later exact proof with one POST only, legacy recovery and truly lost acknowledgement. Reported 5 USDT Buy units and gross/net fee arithmetic are checked; inconsistent converted quantities are refused. |
| PASS | Complete Bybit client/coordinator/IPC tests, focused races and vet. Current documented fee-detail and exact transfer schemas are checked without relaxing production parsing. `reports/bybit-ack-core-all-final.log`, `reports/bybit-ack-race.log`, `reports/bybit-ack-vet.log`. |
| PASS | All 185 frontend tests and TypeScript checks: compact peers across networks, exact comment text/escaping, icon-only safe Explorer routing and one-word withdrawal status/accessible Info. `reports/transaction-table-frontend-literal-final.log`, `reports/transaction-table-typecheck-literal-final.log`. |
| PASS | Native foreground regressions distinguish acknowledged pending/legacy results, actual post-acknowledgement refusals and lost-ack uncertainty. Twelve reload-feedback subcases and their races retain accepted results through a failed later read, while action/lock errors and stale callbacks remain protected. `reports/exchange-followup-feedback-native.log`, `reports/exchange-followup-feedback-race.log`. |
| PASS | CI race routing command/failure tests retain all 43 discovered current packages exactly once, including uncached stress, with serial execution and an explicit 30-minute cumulative package deadline. Timeout-style failure remains failure; no KDF, individual test, protocol, stress or mutation budget is weakened. |
| PASS | Final complete `make test-ci`, `make test-security-ci`, affected native/core/client/IPC races, vet and portable build. All 81 mutants in ten stable-source campaigns are killed; 7,703 hash comparisons match 1,688 current files. `reports/bybit-ack-test-ci-final.log`, `reports/bybit-ack-security-final.log`, `reports/bybit-ack-local-security-summary.json`, `reports/bybit-ack-race-final.log`, `reports/bybit-ack-native-race-final.log`, `reports/bybit-ack-vet-final.log`, `reports/bybit-ack-build-final.log`. |
| PASS | Six actual offline GUI checks on the final built executable, including theme/restart/Enter unlock and Exchange navigation, all five stablecoin summaries, independent selections and recent-request guidance. Stable source fingerprint `089d956023b272faad2b5edcaff6e2726362afd39fc6c176f024e17489ba7c41` and executable SHA256 `d9aedc14a21a8d67614da196abffe70c093fd2a5e8e01c05c35d95754349e618` match the current candidate. `reports/bybit-ack-appearance-final.json`. Positive authenticated withdrawal rows and funded Bybit actions are not fabricated. |
| NOT_RUN | Exact-source hosted test/build/release verification until this candidate is committed and its jobs/artifacts complete. |
| NOT_RUN | Authenticated funded Bybit acceptance and the reported private account's response diagnostics. No real user profile, secrets or automatic mainnet funds are used. Physical installed OS matrix and independent external review remain separate gates. |

The preceding `a6760cd` candidate's Wails job and release 0.1.54 passed. Its
backend job failed at Go's ten-minute cumulative signer race timeout during
production Argon2 work, with no reported data race/assertion failure. Later gates
in that job did not run; the partial artifact is not whole-suite PASS evidence.
The serial race scheduling/package deadline fix preserves the full suite.
