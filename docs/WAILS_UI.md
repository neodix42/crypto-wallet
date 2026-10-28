# Wails / React UI migration

The 2026-10-28 owner request selects the same main UI architecture as `/home/neodix/gitProjects/charting`: pinned Wails v2.17.0 and React/TypeScript, rendered by system WebKitGTK 4.1 on Linux. Charting's optional Screener includes separate Chromium automation; that feature is not copied into this wallet. CryptoWallet's default UI does not load Electron or Chromium.

## Build environment

```sh
sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev golang-go python3 ca-certificates
make
make package-portable
```

The pinned Node/npm toolchain compiles `internal/webui/frontend` before `go build -tags production,webkit2_41,portable`. The generated frontend is embedded into the executable; no Node runtime, browser bundle, static server, frontend source tree or development cache must accompany it. A graphical desktop and system `libgtk-3-0t64 libwebkit2gtk-4.1-0` remain runtime prerequisites.

When global development headers are unavailable, `scripts/native-env.sh` can use an ignored project-local `.tools/native/usr` SDK. This session copied the already extracted SDK from charting into this repository and relocated only the copied pkg-config records; it changed no system package or charting source. This is a local build convenience, not a release/runtime dependency or an instruction to commit `.tools/`. A clean builder can install the development packages above or extract the corresponding distribution development packages under `.tools/native` and relocate their `.pc` prefix paths there.

The local Wails fork is narrowly pinned in go.mod to v2.17.0 under `third_party/wails`. Its scoped hardening and upstream provenance are recorded with that source. Redistribution metadata records hashes of the actual fork source files. Other local Go dependency replacements remain rejected by the portable notice collector. React/ReactDOM and their production dependency notices are collected from installed npm identities matching the locked integrity/version records; Go and frontend notices are embedded before compilation.

## Trust boundary

The frontend is packaged local code. Portfolio state and ordinary actions must remain separate from private prompt data. Exact prompt request IDs, current lifecycle state and Go-authored details authorize each private reply; arbitrary public callbacks cannot supply approval text or retrieve phrases. The trusted webview/prompt implementation necessarily sees entered credentials and displayed phrase words. No browser storage, disk cache, downloaded executable code, remote fonts or remote UI assets are required. Go seed/private-key ownership, encryption, password reauthentication and original transaction journal rules are unchanged.

Authenticated phrase copying is an argument-free private control request bound to the exact prompt ID, session and monotonically increasing sequence. No words travel in that control message. The parent Go UI authorizes the actual GTK clipboard commit under its lifecycle epoch guard and owns CLIPBOARD afterward, so an authorized copy survives closing its temporary private view. PRIMARY is untouched. Cancellation/lock/close revoke pending controls and synchronously reap the private process before returning; an ambiguous acknowledgment cannot trigger replacement automatically. This does not guarantee OS-wide clipboard erasure: other applications and clipboard history can retain copied words.

The Go coordinator/signer apply no syscall sandbox, as requested. No Chrome SUID helper or root configuration is involved. WebKitGTK is a system browser engine. This pinned Wails fork explicitly disables its renderer sandbox before creating the WebView, as requested, and verifies that state without an environment bypass. Renderer processes still exist; the application is not a static Go drawing library. Application-level CSP/bridge restrictions remain mandatory. Do not add `--no-sandbox` to an old Electron binary.

## Current evidence

The recorded installed candidate is version 0.1.1, SHA256 `b82b8c06efd7f7ed849e9467f0a0aabd328f08f0e92f81e4a740ce47eaabb260`. Reports retain exact identities; later rebuilds require affected checks again. Wails results must be bound to the newly compiled executable or unchanged source. The superseded handwritten GTK GUI/coverage campaigns and historical Electron acceptance do not pass these new interface gates.

