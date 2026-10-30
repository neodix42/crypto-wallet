# One trusted UI window

The owner requested this architecture on 2026-10-29. CryptoWallet renders its normal pages and reusable dialogs in the same React tree and system WebKitGTK view. Opening Unlock, Receive, phrase backup, import, Bybit credentials or an approval no longer starts another Go application/WebKit context. No `--internal-prompt` role, isolated HTML entry, UI pipe/session nonce or restricted Wails method allowlist remains. Go-to-UI dialog updates and ordinary Wails bindings replace that mechanism.

The complete installed UI is trusted with passwords, imported/displayed recovery words, optional BIP39 passphrases, exchange credentials and approval intent. A compromised UI can capture those inputs or mislead/approve actions. Separate public/private renderer isolation is no longer a security claim. The browser engine still uses its own implementation processes; this change removes the application's additional prompt process, not WebKit's engine architecture.

Go continues to generate/import roots, derive keys, encrypt/decrypt storage and reconstruct exact signing plans. Binary seed/private keys and vault encryption keys stay in the signer. Main UI, coordinator and signer roles remain in the same portable executable; coordinator and signer still run separately over bounded inherited IPC. The KDF, durable journal, nonce/expiry checks, password reauthentication for funds-moving actions and recovery-word checks are unchanged. No wallet HTTP server or arbitrary signing endpoint is added.

Dialogs receive backend-authored contents. The current dialog is synchronized on startup and replacement. Replies complete that dialog; stale replies cannot complete a later one. Cancel, Escape, lock and close discard pending inputs. Completed dialogs unmount and clear their input/phrase/image DOM. Switching applications does not cancel a dialog. These are correctness/lifecycle safeguards, not a separate trust boundary or a guaranteed erasure of JavaScript/Go/browser memory.

## Copy and paste

Ordinary selection, Copy/Cut/Paste and the native context menu are available throughout the trusted UI. Ctrl+C, Ctrl+Shift+C and Ctrl+Insert copy selected text. Standard paste works in password and import fields. Overview and wallet-tab address buttons copy their already displayed public address directly through the native clipboard runtime. They bypass the wallet event queue, coordinator and signer: no wallet confirmation, snapshot refresh or global busy state is needed. Each button carries the same address as its visible row; other wallet work does not disable it. Receive has an additional Copy address button. Clipboard success is reported only after the native write succeeds. Decred Receive advances its address as before, and the main view is refreshed to show/copy the same current address.

The authenticated phrase-backup dialog still provides numbered words and an explicit Copy phrase button. Words can also be selected and copied manually, as requested. Anyone or any clipboard manager with access to copied words may retain them and control the wallet. There is no automatic clipboard reading or claim of OS-wide clipboard erasure. The main app retains clipboard ownership when a dialog closes.

## Remaining application protections

The UI still loads packaged local assets, rejects foreign navigation/popups, stores no secrets in browser storage/cache and loads no remote scripts/fonts. Blockchain and Bybit HTTP networking remains in Go. Framework/renderer logging does not serialize sensitive bindings. Those protections do not prevent normal clipboard interaction or impose a second UI process. The owner-requested lack of a syscall/WebKit renderer sandbox is unchanged; no root/SUID/helper or system-policy changes are needed.

## Verification

Use `make test-ci`, `make test-security-ci`, `make test-wails-security`, `make test-recovery-portable` and `make test-wails-wallet`. These exercise the current single-window product; retained Electron/handwritten GTK tests are historical local tools. Actual GUI checks use disposable profiles and an owned display/network namespace; their test isolation is not an application prompt boundary. See [the evidence checklist](STATUS.md) for passing, failed, blocked and not-run results bound to the current source/artifact. Physical desktop/OS matrices, funded public chains/Bybit and independent review remain separate gates.
