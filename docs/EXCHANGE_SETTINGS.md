# Settings and Exchange interface — 2026-10-25

Select **Settings**, immediately to the right of **Exchange** in the main tab bar. Settings opens in the main window and contains three tabs:

| Tab | Contents |
| --- | --- |
| Wallet | Password, recovery/setup actions, deterministic accounts and recovery ranges |
| Exchange | Bybit API region, Connect/change key, refresh connection and Disconnect |
| Journal | Local payment journal, wallet/asset selection, reconciliation and older payment/chain-history pages |

Deposit, Swap and Withdraw remain in the main **Exchange** page. Its **Exchange settings** shortcut selects **Settings → Exchange**. Select an account, wallet or main Exchange tab to leave Settings. Information and error notices float over the page, can be dismissed and do not push the content down. Settings itself has no modal overlay or focus trap.

Credential, password and recovery inputs continue to use the existing separate Electron dialogs. After Go password verification, connection setup requests the **Bybit API key** and its matching **API secret**, both masked. “System-generated” refers to the key type created in Bybit: its secret signs authenticated requests using HMAC. The API key alone identifies the connection and cannot authorize trading or withdrawals. The app does not create a new Bybit key and does not support self-generated RSA keys. Settings contains no credential inputs or credential response. No connection action transfers funds.

The Exchange asset selector names the asset. **Token contract** labels an EVM token's issuing contract separately from the owned wallet address. HyperCore spot assets instead display **Token identifier**; a spot token identifier is not an ERC-20 contract. Ethereum USDT's `0xdAC17F958D2ee523a2206206994597C13D831ec7` is the token contract, not a receiving account. ETH and supported USDT in the same Ethereum account use that account's owned receive address. See [the Exchange guide](EXCHANGE.md) for the network, gas and custody requirements.

## Current-change verification

The entries below concern the main-window Settings layout, floating notices, clearer asset identity and credential labels. A previous candidate's passing tests do not establish these results.

| Check | Status and scope |
| --- | --- |
| Settings immediately follows Exchange and renders in the main window | PASS — component assertions and actual packaged Settings navigation |
| Wallet/Exchange/Journal navigation and journal isolation | PASS — component keyboard/context checks and actual packaged tab persistence and Journal navigation |
| Floating dismissible notices without layout movement | PASS — fixed-position structure, dismissal, inert message text and sanitization component checks; three actual packaged layout observations during onboarding and credential cancellation |
| Separate owned wallet address and token metadata | PASS — component assertions including HyperCore/EVM isolation; actual packaged public pixels independently confirm the full owned Ethereum address and USDT contract |
| Masked credentials, cancellation and focus handoff | PASS — existing prompt component/security and Go credential-validation tests; actual packaged app-password cancellation and empty API-input cancellation leave the vault unchanged. The packaged drill checks dialog title prefixes, not API-secret field-label OCR. Clearer API key/secret wording is verified by source review |
| Full frontend suite and enforced coverage | PASS — **132 tests**, zero skipped; all eight shipped JS files have **100% statements**; renderer branches **98.38%**, main **98.41%**, other six files **100%** |
| Go package tests, package floors, retained native input, actual TON emulator and isolated USDT execution | PASS — all **25** measured package floors; native-input checks cover unshipped legacy GTK code |
| Complete Go coverage wrapper | FAIL — required Decred simnet gate BLOCKED because pinned **dcrd v1.10.9** is unavailable; eight explicit optional skips remain NOT_RUN. Decred provisioning is deferred; requirements/floors were preserved |
| Bounded fuzz and Exchange signer mutation controls | PASS — **16** local targets at **three seconds each**, source stable; **five** signer policy mutants killed |
| Rebuilt portable artifact and actual packaged navigation | PASS — `make install`, all **12** artifact payload comparisons, production fuses/ASAR inventory and consistent **0.1.0** versions; **26** source-stable packaged X11 checks on the final candidate, including receive QR/copy, Send-form navigation, restart, profile isolation, all nine network wallets and synthetic secret canaries |
| Final-candidate payment regression | PASS — **seven** actual packaged Electron/geth checks: independently queried successful receipt, exact recipient credit, source fee debit and nonce; loopback-only public genesis fixtures |
| Fresh-profile phrase recovery and owned-process crash checks | PASS — **six** headless fresh-install recovery checks and **seven** Go-process crash/canary checks bound to the final binaries; separate physical-machine and full Electron/OS crash acceptance remain NOT_RUN |
| Actual private Bybit authentication or funded exchange acceptance | NOT_RUN — no real API key or mainnet funds used |

## Previous layout evidence

The previous 2026-10-25 implementation moved region/connect/disconnect into a popup Settings window with Wallet/Exchange tabs. Its evidence is retained in [exchange-settings.json](evidence/exchange-settings.json) and referenced reports. That historical candidate passed 129 frontend tests, its enforced coverage floors, the complete portable build/install, 23 installed X11 checks, seven isolated actual-geth payment checks, fresh-profile phrase recovery and owned-process crash checks. The earlier popup focus issue and failed attempt are retained in `reports/before-settings-focus-fix-packaged-recovery.json`. Closing the popup with Escape and trapping focus described that earlier implementation; they are not requirements for the new main-window Settings page. Current-layout failed OCR/partial attempts remain in `reports/settings-main-packaged-attempt1*`, `reports/settings-main-packaged-attempt2-partial*` and `reports/settings-main-packaged-attempt3*`; they are separate from the final passing candidate report.

The current scoped report is [settings-main.json](evidence/settings-main.json), with final artifact/source bindings and referenced measurements. Go production source changed only for clearer credential prompt strings in this interface task; no Go test files changed. Existing backend evidence retains its own source bindings rather than being silently reused for the new source. The broader [Exchange aggregate](evidence/exchange.json) remains **FAIL** because its current Go coverage wrapper has the blocked Decred prerequisite; the scoped interface PASS does not override it. Earlier packaged reports and ASAR hashes describe their original artifacts. The incomplete earlier soak has no surviving recorded process or terminal completion result and remains NOT_RUN/partial, rather than an active or passing campaign.