| State | Gate / command |
| --- | --- |
| PASS, recorded build scope | Actual one-file Wails build/package and isolated extracted diagnostics: `python3 scripts/go-build-test.py`. The recorded b82b8c candidate has the correct runtime marker, system WebKitGTK dependency, embedded frontend/204557 notice bytes and no profile creation. Electron/Debian tools were prohibited; Node compiled the frontend. A changed executable requires this check again. |
| PASS, actual system-engine scope | `reports/wails-security.json`: eight actual WebKitGTK source-stable checks on disposable X11/HOME prove ephemeral/no-sandbox state, disabled browser storage/developer/clipboard features, strict origin parsing, CSP eval/storage/network denial and blocked popups/navigation. An owned HTTP witness observed zero requests; actual native copying preserves the independent PRIMARY selection. This is not a full wallet workflow. |
| PASS, actual compiled-candidate scope | `reports/wails-inspector.json`: three source-stable checks start the actual b82b8c executable on disposable X11 with an inherited inspector endpoint, observe actual WebKitNetworkProcess/WebKitWebProcess initialization and verify no TCP/HTTP inspector listener exists throughout observation. This is a startup/security probe, not full wallet acceptance. |
| PASS, actual root installation | The same tested executable was installed atomically as repository-root `./cryptowallet`, a regular 0755 file with SHA256 `b82b8c06efd7f7ed849e9467f0a0aabd328f08f0e92f81e4a740ce47eaabb260`. Version 0.1.1/runtime/managed marker match; no real profile was opened. |
| PASS, actual candidate installation | `reports/portable-install.json`: six actual-binary disposable checks pass for b82b8c, including fresh/repeat installation, managed legacy-link replacement, unrelated file/link refusal and tampered-managed-file refusal. This drill does not replace the repository-root executable or open a real profile. |
| PASS, 58 local regressions | Build/install/archive rejection and version/release continuity regression: `python3 scripts/build-tools-test.py` |
| PASS, 15 local regressions | Dependency license/provenance/path regression: `python3 scripts/license-notices-test.py` |
| PASS, current source-stable Go scope | `reports/wails-go-tests.json`: full `go test ./...`, focused Wails/controller/session/IPC/CLI race checks and repository `go vet ./...` pass; the report binds current Go/module/fork sources and exact command logs. Optional integration skips are not promoted to execution passes. |
| PASS, focused Go/race scope | Fourteen Wails adapter tests plus controller and CLI tests pass with race detection, covering private transport/replay/ambiguous-copy acknowledgment, exact GTK copy commit guards, prompt cancellation/reaping and lock/close waits. These use controlled protocol peers; actual WebKit and GUI acceptance remain separately scoped. |
| PASS, 16 component tests | React interaction/security tests: `npm --prefix internal/webui/frontend test`; local component fixtures, not funded/network or actual-window acceptance. |
| PASS, actual uninstrumented GUI | `reports/wails-wallet.json` passes nine workflows plus one graceful-exit row for all three WM_DELETE_WINDOW closes, with sourceStable=true on the exact b82b8c candidate: password/generated-phrase/word-challenge setup, all nine full addresses/copy controls, wallet tabs/middle close, Receive QR, Send-form units, authenticated numbered backup/explicit phrase copy, fresh independent phrase restore under a different password and locked restart/unlock. This is isolated private-X11 acceptance, not a clean OS/Wayland or funded GUI Send result. |
| PASS, recorded headless candidate | `reports/portable-phrase-recovery.json`: six checks on b82b8c candidate compare 27 descriptors across nine networks and indices 0/1/127, restore under the same/different app passwords, restart locked and scan tested public frames/files/logs for synthetic canaries. Three independent one-file installations; not separate physical machines or GUI acceptance. |
| PASS, recorded headless candidate | `reports/portable-send.json`: eight checks on b82b8c candidate execute a real 0.012345678912345678 transfer on pinned private geth. Independent receipt, balance/fee and delayed nonce 1 checks prove one payment; the original journal identity reconciles and survives locked restart. Ordinary UID 1000 and loopback-only network namespace; headless protocol scope, not graphical Send approval. |
| NOT_RUN, driver implemented | Current Wails frontend/runtime coverage and source-bound aggregate evidence: `make coverage`. Exact candidate measurement has not run; old GTK/Electron counters do not certify Wails. |
| NOT_RUN / unfinished | Native production verifier and combined Wails-GUI-approved actual-node Send acceptance |
| NOT_RUN | Clean Ubuntu 24.04/26.04 X11/Wayland and OS lifecycle/upgrade/uninstall matrix; public funded campaigns, 24-hour soak, independent security/legal review, funded private Bybit acceptance |

On Ubuntu 24.04 GitHub-hosted runners, the Wails GUI/geth drivers need their own disposable user/mount/network namespaces. The CI-only helper grants userns permission through an explicitly named, temporary, unattached AppArmor profile solely for exact allowlisted test commands. Both Wails and retained Electron jobs set up this profile, run their fixed commands with `aa-exec`, and always remove it. This does not sandbox the Wails product, change a global sysctl or attach policy to ordinary executables. Twenty offline helper regressions pass, including exact command rejection, setup/failure propagation and cleanup; hosted execution remains NOT_RUN until CI runs.

No real user secrets or existing profile may be used for tests. No automatic mainnet transfer is authorized. A GUI test display/namespace is test infrastructure and does not imply application sandboxing.
