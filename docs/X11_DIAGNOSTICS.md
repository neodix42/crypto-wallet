# Chromium X11 diagnostic handling — 2026-10-25

The wallet launcher now omits three narrowly identified Chromium X11 cache-maintenance messages from terminal output. This is a compatibility workaround for the pinned Electron runtime, with other diagnostics preserved. It does not repair wallet transactions, suppress application errors or change the isolated credential dialogs.

## Cause

Electron **45.0.0** embeds Chromium **156.0.8078.48**, according to [Electron's release record](https://releases.electronjs.org/release/v45.0.0). In that Chromium version, `AtomCache::GetAtom` first asks the X server for an uncached atom. After the server succeeds, the implementation logs an ERROR asking developers to add the atom to its static cache, then retains the result. The three names below are absent from that static list. Their exact cache-maintenance message therefore does not, by itself, mean the wallet action failed. This conclusion follows from the successful-response branch in [the pinned Chromium atom-cache implementation](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.48/ui/gfx/x/atom_cache.cc).

| Atom | Upstream use |
| --- | --- |
| `_NET_WM_STATE_MODAL` | Electron requests this hint for a parented modal window on X11; see [the pinned Electron window implementation](https://raw.githubusercontent.com/electron/electron/v45.0.0/shell/browser/native_window_views.cc). |
| `application/vnd.portal.filetransfer` | Chromium's X11 clipboard path requests this portal file-transfer format. |
| `application/vnd.portal.files` | The same clipboard path requests the alternative portal files format. |

The clipboard behavior is in [the pinned X11 clipboard implementation](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.48/ui/ozone/platform/x11/x11_clipboard_ozone.cc); the two literal MIME names are defined in [its clipboard constants](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.48/ui/base/clipboard/clipboard_constants.h). A format probe does not establish that a file was copied or transferred.

## Exact scope

The launcher filters only a complete, newline-terminated Chromium ERROR line whose source is exactly `ui/gfx/x/atom_cache.cc`, whose prefix has the expected numeric PID/optional TID, timestamp and source-line shape, and whose message is exactly `Add <one of the three names above> to kAtomsToCache`. It accepts the runtime's variable numeric process IDs and source-line number, rather than matching one recorded invocation.

The streaming reader retains at most **4096 bytes** while deciding whether a complete line is eligible for filtering. Oversized lines stream through unchanged, including subsequent chunks until their newline. Unterminated/partial lines, binary data, malformed prefixes, unknown atoms, other source files, FATAL messages and all other output pass through verbatim. Read failures, write failures and short writes remain errors. No logging severity is globally disabled and no Electron logging switch is added.

The implementation is [stderr_filter.go](../cmd/wallet-launcher/stderr_filter.go), with real process setup in [stderr_process.go](../cmd/wallet-launcher/stderr_process.go). There is no additional log file, wallet storage or credential persistence. Tests use public diagnostic strings and synthetic process fixtures.

## Process and security boundary

Electron still replaces the launcher with `syscall.Exec` on the locked creator thread. The existing Go coordinator's parent-death behavior stays intact. A separate invocation of the same Go launcher runs an internal stderr-relay mode. Before that child execs, its descriptor slots **3/4** are replaced with copies of the original stderr; the relay immediately closes those copies. It receives no coordinator IPC pipe, profile argument, wallet command or credential channel. Its environment is restricted to `GOTRACEBACK=none`.

The helper inherits the launcher's IP seccomp restrictions and zero `RLIMIT_CORE`. Linux exec resets `PR_SET_DUMPABLE` protection, so post-exec non-dumpability is not claimed. The helper drains the pipe through EOF after ordinary shutdown or abrupt parent termination, preserving queued genuine diagnostics. It has no parent-death kill signal that would truncate that drain. If Electron exec fails, the launcher restores stderr, closes the pipe and waits for the helper and coordinator rather than leaving bootstrap children running. Private-pipe isolation and lifecycle behavior have separate real-process tests.

## Verification checklist

These results concern the changed launcher. Older [Settings evidence](evidence/settings-main.json) identifies its earlier launcher/artifact; unchanged frontend source coverage can be reused only after checking its source hashes.

| Check | Status and scope |
| --- | --- |
| Exact filtering and preservation | PASS — six top-level filter tests, 45 named subcases and boundary/chunk loops; the filter function has 100% statement coverage |
| Existing launcher exec/private-pipe isolation | PASS — 32 real exec/private-pipe checks |
| New relay lifecycle isolation | PASS — 12 subprocess scenarios, including startup/exec failure, clean shutdown, abrupt parent exit, descriptor replacement and genuine queued diagnostics |
| Complete launcher package | PASS — final full-source measurement **94.23%** statement coverage (**147/156** statements), above the unchanged **90%** launcher floor; the focused package run rounded this to 94.2% |
| Portable build/install, artifact inventory and versions | PASS — `make install`, 12 payload checks, production fuses/ASAR inventory and matching 0.1.0 versions |
| Actual disposable X11 startup | PASS — launch on the isolated display; no user profile or secrets used |
| Actual packaged Electron Send | PASS — seven checks with isolated geth receipt, exact recipient credit, source fee debit and nonce; no public network route |
| Full Go tests and coverage floors | PASS — source-stable package tests and all **25** floors, bound to **190** source/test/module hashes; actual TON emulator and isolated USDT execution gates pass |
| Full Go coverage aggregate | FAIL — required Decred simnet gate BLOCKED because pinned **dcrd v1.10.9** is unavailable; eight optional cases remain explicitly NOT_RUN. Owner-deferred Decred provisioning does not remove that gate |
| Launcher/supervisor race detector | PASS — both complete package race runs; this result does not claim a new whole-repository race campaign |
| Complete fresh-profile packaged X11 drill | PASS — **27/27** current-candidate checks, source stable; six live-log observations contain no known X11 atom message, Mesa diagnostic or IPC stack trace. The drill also exercises the existing Settings/Journal, address/contract, notice, receive/copy, recovery and profile-isolation flows |
| Headless phrase recovery and crash/canary checks | PASS — **six** fresh-install Go protocol recovery checks and **seven** owned Go-process crash/canary checks, rerun against the changed launcher and final binaries; complete Electron/OS crash and separate physical-machine recovery acceptance remain NOT_RUN |
| Full supported X11/Wayland OS matrix, 24-hour soak and independent review | NOT_RUN — the narrow workaround does not close these release gates |
| Private Bybit acceptance or funded mainnet transactions | NOT_RUN — no real API credentials or mainnet funds used |

The current scoped report is [x11-diagnostics.json](evidence/x11-diagnostics.json), with source and artifact identities and referenced measurements. Its interface and isolation PASS results do not override the failing full Go aggregate or establish production readiness. The launcher remains the supported way to start the complete wallet: run `./cryptowallet` after `make install`.
