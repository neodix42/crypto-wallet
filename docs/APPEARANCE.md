# Appearance and unlock behavior

> Owner update (2026-10-29): the shipped Wails UI now uses one trusted main window with reusable in-page dialogs and ordinary Wails bindings. It no longer launches a private UI self-role, isolated prompt page or one-use UI pipe. Normal selection/copy/cut/paste are enabled. The main UI is trusted with entered/displayed secrets and approval intent. This supersedes the separate-renderer and noncopyable-secret requirements below, which describe earlier designs. Go signer/core isolation, cryptography, reauthentication and transaction validation remain. See [the current trust model](TRUSTED_UI.md).


Public address icons copy immediately, and selected addresses support normal
keyboard Copy. Sensitive views retain their separate restrictions. See
[address copying and runtime checks](CLIPBOARD.md).

Open **Settings → Appearance** to choose **Light** or **Dark**, and a global font
size from 14 to 24 px in two-pixel steps. A fresh installation defaults to Light
and 18 px. The selected Settings section has a different background/accent color.
Appearance is available before unlocking and applies to both the portfolio and
temporary private wallet dialogs.

Go persists only the allowlisted theme and integer font size in
`$XDG_CONFIG_HOME/cryptowallet/ui/appearance.json`, normally
`~/.config/cryptowallet/ui/appearance.json`. It is independent of both profile
vaults and contains no password, recovery words, API credentials or wallet data.
Loading/saving rejects symlinks, nonregular/hardlinked or excessively large files,
unknown/duplicate JSON fields and unsafe ownership/permissions. Replacement uses
an owner-checked directory handle, private temporary file, fsync and atomic rename.
The frontend does not store preferences or secrets in browser storage.

The main window now requests 1440 × 1120 logical pixels and fits its size to the
current monitor's logical bounds, reserving a margin for desktop decoration.
It centers after GTK allocates the fitted size. The default 18px
eight-wallet overview uses closer table spacing so it fits the tested
1440 × 1016 viewport without vertical scrolling. Receive addresses retain their
full single-line width; horizontal table scrolling remains available when needed.

Recovery views use wider, word-count/font-aware windows and four word columns
when space permits. At 18px, the backup requests 920 × 830 for 12 words and
920 × 980 for 24 words, including the Copy phrase action and its explanation.
Windows remain bounded by the monitor; small displays and large text retain a
scrollable body with reachable footer actions instead of clipping content.
Window and task-switcher icons are embedded in the executable. Optional desktop
menu/icon registration is described in [the installation instructions](../README.md#optional-application-menu-and-desktop-installation).

Private child processes receive only validated public presentation settings.
The existing one-use prompt carries its theme/font alongside the exact Go-authored
request. Signer IPC, secret reply fields and approval authority remain unchanged.
The two-field Bybit prompt is taller and scales with font size, with a scrollable
body and fixed reachable footer within the available monitor dimensions.

The locked page has no **Retry storage check** link. Press **Enter** to activate
Unlock, then Enter in the masked storage-password field to submit. Repeated or
IME-composition Enter events cannot submit duplicate private replies.

Unlock has two separate costs. Opening its password form starts a fresh private
Go/WebKit process and renders its embedded interface; it does not derive the
password key or query blockchains. The main window immediately reports
**Unlocking wallet…** while the request is pending. The temporary process remains
isolated from the ordinary portfolio and is retired after its one-use reply.

After password entry, the signer authenticates the encrypted vault with its
stored Argon2id parameters and verifies every account descriptor. An unchanged
vault is read without rewriting either encrypted generation. Interrupted payment
states, unsigned exchange cancellations and a retired backup flag still require
an authenticated atomic commit before unlocking succeeds. Those recovery cases
retain the additional encryption/verification work; a failed commit locks the
wallet. The default 256 MiB / three-pass / four-lane KDF, authentication and
per-operation password requirements are unchanged. No derived-key cache is added.

Controlled local measurements distinguish those phases: the original password
form was ready in about 0.85 seconds; an unchanged unlock unnecessarily performed
three KDFs. Removing its no-op write reduced a backend sample from 690 ms to
241 ms and the actual password-to-overview paint sample from 1,206 ms to 692 ms.
The private-window startup remains about 0.85 seconds. These are disposable-profile
samples on this machine, not a hardware-independent latency guarantee. See
[the evidence checklist](STATUS.md) for exact artifacts, scope and remaining gates.

To measure backend unlock locally with the production KDF and all active network
descriptors, use disposable fixtures through this opt-in benchmark:

```sh
./scripts/with-go.sh go test ./internal/signer -run '^$' -bench '^BenchmarkUnlockUnchangedVault$' -benchtime=3x -count=1
```

**Connect Bybit** uses the current unlocked, recovery-confirmed wallet session.
Enter the API key and matching HMAC secret in its separate masked fields, then
confirm the exact service and custody terms. Connecting repeats no app-password
prompt. It changes no funds. Disconnect, payment, spot trade, internal transfer,
withdrawal, phrase backup and token-import authorizations remain intact. Changes
to the wallet generation/session, cancellation or failed persistence deny the
connection and do not expose credentials in public state.

| State | Evidence |
| --- | --- |
| PASS, scoped | Frontend component/protocol tests and strict per-file coverage; finite theme/font validation, public/private presentation, Enter and one-use handling. |
| PASS, scoped | Go preference/path/atomic-persistence checks and controller/private-prompt tests under race detection. |
| PASS, scoped | Connect-only tests cover cancellation, stale generation/session, custody consent, encrypted persistence and unchanged funds-moving reauthentication. Existing exchange and new connect semantic mutants remain required. |
| PASS, actual local GUI | Source-stable targeted Appearance drill and full 20-step Wails generated-root campaign, including Light/18 → Dark/24, selected subtabs, complete Bybit fields/footer, cancellation, restart/Enter unlock and all eight restored wallet addresses. No real credentials or funds. |
| PASS, final local aggregate | All 33 unchanged Go package floors and four required execution gates; both frontend campaigns pass. Controller 97.04%, Wails adapter 95.10%, EVM 96.81%. Instrumented GUI also passes all 20 steps, with counters for every observed private process. |
| NOT_RUN | Full clean Ubuntu X11/Wayland and monitor/scale matrix, and live Bybit authentication/funded acceptance. |

Run `make test` for unit/regression suites, `make coverage` for the full enforced
aggregate, and the following for actual isolated GUI checks on a machine with
the provisioned X11 test tools:

```sh
make build
./scripts/with-go.sh python3 scripts/wails-wallet-test.py --appearance-only \
  --report-name wails-appearance
```

GUI drills use owned profiles/displays and disposable public fixtures. Private
credentials and pixels are not retained in reports.
