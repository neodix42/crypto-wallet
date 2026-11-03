# Stablecoin balances and independent Exchange assets

The 2026-11-04 owner update requires USDT and USDC safe available balances
from both Bybit Funding and Unified accounts on every wizard page. These are
custody balances, separate from the selected wallet's on-chain holdings.
Unobserved or failed reads remain unavailable; they are never displayed as zero.
Opening or refreshing a page cannot move funds.
Connected pages refresh the summary read-only in the background with a bounded
interval and refresh after an explicit action. Navigation, connection and
generation changes discard stale replies. Failed reads invalidate spendability;
typed amounts and the original market-quote timestamp are preserved.

Deposit keeps its selected wallet/network and asset. Funding → Unified and
Unified → Funding select their own asset, including USDT and USDC. A previous
GRAM deposit or trade does not restrict those selections. Amount precision,
current safe source availability and the exact separately approved transfer
remain required. Choosing another asset does not change the deposit address,
rewrite an older operation or authorize a conversion.

Withdraw selects its own asset and a verified network/owned-wallet route in the
same account group. USDT and USDC use their existing reviewed wallet identities;
only routes actually validated against Bybit's current metadata are offered.
The entered debit and fee must fit Funding availability. Unified funds must be
moved explicitly in step 4 first. No guessed contract, implicit bridge or
automatic transfer is added.

The [current-funds policy](BYBIT_CURRENT_FUNDS.md) still applies: older UNKNOWN
operations do not block a new current-funds action, every economic action has
its own ID and exact/password approval, and old requests are never replayed or
replaced automatically.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | Complete native controller suite and focused regressions: all five summaries; independent USDT/USDC transfers/withdrawal selectors; stale/failed reads; precision, owned descriptors, preserved drafts and old UNKNOWN independence. `reports/exchange-stable-ui-all.log`, `reports/exchange-stablecoin-ci-final.log`. |
| PASS | Full coordinator/signer-dispatch suites and six new coordinator regressions: selected asset plus both stablecoins, GRAM-source stablecoin transfers, own-group routes and hostile coin/contract/suspension responses. `reports/exchange-stablecoin-pools-core.log`. |
| PASS | 163 frontend tests and strict TypeScript/build, including semantic table headers, unknown versus zero and safe generic fallback. Nine focused viewport/navigation helper tests pass within the full developer-tool suite. `reports/exchange-stablecoin-frontend.log`, `reports/exchange-stablecoin-ci-final.log`. |
| PASS | Actual uninstrumented offline GUI: all five stablecoin tables, both coin selections in steps 2/4/5 and invalid draft/slippage checks; source stable, no invented credentials, balances, route or payment. `reports/exchange-stablecoin-gui.json`. |
| PASS | Final complete local `make test-ci`, `make test-security-ci`, affected full races, vet and portable build. All 81 compiling mutants in ten stable-source campaigns are killed. `reports/exchange-stablecoin-ci-final.log`, `reports/exchange-stablecoin-security-final.log`, `reports/exchange-stablecoin-race-final.log`, `reports/exchange-stablecoin-vet-final.log`, `reports/exchange-stablecoin-build-final.log`. |
| PASS | Six actual stable-source appearance/restart/Bybit-dialog checks, including all five summary pages and both stablecoin choices in steps 2/4/5. `reports/exchange-stablecoin-appearance-final.json`. |
| PASS | Wails job 126184658919 in [tests 41712718680](https://github.com/neodix42/crypto-wallet/actions/runs/41712718680) for `616e87183d19eb5ae932f4884b5c7d9157c28a42`: all nine source/binary-bound reports pass, including 38 full GUI, five TON, seven recovery and nine actual private-geth checks. Full GUI records all five stablecoin tables and the exact six USDT/USDC selections in steps 2/4/5. Artifact 13271180032 SHA256 `ac27c359f85dcee4054e2f706c37031180c0d56209730faff6455085852e1892` and independently rebuilt exact-Git source fingerprint verified. `reports/exchange-stablecoin-hosted-gui/summary.json`. |
| PASS | Both jobs and every step in exact-source [tests 41712718680](https://github.com/neodix42/crypto-wallet/actions/runs/41712718680) succeeded. Backend artifact 13270987195 SHA256 `9a268bc64298f50c5702e352fe477b1f9784d9d84ab70625578443a980ede532` independently verifies all 30 JSON gates, 81 mutants/ten campaigns, 26 measured fuzz targets, 13 local integrations and 24 recovered descriptors/eight networks. All 8,952 reported source-hash comparisons match 1,704 unique files from the exact Git commit, with zero mismatches. `reports/exchange-stablecoin-hosted-checks/verification.json`, `reports/exchange-stablecoin-hosted-checks/summary.json`. |
| PASS | [Build/release 41712718781](https://github.com/neodix42/crypto-wallet/actions/runs/41712718781) and normal [release 0.1.53](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.53) for `616e87183d19eb5ae932f4884b5c7d9157c28a42`: exact publisher/source/tag/app version and sole portable archive verified. Archive SHA256 `c1ce5549d28c8e8395c0697941f83f15d1cdc5e2ccc1d36f044a1eee28d5261b`. `reports/exchange-stablecoin-release/summary.json`. |
| PASS | Three actual offline X11 checks on that authenticated downloaded binary, with stable source: all five balance tables and all six USDT/USDC selections in steps 2/4/5; invalid draft/slippage refusal. `reports/exchange-stablecoin-published.json`. Authenticated exchange balances and funded state transitions are outside this graphical scope. |
| NOT_RUN | Live authenticated funded Bybit actions, physical installed OS matrix and independent external review. No real credentials or automatic mainnet transfers are used. |

An earlier uncommitted graphical attempt timed out while assuming the taller
Exchange form fit entirely in the viewport. It remains FAIL in
`reports/exchange-stablecoin-appearance.json`. The final driver explicitly
scrolls to keep a public label, input and validation area inside the owned
viewport, and anchors navigation to the common balance headers. Private-pixel
guards, exact step directions, amount assertions and all prior GUI checks
remain required; the dedicated three-check and complete six-check reruns pass.
