# Wallet amounts, addresses and status feedback

The main window reserves an **88-pixel status bar** across its bottom edge. Ordinary information, errors and pending Receive setup guidance stay inside that bar. Long messages scroll within it, and dismissing them does not move the page. Informational Go notices no longer open an additional window. The isolated password, phrase, transaction approval and verified Receive QR dialogs retain their separate origin/session and Go authorization checks; no secret is moved into the portfolio or status bar.

Switching main tabs focuses the selected navigation control. It does not focus or draw a green rectangle around the page heading. Keyboard focus indicators remain on interactive controls.

The account table displays each wallet's native balance as an exact decimal value. State, block/hash and provider trust details appear in its mouse-over tooltip. Loading, unavailable and unknown data remain visibly distinct from zero; missing Decred configuration displays **Connection required**, with setup guidance on hover. The Receive address column uses its full address width and copy control. Narrow windows scroll the table horizontally instead of truncating addresses.

Each open wallet shows its complete public receive address and a copy icon. Copy requests only the account ID; Go supplies the verified address, so changing portfolio text cannot change the clipboard destination. Decred's committed receive descriptor updates the matching wallet and row, including after navigation; lock clears displayed addresses and asset selections.

Wallet pages show the selected balance without provider-observation text. Detailed observation information remains on hover, and RPC details remain under collapsed **Network details & public diagnostics**. Multi-asset wallets use asset buttons instead of a dropdown: Ethereum ETH/USDT and Base/Horizen ETH/ZEN remain independently selectable. The active asset appears in the wallet title and top tab. Each wallet remembers its asset while open; a new wallet view starts with its native asset.

**Send** opens the form and focuses the recipient. It no longer offers **Scan local QR image**. The amount label is **Amount**, with a selected-unit hint such as **1 = 1 ETH**. Values remain exact decimal asset amounts, not atomic-unit inputs: `1` ETH is `1000000000000000000` wei and `1` TON is `1000000000` nanotons. The existing Go conversion and all precision, fee, reservation and approval rules are unchanged. Changing an asset invalidates a prepared approval and updates the unit hint.

## What Bybit confirmation means

The Exchange disclosure now explicitly says to review and approve each deposit, trade or withdrawal in this wallet using the app password. The wallet then performs the approved on-chain payment or authenticated API request. A required Funding/Unified transfer has a separate approval. Connecting or refreshing never spends funds.

Bybit provides authenticated [spot-order](https://bybit-exchange.github.io/docs/v5/order/create-order) and [withdrawal](https://bybit-exchange.github.io/docs/v5/asset/withdraw) API calls. There is no additional manual confirmation API in this wallet's flow. Blockchain deposit confirmations accumulate with blocks; API calls cannot force them. Bybit's account, whitelist and compliance restrictions still apply. See [the API contract](BYBIT_API.md) and [Exchange guide](EXCHANGE.md).

## Evidence checklist

The source/candidate-bound report is [wallet-status.json](evidence/wallet-status.json). A passing scope does not establish the separate production acceptance gates.

The first actual packaged attempt exposed a real missing case: Go can successfully persist a pending wallet after wrong recovery words. Initial notice-routing tests treated that successful response as normal completion, so the footer incorrectly said **Request completed**. The fix returns a fixed, reviewed mismatch code even for that pending success response; the renderer refreshes the existing encrypted wallet before showing the error. New tests require the pending setup controls, useful guidance and no replacement wallet or automatic payment. The failed packaged log/report remain separate from the final result; statement coverage alone did not establish this integration behavior.

Later failed desktop attempts are retained individually. They exposed OCR grouping, viewport navigation and disappearing-window queries in the public-pixel test driver. Address checks still require exact equality with independently derived complete addresses. The local X11 guard tolerates only a vanished owned window during its exact attributes query; its separate regression requires unexpected X11 errors to remain fatal and both the original getter and handler to be restored. Driver repairs did not change the installed application or its acceptance assertions.

Short late-control probes have separate reports and explicit unrun scopes; they do not replace the complete campaign. Exact scratch source was overwritten before archival for the first two failed probes and is marked unavailable. Subsequent passing probe source is retained with its hash.

| Check | Status and scope |
| --- | --- |
| Whole-asset amounts and separate economic approvals | **PASS** — five targeted existing Go tests across the amount, coordinator and signer packages; no backend behavior change. |
| Main-window component suite and enforced frontend coverage | **PASS** — 148 tests, zero skipped; all eight shipped JS files at 100% statements. Main branches 98.47%, renderer 98.26%, remaining six 100%; source and runtime hashes bound to the measurements. |
| Final portable build, fuses, payload comparisons and installation | **PASS** — `make install`, consistent 0.1.0 versions, production fuses and all 12 payload comparisons. |
| Actual packaged X11 status, address, asset, Send and recovery checks | **PASS** — 34 source-stable checks on the final candidate: three fixed 88-pixel footer/layout observations, four heading-outline observations, full independently verified addresses, asset buttons and units, Receive QR/copy, recovery, restart, profile isolation and secret-canary/log scans. Earlier failures remain separate. This private X11 drill does not establish the full installed OS matrix. |
| Actual isolated-geth packaged Send | **PASS** — seven checks on the final ASAR, including independently queried successful receipt, exact recipient credit, source debit including fees and delayed nonce. Loopback-only public genesis fixtures; no public-chain payment. |
| Headless recovery and owned-process crash checks | **PASS** — six fresh-install protocol recovery and seven Go-process crash/canary checks; final installed binary hashes match those tested. Separate physical machines and complete Electron/OS crash acceptance remain unrun. |
| Current Go aggregate | **FAIL / BLOCKED** — all 192 Go source/test/module hashes exactly match the retained passing package tests and 25 floors; these unchanged scopes were not rerun wholesale. The required pinned Decred simnet node remains unavailable. No prerequisite or floor was waived. |
| Live Bybit account or funded external transfers | **NOT_RUN** — no real credential or mainnet payment used. |
| Full clean Ubuntu X11/Wayland/lifecycle matrix, complete soak and independent review | **NOT_RUN** — unchanged production release gates. |
| Production release verifier | **FAIL** — required acceptance requirements remain incomplete, including the blocked Go aggregate and unrun external gates. Frontend, artifact, packaged recovery and isolated Send bindings introduce no verifier failures. See [the retained verifier log](evidence/wallet-status-production-gate.log). |
