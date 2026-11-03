# Independent Exchange steps and Travel Rule response formats

The owner requested freely selectable Exchange steps on 2026-11-03. Deposit,
Funding → Unified, Swap, Unified → Funding and Withdraw are navigation buttons. They select a
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
3. **Swap:** refresh reviewed wallet-supported USDT/USDC pair candidates and current market rules. Both **Buy** and **Sell** are offered only for actual verified spot listings, such as GRAMUSDT or ETHUSDT. Unrelated blockchain network metadata is not part of spot discovery, quote or order authorization. The action spends
   only current Unified funds; it cannot silently move Funding funds. The old
   workflow's transfer amount does not limit an independently approved trade.
   Fresh lot/minimum/precision, safe balance, price/slippage, fee and spot-only
   checks remain. A confirmed original trade retains its actual net output;
   visiting step 4 or 5 selects a supported owned output asset and rounds its
   suggested amount down to network precision.
4. **Unified → Funding:** refresh balances and explicitly approve a fixed
   `UNIFIED` → `FUND` transfer. The amount must fit current safe Unified
   availability. Its durable ID, exact approval and status are separate from
   the trade and withdrawal. After confirmation both pools are refreshed.
5. **Withdraw:** choose the asset and an offered network/owned wallet in the
   selected account group. Go supplies the receiving address. Only currently
   available **Funding** funds cover the inclusive debit and fee; Unified
   availability is never counted or moved implicitly. Current route identity,
   network precision, minimum and fees remain required. Existing funds can be
   withdrawn without a fabricated previous swap ID.

All economic actions retain their separate current-password approval. The
server's safe availability and account restrictions remain authoritative; the
app does not clear a compliance hold. Explicit pending/rejected review is never
relabeled approved. Available existing funds are a separate source of authority
from a claim that one original deposit was credited.

## Durability and recovery

Visible step is separate from saved economic chronology. No vault or wizard
schema migration is required. Legacy saved Step 4 still means Withdraw and
Step 5 still means completed; they map to visible step 5 and step 1 respectively,
without changing any saved economic state. Original signed bytes, nonce reservations,
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

