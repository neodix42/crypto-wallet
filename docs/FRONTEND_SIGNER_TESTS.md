# Desktop and signer command evidence

Observed on 2026-10-23 with the repository's pinned Go, Node, Electron, TypeScript, jsdom and c8 dependencies. All credential fixtures are public test values, including the published BIP39 zero-entropy example. These tests never submit a transaction or access real user secrets.

The desktop exposes fixed public operations, a read-only `getVersion()` and a value-free `onLocked(callback)`. Version metadata comes from Electron's packaged application version through a separate origin-checked IPC handler. The renderer cannot choose an IPC channel or access native event objects. OS lock, suspend, shutdown, explicit lock, core failure and locked status notifications clear cached accounts, assets, QR source bytes, amounts, recipient, memo, balances, history, diagnostics and the prepared plan. Status polling runs every five seconds only while no request is pending; it never emits activity. Generation checks discard replies from a view invalidated by a lock, an account change or a form edit. Native-capable requests have a six-minute transport deadline; the Go coordinator enforces the five-minute inactivity lock. A current, value-free native activity frame renews only its existing request deadline. It never resolves that request or manufactures background activity. Non-native, stale, malformed or excessive activity fails closed.

Hidden accounts retain their original derivation indices and can be shown and unhidden. Explicit inclusive recovery ranges reconstruct at most twenty groups through the signer. Decred's block budget is visible and strictly bounded at two million. Editing a preview does not silently drop its durable reservation: a PREPARED journal row offers explicit cancellation of that original unsigned identity. Signed, awaiting-confirmation and submitted identities have no such cancellation control.

The first screen uses the signer's explicit vault state. Only confirmed `missing` storage with `walletExists:false` offers creation/restoration; `present` offers unlock, and `unavailable` offers a storage retry. No credential dialog opens automatically. The explicit Create app password action sends `{words:12}` to the native signer and suppresses duplicate credential requests while it is pending. The renderer never receives the generated root or password. After success, every returned network descriptor appears under Main account at index zero. The current actual registry contributes twelve testnet/local-network wallets and fourteen asset identities. A generated public DTO fixture binds that list to the registry sources; its account addresses are synthetic renderer fixtures, not native derivation evidence. The Testnet profile label remains visible independently of the compact release-status link. Backup guidance requires the encrypted backup and its password; it does not offer a phrase reveal or password reset.

| Check | Status | Evidence and scope |
| --- | --- | --- |
| All shipped JavaScript included in coverage | PASS | `python3 scripts/frontend-coverage.py`; 35 tests, zero skips; main, preload, validation, codec and renderer each 100% statements and 100% measured V8 branches |
| Mandatory frontend floors | PASS | Main/preload/schema/codec remain at 95% statements and 90% branches; renderer remains at 90%/85%; no shipped file exclusions or reduced floors |
| Hostile renderer and framed streams | PASS | Actual compiled sources in VM; faithful Electron API doubles; actual Node streams; sender/main-frame/origin binding, duplicate and escaped JSON members, replay, wrong method/session/ID, fragmented/coalesced/truncated frames, bounded buffers, permissions, navigation and lifecycle |
| Actual renderer behavior | PASS | Actual HTML/JavaScript in jsdom; text-only hostile metadata, exact decimal strings, public receive/QR controls, hide/show/range controls, cancellation, lock clearing and delayed-reply rejection |
| First-launch and existing-vault UI | PASS | Actual renderer in jsdom; missing/present/unavailable and inconsistent snapshots, explicit one-request native creation boundary, all twelve registry wallet cards/fourteen asset identities, index-zero Main account, native cancellation, backup guidance and lock clearing |
| New generated-wallet onboarding in installed Electron | NOT_RUN by this suite | The jsdom public RPC boundary uses synthetic account descriptors; separate actual packaged/native acceptance must establish password entry, generation and backup with the production signer |
| Signer command and real engine integration | PASS | `go test -race ./cmd/wallet-signer -count=1 -timeout=90s -coverprofile=/tmp/wallet-signer-main-race.cover`; Recorded 98.6% Go statement coverage; dispatch, response and bounded idle serve loop each 100%; final source-bound aggregate coverage is generated separately after version/transport changes |
| Real signing and encrypted recovery | PASS | Actual encrypted `vault.Files`, authenticated backup/reopen/restore, password rotation, expected environment, native approval test UI, actual EIP-1559 bytes and independent sender recovery; signed identity is persisted before returning and retained across unknown-state recovery |
| Native response activity | PASS | Actual GTK dialogs and public XTest key events on an owned private Xvfb display; activity contains only `{}` with the current request ID/session; broken-pipe suppression and callback removal after final reply, error and panic |
| Signer native startup and IPC | PASS | Actual seccomp, GTK initialization and production main/serve loop; EOF, replay, foreign session, oversized/truncated input, failed output, idle native pumping and bounded read-ahead |
| Signer startup error subprocesses | PASS | Invalid environment exits 2; unavailable display exits 4; child coverage is not added to the parent Go profile |
| Remaining signer command coverage | NOT_RUN | Isolation-install failure and unsafe-profile startup exits are not forced in this suite; no production injection hook was introduced to cover them |
| Clean installed desktop, OS lock/suspend and Wayland matrix | NOT_RUN by this suite | Component doubles and private X11 tests do not establish these acceptance gates; consult the separate package/native reports |
| Public-network or mainnet funding | NOT_RUN | No funds are automatically sent; local chain execution evidence is recorded separately in `EVM_CORE.md`, `DCR_TON.md` and `NEAR_APTOS.md` |
| Independent external security review | NOT_RUN | Coverage is not a review or a proof of secret erasure |

Reproduce from the repository root:

```sh
npm --prefix desktop run check
node --check desktop/ui/renderer.js
python3 scripts/frontend-coverage.py
./scripts/with-go.sh python3 desktop/tests/generate-registry-fixture.py # only after a reviewed registry change
go test -race ./cmd/wallet-signer -count=1 -timeout=90s -coverprofile=/tmp/wallet-signer-main-race.cover
go tool cover -func=/tmp/wallet-signer-main-race.cover
go vet ./cmd/wallet-signer
```

The signer tests reuse an explicitly supplied private `:18x` display or start a test-only Xvfb from PATH or `.tools/xvfb/root/usr/bin/Xvfb`. They never attach to the desktop user's `:0` display. The server lives until the test process finishes because GTK retains its display connection between tests. If no isolated display is available, the relevant tests explicitly report `NOT_RUN` as skips; that is not native acceptance and the lost coverage remains visible. Legal test KDF parameters are 64 MiB, three passes and four lanes; the production default and format's minimum checks remain unchanged.

`reports/frontend-coverage.json` and `reports/desktop/coverage-summary.json` are generated evidence. The final signer race run also produced `reports/signer-command.json` and `reports/signer-command.cover`: 46 test/subtest outcomes, zero skips, exact source hashes and the command's coverage table. `desktop/tests/runtime.test.cjs` explicitly distinguishes its Electron boundary doubles from packaged integration. `cmd/wallet-signer/main_test.go` uses the real engine and adapters rather than a fake transaction implementation.
