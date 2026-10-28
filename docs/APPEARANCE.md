# Appearance and unlock behavior

Open **Settings → Appearance** to choose **Light** or **Dark**, and a global font
size from 14 to 24 px in two-pixel steps. A fresh installation defaults to Light
and 18 px. The selected Settings section has a different background/accent color.
Appearance is available before unlocking and applies to both the portfolio and
temporary private wallet dialogs.

Go persists only the allowlisted theme and integer font size in
`$XDG_CONFIG_HOME/localwallet/ui/appearance.json`, normally
`~/.config/localwallet/ui/appearance.json`. It is independent of both profile
vaults and contains no password, recovery words, API credentials or wallet data.
Loading/saving rejects symlinks, nonregular/hardlinked or excessively large files,
unknown/duplicate JSON fields and unsafe ownership/permissions. Replacement uses
an owner-checked directory handle, private temporary file, fsync and atomic rename.
The frontend does not store preferences or secrets in browser storage.

Private child processes receive only validated public presentation settings.
The existing one-use prompt carries its theme/font alongside the exact Go-authored
request. Signer IPC, secret reply fields and approval authority remain unchanged.
The two-field Bybit prompt is taller and scales with font size, with a scrollable
body and fixed reachable footer within the available monitor dimensions.

The locked page has no **Retry storage check** link. Press **Enter** to activate
Unlock, then Enter in the masked storage-password field to submit. Repeated or
IME-composition Enter events cannot submit duplicate private replies.

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