The [spot instrument API](https://bybit-exchange.github.io/docs/v5/market/instrument)
provides current listing and order rules; reviewed candidate symbols do not
replace it. Explicit transfers use the
[internal transfer API](https://bybit-exchange.github.io/docs/v5/asset/transfer/create-inter-transfer).
Step 5 creates a [withdrawal](https://bybit-exchange.github.io/docs/v5/asset/withdraw)
with Funding as the source.

## Five-step acceptance checklist

| State | Scope |
|---|---|
| PASS | 160 frontend tests, including five independent navigation controls and inert malformed/economic IDs. Focused native tests cover reverse transfer, Funding-only limits, original-ID tracking, old-step mapping and read-only spot flags. `reports/exchange-five-frontend.log`, `reports/exchange-five-native-focused.log`. |
| PASS | Complete aggregate `make test-ci` after the final event-guard/test-fixture correction; complete affected races including signer, final native races, vet and portable build. `reports/exchange-five-ci-final2.log`, `reports/exchange-five-race-complete.log`, `reports/exchange-five-native-race-final.log`, `reports/exchange-five-vet-complete.log`, `reports/exchange-five-build-final.log`. |
| PASS | Six actual stable-source X11 checks. All five pages are clicked in order 3 → 2 → 4 → 5 → 1, without credentials or a financial request. `reports/exchange-five-appearance-complete.json`. |
| PASS | Actual unauthenticated Bybit spot-instrument GETs for GRAMUSDT, ETHUSDT, ETHUSDC and USDCUSDT returned exact Trading listings and current rules. `reports/exchange-five-public-instruments.json`; this is no claim of authenticated funded acceptance. |
| PASS | Complete security suite with all 81 semantic mutants killed across ten source-stable campaigns. 4,125 recorded source hashes were independently compared. `reports/exchange-five-security-complete.log`, `reports/exchange-five-local-summary.json`. |
| PASS | Fixed the hosted GUI locator's address crop, which previously included part of the adjacent Copy icon. All 220 current-UI helper tests (23 focused locator cases), a nine-copy graphical clipboard campaign and the full 38-check generated-root campaign pass. Exact clipboard equality, private-pixel guards and independent eight-wallet phrase recovery remain required. `reports/exchange-five-clipboard-fix.json`, `reports/exchange-five-full-gui-fix.json`. |
| PASS | Final hosted Wails job for `17d2a32` in [tests 41634158286](https://github.com/neodix42/crypto-wallet/actions/runs/41634158286): all nine source/binary-bound reports, 38 full UI, five TON, seven recovery and nine actual private-geth checks. Artifact 13230848136 SHA256 `594ce49edfbe0bd0b113014e421b2ee7a4e31ac4206a075c9f5f6a727b7ca399`. `reports/exchange-navigation-hosted-gui/summary.json`. |
| PASS | All 227 current-UI helper tests, including seven directional-stepper cases. The actual step bar is bounded by its unique page/source anchors, matches both directional words, and rejects reversed/ambiguous/out-of-section text. Hosted and published checks click all five steps without depending on circled-number OCR. `reports/exchange-direction-helper-all.log`, `reports/exchange-direction-appearance.json`. |
| PASS | [Build/release 41634158316](https://github.com/neodix42/crypto-wallet/actions/runs/41634158316) and normal [release 0.1.50](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.50): exact source `17d2a32a7e86becca0e2d53d8097e2f9d1e12657`, publisher/tag/version/inventory and byte integrity verified. Archive SHA256 `fab860270c9235ecba951314abf2e8b130fbbce18914a71a4305b112db85d31b`. The authenticated downloaded executable passed six actual stable-source X11 checks; binary SHA256 `47e9166f2f05ba49e508aa5849a6df1d4ce29586cf04040d9ee631a4687ab2cf`. `reports/exchange-navigation-release/summary.json`, `reports/exchange-navigation-published.json`. |
| PASS | Both jobs in [tests 41634158286](https://github.com/neodix42/crypto-wallet/actions/runs/41634158286) succeeded for `17d2a32`. Independently verified backend artifact 13231532327 SHA256 `495b32956566a168f3dd6b235e92ee8875fbe36247c50d7b9e32d20d641f92d0`: all 30 JSON gates, 81 mutants/ten campaigns, 26 measured fuzz targets, 13 local integrations and 24 recovered descriptors/eight networks. All 8,870 hash comparisons matched 1,692 unique source files, with zero mismatches. `reports/exchange-navigation-hosted-checks/verification.json`, `reports/exchange-navigation-hosted-checks/summary.json`. |
| NOT_RUN | Authenticated real Bybit deposits, transfers, trades and withdrawals; public funded acceptance, physical installed Ubuntu/X11/Wayland matrix and independent external security review. No user credentials or automatic mainnet funds used. |

The earlier hosted run for `3d9f5e8` failed at an address-selection OCR locator
before Exchange and its backend job was cancelled during integrations after the
helper fix was pushed. It is not a complete hosted PASS. The next `61ee558`
GUI run passed 25 checks and then failed at a bare `4` OCR lookup. Its complete
backend artifact independently passed all 30 gates, but that overall workflow
remains FAIL. The directional-label fix in `17d2a32` preserves exact post-click
page checks, private-pixel guards and all recovery/copy assertions. A separate local
simultaneous-display start and one private phrase OCR observation were also
rejected; they are preserved as failed observations. The accepted fresh full run
retained every word/ordinal/checksum and exact-copy assertion.

Earlier aggregate fixtures which expected the removed Bybit controls were updated
to exercise the shared direct UI through Binance. Separate five-step tests retain
Bybit authority, limits and stale-event refusals. Earlier source-changing graphical
and mutation runs have `sourceStable:false` and are not counted as final evidence.

## Historical four-step evidence (source `18462f5`)

These results precede the current five-step amendment and do not certify its
new code.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | 160 frontend checks and strict TypeScript; clickable step IDs, keyboard semantics, no inferred completion or form submission, economic IDs kept inert in navigation. `reports/exchange-free-navigation-frontend.log`, `reports/exchange-free-navigation-typecheck.log`. |
| PASS | Client tests/races/vet for all eight valid numeric review representations and unknown/null/empty/type/shadow refusals. No raw provider value enters diagnostics. |
| PASS | New independent backend, actual signer approval/persistence/UNKNOWN protection/cancellation and strict mode tests; full core/walletsigner tests, affected races/vet. `reports/exchange-free-backend-all-final.log`, `reports/exchange-free-backend-race-final.log`, `reports/exchange-free-backend-vet-final.log`. |
| PASS | All 16 independent controller cases, complete native tests/races, asynchronous original-ID tracking, fresh post-transfer pools, net-output withdrawal defaults and explicit unsigned cancellation. `reports/exchange-free-native-complete-final.log`, `reports/exchange-free-native-race-complete-final.log`, `reports/exchange-free-all-current.log`, `reports/exchange-free-contract-final.log`. |
| PASS | Complete `make test-ci`, `make test-security-ci`, affected full races including signer, vet and portable build. All 81 semantic mutants in ten campaigns killed with stable sources. `reports/exchange-free-ci.log`, `reports/exchange-free-security.log`, `reports/exchange-free-race.log`, `reports/exchange-free-vet.log`, `reports/exchange-free-build.log`. |
| PASS | Six actual stable-source X11 checks, including new Swap → Funding → Withdraw → Deposit clicks without credentials, invented address or economic request. Binary SHA256 `0b79e465711a59b897f2c5ccedfcd10069ebaefe21849bca235f1bbadc927fd4`. `reports/exchange-free-appearance.json`. |
| PASS | Prior-source hosted tests [41619154765](https://github.com/neodix42/crypto-wallet/actions/runs/41619154765), backend artifact 13224099575 SHA256 `606ee4aeec41d131d4691e8cea7f47032ae3724ea22f8a998232813ad0d38e7e`: all 30 gates, 81 mutants, 26 fuzz targets, 13 integrations and 24 descriptors. Prior [release 0.1.47](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.47) and downloaded-binary acceptance also passed. These certify only source `18462f5`. |
| NOT_RUN | Live authenticated Bybit review/deposit/transfer/trade/withdrawal acceptance, physical OS/Wayland matrix and independent external review. No user secrets or automatic mainnet funds are used. |
