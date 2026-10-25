# Wallet layout and verification

This document records the earlier layout candidate. Current Settings uses main-window tabs; current notices use a fixed bottom status bar, full-width addresses and wallet asset buttons. See [WALLET_STATUS.md](WALLET_STATUS.md) for the latest behavior and candidate-specific evidence.

All application dialogs now use Electron by explicit owner instruction. Go generates the recovery root, validates recorded words/restoration and transaction effects, encrypts storage and signs. The generated phrase, passwords and challenge inputs are confined to a separate temporary Electron prompt window and its private Go transport; the portfolio page never receives them. This expands the trusted surface beyond Go. It does not preserve the former claim that secrets never enter Electron. See [FRONTEND_SIGNER_TESTS.md](FRONTEND_SIGNER_TESTS.md) for the precise boundary and memory-clearing limits.

First launch asks for an app password, shows the generated BIP39 phrase for recording and asks for selected words. Successful Go verification enables receiving/sending. Cancellation or wrong words preserve the same encrypted pending root. A fresh installation offers **Restore from recovery phrase** using a specified local password and any original optional BIP39 passphrase. Portable encrypted-backup creation, verification and file restoration remain removed.

Each deterministic account index has a top tab, starting with Main account. Its semantic table contains Network, Assets, Native balance and Receive address. Row click or Enter/Space opens a wallet tab; its copy icon calls signer-validated receive copy without opening the row. Addresses remain single-line with a full tooltip. Balances use exact atomic-string formatting, distinct native assets/ledgers and one background query at a time. Missing, malformed, incomplete or failed data remains unknown/unavailable; provider observations are not consensus proofs.

Wallet tabs initially show balances and network details. Send opens a separate form. Pending setup offers **Finish recovery phrase setup** while preparation remains gated. Completing setup preserves only an unchanged original draft and never prepares/sends automatically. Closing a form/tab clears local input and rejects delayed results without cancelling durable preparations or releasing reservations. Middle-click/Delete closes wallet tabs; account tabs remain.

Receive opens the isolated Electron network/address/QR dialog with values validated and supplied by Go. Pending setup continues to it only after successful confirmation for the unchanged wallet/view/session. Copy uses the separately supplied address, keeps the dialog open and does not run on Done. Public QR files and clipboard actions are explicit. Decred's durably issued descriptor can update its own public row after navigation, but cannot overwrite another wallet or survive lock.

The top-right Settings button contains recovery/password/account actions and the Local payment journal. Journal wallet/asset selectors operate independently of Send and preserve original cancellation/reconciliation identities. The modal traps focus, makes its background inert and dismisses with Escape, Close or backdrop. Credential actions close it before the separate prompt opens. Lock clears drafts, views, Settings, journal and queued background work, while prompt destruction clears its sensitive DOM/session on a best-effort basis.

## Current verification

| Check | Status | Evidence and scope |
| --- | --- | --- |
| Tables, row/copy/keyboard behavior, account/wallet tabs | PASS in component tests | Actual HTML/JavaScript, all supported registry shapes, active/inactive closing and stale reply rejection |
| Exact native balances and bounded scheduling | PASS in component tests | Precision beyond JavaScript Number, provider/descriptor invalidation, superseded refreshes, prompt-action pauses and lock cleanup; no live balance claim |
| Send, Receive continuation and Settings journal | PASS in component tests | Hidden-form guards, unchanged draft binding, cancelled/incomplete/locked/navigated continuation rejection, independent journal and no automatic reservation release |
| Isolated Electron prompts | PASS in component tests | Strict schemas/IDs/origin/frame, one-time delivery, masked input, phrase clearing, title binding and lifecycle cancellation; component doubles do not establish the installed Chromium sandbox |
| Frontend coverage | PASS | 95 tests, zero skips; eight shipped JS files all 100% statements. Main 98.4% branches, renderer 97.98%, remaining six 100%; critical prompt UI floors 95%/90%. `reports/frontend-coverage.json` |
| Fresh-install deterministic restoration | PASS in headless protocol integration | Six checks, three separate application copies/HOME/config/processes, no copied vault/keyring; all 27 descriptors for nine mainnet networks at indices 0/1/127 match with same/different local passwords; `reports/phrase-recovery.json` |
| Current installed Electron workflow | PASS on final hardened artifact | All 20 actual-X11 checks: default 12-word setup/confirmation and fresh-profile phrase restore, exact decoded QR/clipboard, Settings journal, native balance observation, Send/form/tab controls, restart/profile isolation and tested canaries; `reports/packaged-recovery.json`. All five phrase lengths have prompt component coverage. Installed sending, separate hardware and full OS/Wayland/lifecycle acceptance remain NOT_RUN |
| Current Go campaign | PASS within final source-bound scope | 23 measured package floors across 149 stable source/test/module hashes; 22 shipped packages plus retained, unshipped GTK regression code. [COVERAGE.md](COVERAGE.md) lists explicit skips and all uncovered blocks |
| Separate physical machines, full supported OS/Wayland/lifecycle matrix, funded acceptance, 24-hour workload and independent review | NOT_RUN | Same-host component/protocol tests do not establish external release gates |
| Automated mainnet payments | NOT_RUN | Never attempted |

The final headless and installed drills use the same hardened Go binary manifest. [FRONTEND_SIGNER_TESTS.md](FRONTEND_SIGNER_TESTS.md) lists the complete binary/ASAR hashes. Earlier title/OCR driver failures remain separately recorded; final passing runs do not relabel them.

## Historical evidence

The preceding native phrase campaign passed 68 frontend tests and 20 packaged checks on its own artifact. Earlier [wallet-actions](evidence/wallet-actions.json) and [wallet-layout](evidence/wallet-layout.json) campaigns passed 65/27 and 53/22 frontend/packaged counts respectively. Native GTK tests and encrypted-file restoration from those campaigns remain historical, not current Electron recovery evidence. Failed OCR, file-selection and account-tab attempts remain separately recorded failures. See [STATUS.md](STATUS.md) for outstanding release gates.
