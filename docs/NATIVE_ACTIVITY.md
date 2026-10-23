# Native activity evidence

Recorded 2026-10-23 on Linux amd64, Go 1.27.3. These are implementation and local test results, not an external security audit. All test inputs are public synthetic values.

The signer installs a native GDK event dispatcher that recognizes only key presses and mouse button presses. It inspects the event type alone, passes every event unchanged to GTK, and never reads key text, key codes, pointer coordinates or credential values for activity. Focus, redraw, timer and programmatic entry changes do not produce activity. The dispatcher mechanism is shared by GTK's X11 and Wayland backends; only X11 has been tested here.

`secureui.SetActivity` attaches a value-free callback for one active private request and limits notifications to one per second. The signer sends an `activity` frame with exact body `{}` and the active request's ID and session. It removes the callback before writing the actual response and on deferred cleanup. These notifications stay on the inherited signer/coordinator pipes; no credential input is forwarded to Electron.

`supervisor.Child.Call` consumes these notifications before the final response. It verifies the active request ID, private session, method, empty body and absence of error fields before invoking `Child.OnActivity`. It limits callbacks to one per second and rejects more than 64 frames in a one-second window. Invalid or late frames terminate the child rather than updating activity. Killing a child interrupts a pending native request. The coordinator must install `OnActivity` at initialization to update its existing user-activity timestamp; a notification does not itself unlock a session.

| Check | Evidence/status |
|---|---|
| Actual GTK native input | **PASS**: private Xvfb + XTest, two keys separated by more than one second and one mouse button produce exactly three callbacks; initial focus/redraw/idle interval produces none |
| Programmatic input / background timers | **PASS**: GTK entry assignment and GLib dialog-response timer produce no activity |
| Native rate limit and lifecycle | **PASS**: 10,000 immediate callbacks forward once; a later event forwards; clearing disables callbacks; a new request can report immediately |
| Native secret and public display boundaries | **PASS**: over-limit multibyte input is refused, real public PNG display succeeds, invalid PNG fails closed, long exact-effects confirmation scrolls, file cancellation returns no path, a real file chooser preserves a selected synthetic path, and unavailable display initialization fails closed |
| Private IPC session and payload validation | **PASS**: foreign ID/session, nonempty/null body, error field, wrong version/method, oversized frame, abrupt EOF and 65-event burst are refused; no synthetic canary is disclosed by errors |
| Late event / lock interruption | **PASS**: a previous request's late event is refused by the next call; killing a peer interrupts a pending request |
| Wrong environment recovery | **PASS**: real encrypted testnet backup cannot restore into a mainnet profile, writes no destination vault and leaves the source backup unchanged; matching-profile recovery succeeds; wrong-profile unlock/import are refused |
| Sustained typing for a full five-minute wall-clock interval | **NOT_RUN**: shorter real input intervals establish dispatch, not this duration |
| Wayland native activity, installed clean-desktop lock/suspend matrix | **NOT_RUN** |

Reproduce private input acceptance with `python3 scripts/native-input.py`. It uses an installed Xvfb or the existing test-only `.tools/xvfb/root/usr/bin/Xvfb`, refuses occupied displays and cleans up the process on exit. It records exact test names, outcomes and measured coverage in `reports/native-input.json`; missing prerequisites are reported as **BLOCKED**. The automation source and XTest helper are absent from production builds. The current native-input suite covers 98.1% of `internal/secureui` statements; the private supervisor suite covers 96.2%. Coverage does not establish the unrun desktop and duration matrix.

Run private IPC and recovery regressions with:

```sh
go test -race -cover ./internal/supervisor
go test -race ./internal/signer -run 'TestWrongProfile'
```

An absence of native events must continue to expire the coordinator's session. Background polling and timers must not refresh its inactivity timestamp.
