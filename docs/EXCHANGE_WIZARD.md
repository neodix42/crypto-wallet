# Bybit wizard and deposit destination

The owner requested a sequential Deposit → Funding/Unified → Spot swap →
owned-wallet withdrawal workflow on 2026-11-02. Bybit opens this wizard by
default. Binance/KuCoin keep their existing provider-specific controls and
capability restrictions. Manage existing Bybit funds retains independent
controls for pre-existing custody balances and original-operation recovery.

## Displayed deposit destination

Refresh market / deposit address obtains authenticated coin/network metadata
and the selected master account's deposit address. The page displays the
destination, exact memo or explicit absence, provider chain, minimum and
observed wallet balance. It displays no invented address when disconnected or
when the read fails. Copy uses the displayed public address directly.

Two overly strict checks caused the reported rejection: coin-info and address
responses can use different reviewed display aliases for the same exact chain;
TON unique deposit addresses can explicitly have an empty memo. Both responses
must still identify the reviewed network, use the same provider
chain and corroborate the full token contract. Missing/null memo data is not
an explicitly empty memo. Full TON masters compare canonical binary addresses;
their friendly representation is case sensitive. Bybit's six-character token
suffix can only corroborate the full authenticated coin-info contract.

Coin-info originally rejected the complete coin whenever any of its network
rows had incomplete limits or confirmation metadata. Invalid rows now remain
marked and unusable, without blocking a different fully checked network; they
remain included in ambiguity checks. A deposit-specific metadata read does not
require withdrawal-only fees/limits and never invents zero or unlimited values.
Withdrawal still obtains the strict current withdrawal metadata. Fixed field
categories identify missing/invalid precision, limits, confirmations or flags
without exposing response bodies or credentials. HyperEVM's explicit aliases
include HYPEREVM and, only under Bybit's exact HYPEREVM service code, older
Hyperliquid/HypeEVM display names. HyperCore cannot use those aliases.

These reproduce local failure classes consistent with the owner's ETH/Arbitrum
and HyperEVM reports; the user's private response is not inspected. Authenticated
acceptance for that account remains NOT_RUN until separately exercised.

Deposit automatically uses the shown destination. Its two-minute UI preview is
bound to the selected source account and asset. Go obtains fresh metadata and
address immediately before preparing; a changed address or memo returns
EXCHANGE_DEPOSIT_DESTINATION_CHANGED with no prepared operation, signature or
submission. Failures distinguish network, contract, address and memo. Existing
exact fee/reserve checks, approval, current password and signing remain.

## Four steps

1. Deposit an exact native/token amount to the verified Funding destination.
   After its initial exact approval/password, the wallet signs/sends once and
   automatically checks the original operation. Pending confirmations do not
   reopen a dialog. A confirmed chain receipt advances to step 2; this alone
   never authorizes spending exchange funds. TON checks its linked sender and
   recipient transaction hashes, distinct from the signed external-message ID.
2. Inspect the deposited asset and fresh transferable/safe Funding and Unified
   balances. Exact Bybit credit is checked automatically on this page: matching
   transaction, network, token, destination and memo, successful normal credit
   and no applicable pending Travel Rule review. The region-specific Travel
   Rule field may be absent according to Bybit's contract; an explicit pending
   or denied value still blocks spending. Missing mandatory credit fields,
   duplicates and cursor loops fail closed. The transfer remains disabled until
   verified credit. Its default and ceiling conservatively subtract a nonempty
   reported fee from the smaller of original/reported amounts. Raw amount and
   fee remain separate; ambiguous gross/net semantics can leave a remainder,
   and an empty fee is labelled unreported rather than claimed zero.
   Explicitly
   approve an exact FUND → UNIFIED internal transfer within the deposit budget.
   Only the original transfer's verified completion and sufficient current
   Unified balance permit the next step. Bybit defaults deposits to Funding,
   but a user's global auto-transfer preference may move them elsewhere. The
   wallet explains insufficient Funding and does not change that global setting.
3. Discover actual active spot pairs with supported owned-wallet withdrawal
   routes. Nonstable assets sell into available USDT/USDC pairs. Deposited
   stablecoins can buy supported assets, including the supported direct
   stablecoin pair. There is no invented listing or implicit multi-hop route.
   Fresh price, precision, lot/minimum/maximum, safe balance and slippage checks
   precede exact approval. The spending ceiling is the amount moved by this
   workflow. Complete execution pages and received-asset fees determine net
   output, including partial terminal fills. A resumed completed order rereads
   only its original order/fills; it cannot replay a POST or regress local state.
