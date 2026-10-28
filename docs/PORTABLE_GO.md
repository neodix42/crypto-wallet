# One-file Go wallet with Wails UI

The owner's 2026-10-28 request replaces the handwritten native Go/GTK interface with the approach used by the sibling `charting` project: **Wails v2.17.0, React and TypeScript**, using system **WebKitGTK 4.1** on Linux. The charting application's main UI is not Electron/Chromium; its optional Screener separately bundles Chromium. CryptoWallet does not include that Screener or Chromium.

The wallet remains one portable Go executable, `cryptowallet`, containing compiled frontend assets, coordinator and signer self-roles, and third-party notices. It needs system GTK3/WebKitGTK libraries rather than a bundled browser runtime. Node/npm are build-time dependencies; downloaded wallets need neither them nor `.tools/`. No `chrome-sandbox`, SUID helper, root ownership adjustment, AppArmor change or kernel sandbox setting is required. The Go roles apply no syscall sandbox. The pinned Wails implementation also explicitly disables WebKitGTK's renderer sandbox before creating its WebView, as requested; it uses no environment bypass or Chromium flags. Ephemeral storage, embedded-only assets and strict browser/bridge restrictions remain application policy rather than kernel confinement.

## Build and run

Ubuntu 24.04/26.04 amd64 runtime prerequisites:

```sh
sudo apt update
sudo apt install -y ca-certificates libgtk-3-0t64 libwebkit2gtk-4.1-0
```

Build prerequisites additionally require `build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev golang-go python3`. The build pins Go 1.27.3 and Node 24.19.0/npm 11.17.0, installs the locked frontend dependencies without lifecycle scripts, compiles React/TypeScript, inventories actual shipped dependencies, then builds Go with `production,webkit2_41,portable`. See [WAILS_UI.md](WAILS_UI.md) for the project-local SDK alternative.

```sh
make                           # bin/cryptowallet
bin/cryptowallet --version
bin/cryptowallet --runtime-info # wails-webkitgtk4.1; portable-go; sandbox=none
bin/cryptowallet --licenses
make install                   # repository-root ./cryptowallet; no sudo
make package-portable          # alias: make package-go
make test-portable             # actual ELF/version/notices/dependency/archive checks
```

`dist/cryptowallet-<version>-linux-amd64.tar.gz` contains exactly one regular 0755 executable, `cryptowallet`, at archive root. No folder wrapper, JSON, evidence, helper or license sidecar is archived. Required Go/Wails/React dependency notices are embedded and available with `--licenses`; independent legal review remains NOT_RUN.

`make install` verifies and atomically replaces only its managed root executable, retaining the installation marker for safe upgrades. Existing managed Electron launcher links can be replaced; previous `wallet-runtime/` generations and user profiles are preserved. An unrelated file/link or unsafe destination is rejected. Downloaded wallets run directly after extraction.

The release workflow compiles/packages once and publishes that exact authenticated artifact in a separate job. Version, bare numeric tag and regular Release name match and automatically increment across numeric and legacy `v` tags. Tests run independently; the release build marks them NOT_RUN and links the exact commit's checks. Detached verification records remain an Actions artifact, not extra release assets. Existing published Electron/GTK binaries are not changed by this source update.

## Data and security

Profile paths remain `~/.config/localwallet/mainnet` or `$XDG_CONFIG_HOME/localwallet/mainnet`, with a separate testnet profile. Installation does not read, replace or migrate a real vault. All nine networks/twelve asset identities, deterministic profiles, encrypted vault formats, real transaction adapters and journal reservations remain required.

Go owns random phrase generation, password/word verification, immutable transaction plans, exact approvals, signatures and encrypted storage. Binary seeds/private and extended keys remain in the signer. The UI and coordinator remain trusted with temporary credential/phrase transport and display. Moving to WebKit adds browser-engine and JavaScript memory copies to this boundary; generation in Go does not protect against a compromised trusted UI. No secrets belong in ordinary portfolio state, logs, browser storage, plaintext files, network requests or QR codes. The authenticated backup dialog's explicit Copy phrase action is the sole phrase clipboard exception; clipboard history can retain it.

The signer remains a distinct process with protected memory and bounded inherited private pipes, but no OS-enforced network confinement. Legacy seccomp/network-denial evidence cannot be assigned to this mode. The browser UI's permitted bridge and origin/session restrictions require their own current evidence; see [WAILS_UI.md](WAILS_UI.md).

## Evidence checklist

Current Wails evidence and exact candidate identities are recorded in [WAILS_UI.md](WAILS_UI.md). Historical GTK results are retained in [NATIVE_GTK_HISTORICAL.md](NATIVE_GTK_HISTORICAL.md); earlier Electron reports remain historical. None certifies a new Wails binary. A rebuilt executable requires affected tests to run against its exact hash.

The native production-evidence verifier and combined GUI-approved real-node Send acceptance remain separate gates until actually implemented and executed. Headless real-geth integration proves real signing/execution only within its recorded scope. Clean Ubuntu 24.04/26.04 X11/Wayland, OS lifecycle, full upgrade/uninstall recovery, funded public testnets, 24-hour soak, independent security/legal review and private funded Bybit acceptance remain NOT_RUN unless separately evidenced.

Explicit `make build-desktop`, `make package-desktop`, `make install-desktop`, `make test-packaged-desktop` and `make test-packaged-send-desktop` retain the historical Electron tooling. The handwritten GTK test driver remains explicitly `make test-native-wallet-legacy`. Neither produces the default release.
