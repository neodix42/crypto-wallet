This is the historical handwritten GTK3 adapter, retained for explicit legacy
regressions. The default wallet uses Wails/React in `internal/webui`; this
package is not imported by the current `cmd/cryptowallet` application.

The old adapter has
no Chromium, Electron, HTTP server or sandbox helper. Its narrow C ABI binding
links the installed GTK3 runtime libraries; it does not require GTK development
headers to compile.

Create `Window` and call `Run` from the same main goroutine. `New` locks that
goroutine to the GTK thread. `Render`, `Dispatch`, `Prompt`, `CancelPrompt` and
`Close` marshal work from backend goroutines. `DispatchControl` is reserved for
value-free idle-lock/lifecycle checks and runs even during a private modal.
Every GTK widget operation runs
on the GTK thread, including private modal dialogs. The GLib dispatch source
permits recursion so cancellation still runs inside a modal event loop; ordinary
backend UI updates wait until the prompt closes.

`View`, `Page`, `Node`, `Option` and `Event` carry public data only. The supported
nodes are label, heading, separator, row, column, button, entry, combo, check and
read-only single-line address. Text is inert, without GTK markup. Public input
IDs referring to passwords, phrases or credentials are rejected. `Tooltip`
allows provider metadata to stay off the visible balance value. A row with an
ID emits that event when its background is clicked; child buttons and inputs
retain their own actions. Notebook changes emit `tab`; middle clicks emit
`closeTab`. Key/button activity emits only a throttled `activity` event without
input text or coordinates. Window destruction emits `close`.

Only `Prompt` accepts private data, using the existing bounded, exact
`uiprompt` schema and one-use identity. Input fields are masked and byte-limited,
then cleared before destruction. Phrase labels are numbered and nonselectable,
then cleared before destruction. Go-owned temporary byte buffers and C strings
are cleared where possible; GTK and immutable Go strings do not provide a
guarantee of complete memory erasure. Initial phrase recording performs no
extra word challenge here because the signer already verifies the subsequent
challenge. Only `recoveryBackup` has an explicit Copy phrase button, with the
authorized clipboard-history warning. A cancellation epoch invalidates pending
prompts, stale replies and backup copying before the GUI close is dispatched.
No private field is connected to the public event callback.

Run public representation tests with `go test ./internal/nativeui`. Run the
actual private GTK adapter tests with:

```sh
python3 internal/nativeui/test-x11.py
```

The helper compiles a race-enabled test binary, creates a fresh temporary HOME
and an unused private Xvfb display, and writes `reports/native-ui.json`. It uses
only frozen public fixtures, retains no private screenshots and never opens an
existing wallet profile. Clean Ubuntu desktop, Wayland and independent security
review remain separate acceptance gates.