4. Offer current withdrawal networks for that output asset and owned accounts
   in the same account group. The recipient is filled automatically and cannot
   be replaced by a caller-supplied address. Fee, minimum and precision are shown.
   The default debit is rounded down only at network precision; any remainder
   stays explicitly displayed on Bybit. Every debit is bounded by actual net
   receipts. A required Unified → Funding move has a separate approval before
   withdrawal. Completion means Bybit's report, with destination-chain credit
   still checked separately in the wallet.

Each economic action has its own approval/password and durable original ID.
Navigation metadata grants no signing or API capability. Wizard IDs never enter
remote payment/order payloads. No automatic next economic action, resubmission
or replacement follows a refresh, restart, timeout or unknown response.

Successful connection/disconnection clears cached deposit destinations before the next preview. A hidden original source remains the locked selected network for its active workflow.

The encrypted optional wizard extension preserves old vaults and immutable
operation digests. State is bound to the selected Bybit credentials; lock clears
the UI draft. Loading the workflow is local and does not require a remote API
call. A failed prepare reply can still recover its saved original ID. Clearing
an unsigned draft or definitively failed/cancelled exchange operation requires
an explicit dialog and retains its original record. Submitted/UNKNOWN operations
cannot be cleared or replaced. The phrase restores wallet keys, not this local
workflow, Bybit credentials or custody access.

The existing regional questionnaire, whitelist, supported route, custody and
fee restrictions remain. The wizard checks Unified/master status and required
Spot Trade, Account Transfer and Withdraw permissions before depositing into a
flow the key cannot finish. A symbol whitelist can still reject a later trading
request. Deposit and wizard withdrawal do not require unrelated spot-market or
trading-fee reads. Bybit's read APIs have historical retention/window limits;
an unavailable original record remains unresolved and never enables a retry.

