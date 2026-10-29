# First-run import and authenticated phrase backup

> Owner update (2026-10-29): the shipped Wails UI now uses one trusted main window with reusable in-page dialogs and ordinary Wails bindings. It no longer launches a private UI self-role, isolated prompt page or one-use UI pipe. Normal selection/copy/cut/paste are enabled. The main UI is trusted with entered/displayed secrets and approval intent. This supersedes the separate-renderer and noncopyable-secret requirements below, which describe earlier designs. Go signer/core isolation, cryptography, reauthentication and transaction validation remain. See [the current trust model](TRUSTED_UI.md).


A verified empty profile offers **Restore from recovery phrase**. The same main window asks for the phrase, optional original BIP39 passphrase and a chosen app password. Import is available only for an empty profile and never replaces an existing vault.

For a confirmed unlocked wallet, open **Settings → Wallet → Back up recovery phrase**. Confirm the display warning and enter the current app password. Go verifies it before the in-window dialog shows the existing phrase as numbered words. Use **Copy phrase**, or select and copy the words normally. Clipboard/history services and other applications may retain them. No automatic or OS-wide erasure is promised.

Keep an optional BIP39 passphrase separately; the backup dialog does not disclose it. The app password protects this installation and is independently chosen when restoring elsewhere. Phrase display does not replace recorded-word confirmation, change the root/vault generation or alter the payment journal. Wrong password, pending recovery, cancellation, lock, changed session or persistence failure deny the operation. Inputs and completed dialog DOM clear as a best effort; the UI is trusted with displayed/entered secrets. See [TRUSTED_UI.md](TRUSTED_UI.md).

Earlier separate Electron/Wails prompt evidence below is historical and applies only to the artifact it names.

## Previous display-only evidence — 2026-10-27

The checklist below records the earlier candidate before Copy phrase was authorized. Current clipboard-copy evidence is in [STATUS.md](STATUS.md).

| Status | Executed scope and limit |
| --- | --- |
| PASS | Four modified Go packages, fresh uncached runs; focused race checks for backup authentication, protocol and lifecycle boundaries. New backup functions have **100% Go statement coverage**. |
| PASS | **159 frontend tests**, zero skipped; every shipped JS file has **100% statements**. Main branches **98.55%**, portfolio **98.27%**, other six including secret-prompt logic **100%**. Strict method/kind/schema binding, all supported phrase lengths, retained-node clearing, cancellation, late delivery and export restrictions are exercised. |
| PASS | **13 actual installed X11 checks**: first-run import/cancel and independent public address, confirmation/password cancellation, wrong password, all numbered words, fresh repeat authentication, unchanged ciphertext per backup, unchanged public clipboard, lock/restart and optional-passphrase warning. Pixels and phrase OCR stay in RAM. |
| PASS | **39** canonical packaged recovery checks, **eight** Unlock/status checks and **seven** actual Send checks against isolated geth, all bound to the final installed artifact. Restore selectors target actual public button pixels, so repeated explanatory prose cannot be mistaken for the control. |
| PASS | **Six** fresh-copy Go recovery checks reproduce all **27 descriptors** across nine networks at indices 0/1/127 with the same or a different local app password. **Seven** owned-process crash/canary checks pass on the current binaries. Same-host fresh copies do not establish separate physical-machine acceptance. |
| PASS | Four new semantic mutants are killed by their exact security assertions with passing controls: ready/reentry, explicit confirmation, current password and captured generation. The complete default **18-mutant** campaign was **NOT_RUN** for this change. |
| PASS | Three mutation-runner CLI regressions cover twelve rejected inputs, including normalized report-path aliases that could otherwise let a selected run overwrite the aggregate report. |
| PASS | **16** short fuzz campaigns, explicit bounded stress, source/coverage integrity, type/API checks and vet. Stress uses production parsers/signing/storage with a bounded fake ledger; it is not blockchain consensus or packaged desktop stress. |
| PASS / FAIL aggregate | All **25 Go package floors** pass against **193** source/test/module hashes, including the actual TON emulator and isolated USDT execution. The aggregate remains **FAIL** because pinned **dcrd v1.10.9** is unavailable; nine skipped aggregate tests remain separately identified in `reports/go-coverage.json`. |
| NOT_RUN | Physical-machine recovery, full clean supported Ubuntu X11/Wayland/session-lock matrix, funded public-network/Bybit acceptance, complete 24-hour soak and independent review. No real user secrets or mainnet funds were used. |

Run `make test-packaged` for the original packaged regression, the new `scripts/phrase-backup-packaged.py` drill and the existing Unlock layout probe. The scoped source/artifact-bound report is `reports/phrase-backup.json`; individual logs and failed automation attempts remain in the ignored `reports/` directory. Three early focused automation attempts failed on prose/button targeting, an unlock-refresh race and an invalid assumption that Unlock itself never persists. The corrected driver selects actual public button pixels, waits for restored public state and checks a fresh post-Unlock ciphertext baseline. These failures are retained separately from the final pass; the app was not changed to satisfy those mistaken expectations.
