# CryptoWallet's pinned Wails source

This directory is Wails `github.com/wailsapp/wails/v2` **v2.17.0** with a small,
explicit wallet policy. Upstream's complete 963-file source tree and original
licenses are retained. This is development source, never an extra release asset
or installed runtime directory. Go's root `replace` selects this local module.

`WALLET-UPSTREAM.json` records every original upstream file hash.
`WALLET-PATCHES.json` enumerates every changed or added wallet file with its final
hash. `python3 scripts/verify-wails-fork.py` checks the complete tree, refuses
unlisted edits/additions/removals/symlinks, and never regenerates these manifests.
A deliberate patch update must update the reviewed manifest in source control.

## Transport

The Linux browser-to-Go channel accepts only exact `wails://wails/` or
`wails://wails/index.html` documents, the fixed `runtime:ready` initializer, and
strictly validated, bounded Wails `C` call envelopes. Methods are:

- `PublicBridge.Snapshot()` and `PublicBridge.Event(one payload)`;
- `PrivateBridge.Prompt()`, `PrivateBridge.Reply(one payload)`,
  `PrivateBridge.Copy()` and value-free `PrivateBridge.Activity()`.

The public and private facades are bound in **different Go application
processes**, using private inherited pipes for the latter. The policy does not
make a private method available to the public process: only the facade bound by
the owning Go process can resolve. There is no generic runtime IPC, obfuscated
method call, JavaScript log, clipboard API, browser opener, arbitrary event,
notification, dialog, file, drag, inspector, resize, show/hide or quit authority.
Native close-button messages arrive through the separate native channel.
Go-to-JavaScript events and trusted Go runtime operations remain available.
Bridge return values and argument errors are never written to Wails runtime logs.
Wails' injected JavaScript event listeners are local registrations; they require
no browser-to-Go event permission.

Upstream's script message callback reports the current top-level document URI,
rather than the sending JavaScript frame's URI. The wallet therefore denies
frames in its response CSP, rejects popups and external navigation before load,
and never loads imported/remote HTML. This top-level check must not be reused
as authorization for applications that permit foreign frames.

## Clipboard commit capability

`runtime.ClipboardSetTextAuthorized(ctx, text, authorize)` is a trusted **Go-only**
capability. Linux schedules the operation on GTK, then calls the synchronous
`authorize(commit)` callback. The owner must validate its current prompt/session
and invoke `commit` under the same mutex used to revoke that state. Success is
reported only after the actual native clipboard write. Context cancellation
revokes queued work, waits out an already-running commit, and prevents a retained
callback from writing after the operation returns. Callback failures expose only
a fixed error. The browser facade allowlist does not expose this API.

GTK writes **CLIPBOARD only**. PRIMARY selection ownership and its existing text
are preserved; copying a recovery phrase does not silently export it into a
second selection. Temporary C text allocations are zeroed before free as a best
effort; immutable Go strings and the explicitly requested clipboard ownership
have separate lifetimes. No automatic clipboard-history retention or erasure
claim is made. The generic trusted-Go setter also waits for the native write,
while wallet lifecycle-sensitive copies use the authorized capability.

Focused race tests exercise queued acknowledgment, owner-lock serialization,
stale/canceled callbacks, cancellation during a running commit, failed writes,
retained callbacks and fixed panic handling. The independent native probe places
public markers in CLIPBOARD and PRIMARY on its owned display, performs the exact
C clipboard helper, then verifies only CLIPBOARD changed.

## WebKitGTK policy

Every WebView uses its own **ephemeral WebKitWebContext**. HTML local storage,
HTML5 databases, page caching, developer extras, browser clipboard access,
WebGL/audio/media/WebRTC, JavaScript-created windows, modal dialogs, file URL
access and disabled web security are turned off. Permission requests, file
choosers, authentication, downloads, printing, fullscreen and notifications are
denied. Browser copy/cut shortcuts are intercepted; wallet copy buttons invoke
explicit Go policy. Paste remains available.

Only the `wails://wails` embedded custom scheme is registered. Its URI handler
rejects userinfo, ports, queries, fragments, escapes, traversal and non-ASCII
paths. Documents are limited to `/` and `/index.html`; static resources must use
this same scheme and host. External HTTP/file/data document navigation and new
windows are refused.

Every asset response supplies `Cache-Control: no-store`, `Referrer-Policy:
no-referrer`, `X-Content-Type-Options: nosniff`, a denied browser permissions
policy, and this CSP:

```
default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'none'; object-src 'none'; frame-src 'none'; worker-src 'none'; base-uri 'none'; form-action 'none'; media-src 'none'; manifest-src 'none'
```

The interface has no remote JavaScript, inline JavaScript, eval, browser network
API, worker, frame or form-post capability. Inline **styles** support React
layout. Blockchain and exchange networking stays in the existing Go services.
These are application-level browser restrictions, not a claim of kernel network
isolation in the event of a native WebKit vulnerability.

The owner explicitly requested **no operating-system sandbox**. The new context
calls `webkit_web_context_set_sandbox_enabled(context, FALSE)` before creating a
WebView or subprocess. No `chrome-sandbox`, SUID/root setup, Bubblewrap setup,
system policy change or environment sandbox bypass is used. Memory protection,
no-core-dump handling, process roles and wallet cryptography remain in Go.
GTK3, WebKitGTK 4.1 and their shared-library dependencies are supplied by the OS.
Linux is the only target verified by these additions.

## Evidence

Run the independent Go policy/logging tests and upstream asset tests:

```
go -C third_party/wails test -race -tags webkit2_41 ./internal/frontend/walletpolicy ./internal/frontend/walletclipboard ./internal/logger ./pkg/runtime ./pkg/assetserver ./internal/frontend/dispatcher
```

`python3 scripts/test-wails-security.py` builds the real C security callbacks,
opens actual system WebKitGTK on an owned Xvfb display with a disposable HOME,
checks ephemeral/no-sandbox state and settings, and exercises CSP eval/storage
and network denial plus popup/external navigation. An owned loopback HTTP server
must observe **zero** requests. The report binds the relevant source hashes.
The test CSP must exactly match the production response header before it runs.
No wallet process, user secrets, real profile, RPC or funds participate.

These tests do not replace end-to-end wallet recovery/send tests, physical OS
lifecycle tests, or independent review. Browser permission/file/download denial
handlers are compiled and inspected; this probe does not claim to exercise every
OS dialog or browser API path.
