# CryptoWallet's pinned Wails source

This is Wails `github.com/wailsapp/wails/v2` v2.17.0 with explicit Linux wallet changes. The complete 963-file upstream tree and licenses remain. `WALLET-UPSTREAM.json` records its hashes; `WALLET-PATCHES.json` records reviewed final changed/added files. `python3 scripts/verify-wails-fork.py` rejects unlisted edits, additions, removals and links. These files are development source, not release runtime helpers.

## Single trusted application UI

The owner's 2026-10-29 request removes separate private prompt processes, pages and one-use UI pipe channels. The main Wails application binds both page/controller and reusable dialog APIs. Normal Wails binding/runtime dispatch is available; the former wallet-specific method allowlist is removed. Runtime diagnostics remain silent, binding errors are redacted and arguments/results are not logged. The UI is trusted with prompted passwords, displayed recovery words and approval intent. Compromise of the UI is no longer isolated from another prompt renderer. Signer/core protocol and cryptographic checks are separate.

## Normal clipboard interaction

Every view supports ordinary selection, Copy/Cut/Paste and context menus. Native Ctrl+C, Ctrl+Shift+C and Ctrl+Insert execute WebKit Copy. Cut and Paste use normal WebKit editing behavior. JavaScript clipboard access is enabled, and clipboard permission requests are allowed for the trusted embedded UI. There is no per-view public-copy option or default-deny secret-view keyboard handler. Native Go clipboard setters acknowledge the actual GTK write before reporting success. The helper writes CLIPBOARD and preserves PRIMARY; ordinary text selection can itself own PRIMARY as expected on Linux. Closing an in-page dialog does not destroy the app's clipboard owner. No clipboard-history erasure guarantee is made.

## Local asset and engine policy

The application retains an ephemeral WebKit context, disabled browser storage/page cache/developer extras and local embedded assets. Its custom `wails://wails/` origin rejects malformed paths, foreign navigation and popups. No remote UI code, frames or form posts are loaded. A local-only CSP denies network/eval/worker/frame capabilities; Go services perform blockchain/exchange networking. Media, WebRTC, downloads and unrelated browser permissions remain disabled. These protections do not restrict normal clipboard behavior or create another UI process.

The owner-requested renderer sandbox remains explicitly disabled before WebView creation. There is no Electron/Chromium, SUID helper, root setup, Bubblewrap setup, environment bypass or system-policy modification. System GTK3 and WebKitGTK 4.1 remain runtime dependencies. WebKit still uses its own engine subprocesses; those are not separate CryptoWallet prompt applications.

## Verification

`make test-wails-security` verifies the pinned fork, exercises Go dispatch/logging/clipboard tests, compiles real C callbacks and runs actual system WebKit on an owned X11 display with disposable HOME. Public fixtures verify all three copy aliases, native clipboard acknowledgment/PRIMARY preservation, ephemeral/no-sandbox settings, CSP eval/storage/network denial and blocked navigation/popups. The HTTP witness must observe zero requests. The actual wallet GUI campaign separately verifies same-window input/recovery/Receive/approval/clipboard behavior. Historical reports requiring isolated prompt processes or default-deny private copying certify only those historical artifacts. Physical desktop/Wayland/OS matrices, funded acceptance and independent review remain separately scoped.
