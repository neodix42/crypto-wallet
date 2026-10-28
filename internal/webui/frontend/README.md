CryptoWallet's Wails v2 interface uses React 19, TypeScript and Vite, following
the neighboring charting project's frontend stack. Production builds embed
`dist/` in the Go executable. Node/npm are build tools, not runtime dependencies.
The application uses the installed WebKitGTK engine through Wails; it does not
ship Electron or Chromium.

Run `npm ci --ignore-scripts`, `npm test` and `npm run build` in this directory.
Dependencies are pinned in `package-lock.json`. The generated outputs are
`dist/index.html` and `dist/secret/index.html`, with fingerprinted JS/CSS assets.
Source maps and development servers are not part of the production build.

The public entrypoint binds only `window.go.webui.PublicBridge.Snapshot()` and
`Event({ID,Value})`, and listens for `wallet:view`. It renders the Go-authored,
bounded public view as inert text. It has no password or phrase input, private
bridge, clipboard API, browser storage or network requests.

The private entrypoint runs in a separate short-lived Go/Wails process and
webview data context. Only it binds `PrivateBridge.Prompt()`, `Reply(reply)`,
argument-free `Copy()` and value-free `Activity()`. Go validates the current
one-use prompt and remains authoritative for approval, copying and cancellation.
Credential fields stay masked, phrases display as numbered words, and input/word
DOM is cleared before reply. Initial phrase recording has no copy operation;
authenticated backup explicitly offers Copy phrase with a clipboard-history
warning. File and direct public-copy prompt kinds are handled in Go, not by
path input or browser clipboard access. Runtime/JS memory copies are not subject
to a guaranteed erasure claim.

Both HTML documents use a strict local-assets CSP with no outbound connections,
HTML insertion, eval, frames or form navigation. The Go host must retain its
separate public/private bridge allowlists, deny standard privileged Wails runtime
messages and navigation, and supply no secrets to the ordinary public view.

Vitest tests validate allocation bounds, secret-field rejection, prompt kind and
byte limits, inert provider text, stale snapshot ordering, table/copy actions,
tab closing, seven-second status expiry, private masked input, clear-before-reply,
explicit backup copying and revocation. These DOM tests complement the installed
Go/WebKitGTK acceptance checks; they do not certify OS-wide secret erasure or
funded transactions.