Official contracts checked on 2026-11-02:
[master deposit address](https://bybit-exchange.github.io/docs/v5/asset/deposit/master-deposit-addr),
[deposit records and restriction states](https://bybit-exchange.github.io/docs/v5/asset/deposit/deposit-record),
[deposit account preference](https://bybit-exchange.github.io/docs/v5/asset/deposit/set-deposit-acct),
[internal transfer](https://bybit-exchange.github.io/docs/v5/asset/transfer/create-inter-transfer),
[safe account balances](https://bybit-exchange.github.io/docs/v5/asset/balance/account-coin-balance).

The subsequent submitted-deposit recovery correction and public TON trace are
documented in [automatic confirmation](EXCHANGE_DEPOSIT_CONFIRMATION.md).

## Hosted graphical-driver correction

The first hosted wizard run (`7c40510`, run 41496791295) failed at public address
selection: PSM11 produced a complete address box plus overlapping internal word
boxes. Seven earlier steps passed, but that job is FAIL, not acceptance.
The same issue was retained in the first local full attempt.

The driver now retries that specific overlap using PSM7 on the already captured,
guard-checked public address row. It requires one complete token in valid bounds
and reruns the original strict locator. It cannot recapture a private window,
change a glyph to the expected value, use an expected SDK address to locate text,
or bypass any clipboard assertion. Every shortcut must still replace a fresh
sentinel in an independent OS client with the independently derived full address.
The original overlap, clipping, wrong-row and private-window guards remain. A separately labelled local OCR-fault injection also exercises the real captured-row decoder and all nine exact keyboard-copy assertions; no application response or clipboard value is mocked.
Three added decoder/capture tests bring the current helper suite to 217 passing
checks; the actual seven-check clipboard campaign also passes source-stable.
`reports/exchange-wizard-ocr-helper-tests.log`,
`reports/exchange-wizard-ocr-clipboard.json`, `reports/exchange-wizard-ocr-test-ci.log`.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | Local focused coordinator fixtures: all four separately submitted steps, original deposit binding/credit, original terminal-order reread, changed-address/memo refusal, restricted/foreign/duplicate credits and cursor loops. `reports/exchange-wizard-coordinator-tests-v4.log`, `reports/exchange-wizard-protocol-current.log`. These are protocol fixtures, not live chain/Bybit execution. |
| PASS | Real encrypted signer/vault phase persistence and restart, exact per-operation password/HMAC authority, changed/skipped/foreign state refusal, safe unsigned cancellation and UNKNOWN retention; optional vault extension and unchanged old digests. `reports/exchange-wizard-policy-tests-v2.log`. |
| PASS | Local native wizard gating, current-source preview/address copy, exact net receipts, proceeds budgets and visible precision remainder. `reports/exchange-wizard-controller-final.log`. |
| PASS | 158 React checks including ordered non-actionable progress, safe public deposit address/memo and direct copy; TypeScript/production bundle. `reports/exchange-wizard-frontend.log`, `reports/exchange-wizard-typecheck-build.log`. |
| PASS | Isolated network metadata, deposit-only versus strict withdrawal metadata, fixed field error/canary tests and exact HyperEVM-versus-HyperCore route guards. `reports/exchange-wizard-route-metadata-final-v3.log`, `reports/exchange-wizard-final-focused.log`. |
| PASS | Final `make test-ci`, 158 React checks, portable build, final native/Wails races and all current security controls including 81 semantic mutants. `reports/exchange-wizard-ci-complete.log`, `reports/exchange-wizard-final-build.log`, `reports/exchange-wizard-final-ui-race.log`, `reports/exchange-wizard-security-complete.log`. |
| PASS | Full affected backend/client/vault/IPC race suite and vet. The last follow-up changes only UI preview invalidation/hidden-source selection and the graphical driver; its final native/Wails races pass separately above. `reports/exchange-wizard-race.log`, `reports/exchange-wizard-vet.log`. |
| PASS | Six actual X11 Appearance, masked Bybit cancellation, Exchange navigation and wizard draft checks on final binary `19cd18aa819256a9f38938c2ea44e52213f7e5d1d3bb1d1c912c5510fb5faed9`, stable sources. The wizard check is now included in the default hosted graphical path. `reports/exchange-wizard-final-appearance.json`. |
| PASS, final local graphical | All 38 generated-root/fresh-copy graphical workflows pass with the corrected driver and stable sources. Six actual checks on the authenticated downloaded 0.1.44 executable also pass. `reports/exchange-wizard-ocr-full.json`, `reports/exchange-wizard-published-v2.json`. Earlier 37-step and 26-target fuzz checkpoints remain in their original reports; final hosted fuzz acceptance is recorded below. |
| PASS | Final binary independently restores all 24 wallet descriptors and passes nine actual private-geth checks, including unchanged-nonce/zero-approval refusal of an unrecorded quote. `reports/exchange-wizard-recovery-final.log`, `reports/exchange-wizard-geth-final.log`. |
| PASS, first publication | Normal release 0.1.43 and build 41496791072 match `7c40510`; publisher/source/tag/version/archive inventory and downloaded bytes verify. Six actual checks on that authenticated published executable pass. This does not turn its failed hosted graphical run into acceptance. `reports/exchange-wizard-release/summary.json`, `reports/exchange-wizard-published.json`. The graphical-driver follow-up requires its own hosted result. |
| PASS, exact-source hosted desktop | Both jobs in [tests 41499890553](https://github.com/neodix42/crypto-wallet/actions/runs/41499890553) succeed for `f8945c6a4f06a664abea70b88eda05fa72162420`. Desktop job 125464880611 passes 38 full graphical, five TON, seven independent recovery and nine actual private-geth checks. Artifact 13170271527 digest `646d3bcc9be800f3998dc69b6610c815609581a376847b7c22ea5383dffe13a3`, all nine report/source/binary bindings and the new wizard draft check verify. `reports/exchange-wizard-hosted-gui-v2/summary.json`. |
| PASS, exact-source hosted backend | Job 125464881202 passes units/helpers/frontend, security, all 81 mutants in ten campaigns, full races/stress, independent interoperability, 13 actual local integration gates, 26 measured fuzz targets, dependencies, 24-descriptor recovery and crash/secret canaries. All 30 JSON gates in artifact 13169872925 verify against exact Git blobs: 1,671 source files, zero mismatches; archive SHA256 `c4d42e614a28bc570ad59c0465c6af8e56b57ee28f03511d2f4fd7fefa989335`. `reports/exchange-wizard-hosted-checks-v2/verification.json`. |
| PASS, final publication | [Build/release 41499890530](https://github.com/neodix42/crypto-wallet/actions/runs/41499890530) and normal [release 0.1.44](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.44) match that exact source. Publisher/source/tag/version/inventory and downloaded bytes verify; one portable archive contains only cryptowallet, install-user.sh and the PNG/SVG icons. Archive SHA256 `00d953e02e5eaf0a3b561bd922988131bbd4da24f350f71527eaadaf59e99832`. `reports/exchange-wizard-release-v2/summary.json`. |
| NOT_RUN | Private Bybit account/region/IP/whitelist acceptance and funded deposit/transfer/spot/withdrawal. Physical OS/Wayland matrix and independent review. No real user secrets or automatic mainnet funds are used. |

Development failures remain distinct in their logs: earlier UI assertions
referenced the replaced controls; one synthetic USDT withdrawal exceeded its
fixture's ETH cap; one TON test parsed the pinned raw master as a friendly
address; early reconciliation tests sent extra fields to the strict endpoint.
The first actual account-layout campaign passed its twelve UI steps but rejected source changes while metadata fixes were being made; it is not final stable-source evidence. Those failed attempts are not passing acceptance or reasons to loosen production
validation.
