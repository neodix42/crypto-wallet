# Independent Exchange steps and Travel Rule response formats

The owner requested freely selectable Exchange steps on 2026-11-03. Deposit,
Funding → Unified, Swap and Withdraw are now navigation buttons. They select a
page without preparing, approving, signing, cancelling or sending anything.
Selecting step 3 does not assert that steps 1 or 2 completed. Navigation stays
available during reads; an active economic approval retains its own context.

## Review field compatibility

Bybit's review states retain their meaning:

| State | Meaning | Original linked deposit eligibility |
|---|---|---|
| `0` or `"0"` | Approved | Other exact-credit checks must still pass |
| `1` or `"1"` | Counterparty information required | Blocked |
| `2` or `"2"` | Under review | Blocked |
| `3` or `"3"` | Rejected | Blocked |

Numeric strings are normalized losslessly. There is no truthiness conversion
or unknown-to-approved fallback. Missing remains optional under the existing
Bybit contract; explicit null, empty, wrong-type, unknown-value and case-shadow
responses remain unclassified and receive fixed `DEPOSIT_RECORD_REVIEW_*`
reasons. The old generic REVIEW code did not reveal which representation this
private account returned. No real profile, key, secret or authenticated Bybit
response was inspected during development.

The [primary API contract](https://bybit-exchange.github.io/docs/v5/asset/deposit/deposit-record)
defines the numeric states. Supporting canonical strings preserves those exact
values rather than inventing additional approval states.

## Current balances, separate actions

1. **Deposit:** select an owned source wallet and asset, refresh the verified
   Bybit destination/memo and enter a new amount. Each explicit Deposit creates
   a separate original on-chain operation. Old operations and workflow metadata
   remain intact. Any unresolved payment for that account blocks another
   deposit before preparation; it is reconciled by its original identity.
2. **Funding → Unified:** refresh the selected coin's safe account balances and
   choose an affordable amount. This explicitly moves existing custody funds
   between the same user's Bybit accounts. It does not claim that a particular
   deposit passed Travel Rule review. Fresh account mode, permission, balance,
   exact consent/password and durable-transfer checks apply. After completion,
   balances are refreshed read-only.
3. **Swap:** refresh supported pairs and current market rules. The action spends
   only current Unified funds; it cannot silently move Funding funds. The old
   workflow's transfer amount does not limit an independently approved trade.
   Fresh lot/minimum/precision, safe balance, price/slippage, fee and spot-only
   checks remain. A confirmed original trade retains its actual net output;
   visiting Withdraw selects a supported owned output asset and rounds its
   default debit down to network precision.
4. **Withdraw:** choose the asset and an offered network/owned wallet in the
   selected account group. Go supplies the receiving address. The debit uses
   currently available custody funds and current withdrawal limits/fees.
   Any required Unified → Funding transfer still has separate exact approval.
   Existing funds can be withdrawn without a fabricated previous swap ID.

All economic actions retain their separate current-password approval. The
server's safe availability and account restrictions remain authoritative; the
app does not clear a compliance hold. Explicit pending/rejected review is never
relabeled approved. Available existing funds are a separate source of authority
from a claim that one original deposit was credited.

## Durability and recovery

Visible step is separate from saved economic chronology. No vault or wizard
schema migration is required. Original signed bytes, nonce reservations,
credential/endpoint bindings and encrypted wallet/exchange records remain.
The signer refuses a replacement while an exchange request is unresolved.
Restored remote pending operations stay visible for original-ID reconciliation.
Unsigned PREPARED/AUTHORIZED remote requests may be cancelled only through an
explicit exact-summary confirmation with state/session/digest rechecks; UNKNOWN
and SUBMITTING requests cannot be cancelled or replaced.

Read-only tracking follows current-session free operation IDs, never creates
another action, and rejects stale profile/provider/generation/state results.
It does not extend the unlock timeout. New current-session deposits avoid the
automatic pending inbox while being tracked. On a fresh restart, independent
wallet deposits remain in wallet history and the existing original-payment
recovery mechanism; no unsupported persistent Exchange correlation is claimed.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | 160 frontend checks and strict TypeScript; clickable step IDs, keyboard semantics, no inferred completion or form submission, economic IDs kept inert in navigation. `reports/exchange-free-navigation-frontend.log`, `reports/exchange-free-navigation-typecheck.log`. |
| PASS | Client tests/races/vet for all eight valid numeric review representations and unknown/null/empty/type/shadow refusals. No raw provider value enters diagnostics. |
| PASS | New independent backend, actual signer approval/persistence/UNKNOWN protection/cancellation and strict mode tests; full core/walletsigner tests, affected races/vet. `reports/exchange-free-backend-all-final.log`, `reports/exchange-free-backend-race-final.log`, `reports/exchange-free-backend-vet-final.log`. |
| PASS | All 16 independent controller cases, complete native tests/races, asynchronous original-ID tracking, fresh post-transfer pools, net-output withdrawal defaults and explicit unsigned cancellation. `reports/exchange-free-native-complete-final.log`, `reports/exchange-free-native-race-complete-final.log`, `reports/exchange-free-all-current.log`, `reports/exchange-free-contract-final.log`. |
| PASS | Complete `make test-ci`, `make test-security-ci`, affected full races including signer, vet and portable build. All 81 semantic mutants in ten campaigns killed with stable sources. `reports/exchange-free-ci.log`, `reports/exchange-free-security.log`, `reports/exchange-free-race.log`, `reports/exchange-free-vet.log`, `reports/exchange-free-build.log`. |
| PASS | Six actual stable-source X11 checks, including new Swap → Funding → Withdraw → Deposit clicks without credentials, invented address or economic request. Binary SHA256 `0b79e465711a59b897f2c5ccedfcd10069ebaefe21849bca235f1bbadc927fd4`. `reports/exchange-free-appearance.json`. |
| NOT_RUN | Exact-source hosted campaigns, publication and publisher verification until recorded. |
| NOT_RUN | Live authenticated Bybit review/deposit/transfer/trade/withdrawal acceptance, physical OS/Wayland matrix and independent external review. No user secrets or automatic mainnet funds are used. |
