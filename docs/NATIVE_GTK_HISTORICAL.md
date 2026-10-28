# Historical handwritten GTK interface evidence

This records the superseded 2026-10-27 GTK implementation. It does not certify the current Wails/WebKitGTK interface or a rebuilt executable. See [WAILS_UI.md](WAILS_UI.md) for the current design and evidence.

# One-file native Go wallet

The owner's 2026-10-27 request replaces the default Electron distribution with a **single portable Go executable**, `cryptowallet`, using native GTK3 windows. The executable also starts its coordinator and signer as child roles of itself over inherited private pipes. No Electron, Chromium, web view, `chrome-sandbox`, helper program, Node runtime or bundled application directory is needed. No syscall sandbox is installed by this mode.

## Build and runtime

Ubuntu 24.04/26.04 amd64 runtime prerequisites:

```sh
sudo apt update
sudo apt install -y ca-certificates libgtk-3-0t64
```

Build prerequisites add `build-essential golang-go python3`. Go 1.27.3 is pinned; a Go 1.21+ bootstrap installation downloads it if needed. The handwritten GTK3 ABI binding uses system runtime libraries and does not require GTK development headers. The resulting file is dynamically linked to GTK/glib/libc, rather than statically embedding those operating-system libraries.

```sh
make                           # bin/cryptowallet
bin/cryptowallet --version
bin/cryptowallet --runtime-info # native-gtk3; portable-go; sandbox=none
bin/cryptowallet --licenses     # embedded upstream texts
make install                   # repository-root ./cryptowallet
make package-portable          # alias: make package-go
make test-portable              # inspect actual local ELF and archive
```

`dist/cryptowallet-<version>-linux-amd64.tar.gz` contains exactly one regular executable named `cryptowallet`, with mode 0755 and fixed archive ownership/time metadata. No directory wrapper, evidence, JSON, third-party sidecar, separate launcher, backend helper or installer is archived. Upstream notices are collected from the actual `cmd/cryptowallet` dependency closure using checksum-verified module sources, plus the selected Go standard-library texts, and embedded before compilation. Independent legal/license-obligation review is **NOT_RUN**.

The default build/install/release never invokes npm or packages Electron. Node remains an independent developer/test/signing dependency. The publisher uses its pinned Node signing toolkit in a separate job, without rebuilding the wallet. The single archive is a regular GitHub Release asset; release name, bare tag and embedded version match and automatically increment across both numeric and legacy `v` tag histories. Detached signature records stay in the publishing run's Actions artifact, outside release assets.

`make install` verifies the locally built candidate, stages a regular executable and atomically replaces the managed root entry. An existing managed Electron symlink is recognized; its previous `wallet-runtime/` generations are not removed. Subsequent native upgrades recognize a current-user-owned `.cryptowallet-install.json` marker and the current executable's hash. Unrelated files, symlinks, linked/unsafe markers and unsafe destinations are rejected. The marker is local installation metadata and is not a runtime dependency or release asset. Downloaded executables run directly after extraction.

## Data and secret handling

Existing profile paths remain `~/.config/localwallet/mainnet` or `$XDG_CONFIG_HOME/localwallet/mainnet`; testnet stays separate. No installation/build target reads user secret files, creates replacement wallets, migrates derivation profiles or deletes old data. All nine networks/twelve asset identities and existing vault/recovery formats remain required.

The coordinator and signer retain bounded framed IPC, current-user process checks, signer-owned encrypted state and password-authenticated exact-operation approval. The native GTK UI and coordinator necessarily handle temporary password/phrase input/display, so both belong to the trusted secret boundary. Binary seeds and private/extended keys remain in signer code. Existing locked/pending/recovery state rules, secret-memory protections, encrypted credentials, narrow authenticated phrase-copy permission, journal reservation/reconciliation and no-automatic-mainnet-send requirements still apply.

The owner explicitly requested **no sandbox**. This mode therefore does not apply seccomp, Chromium sandboxing or filesystem/user/network namespaces to the application roles. It does not install a root-owned helper, change kernel settings, add AppArmor rules or require `--no-sandbox`. The OS does **not** enforce the previous signer's network denial. Separate processes and private pipes preserve protocol separation, but are not a substitute for syscall confinement. Previously passing sandbox/network-denial tests certify the retained legacy mode only; they must not be cited as evidence for this native build.

The mainnet profile remains the default and no command automatically transmits mainnet funds. An unavailable RPC balance remains unknown, never a verified zero. No real user passwords, phrases or API credentials may be used for tests.

## Evidence checklist

Local checks on 2026-10-27 are deliberately scoped. The final candidate is version 0.1.1, SHA256 `bfc3652c18274f807eb823084bfd68b0c94c9fc0cd3e4d12d7e5612bb4dc18dd`; exact hashes in each report identify what ran. A rebuilt executable or changed source must rerun affected checks. Passing legacy Electron evidence is never assigned to the native build.

| State | Check and evidence |
| --- | --- |
| PASS, recorded candidate | Actual `make package-portable` / `scripts/go-build-test.py`: one regular executable, ELF64 amd64/version/runtime marker, system GTK dependencies with no Electron/Chromium/libseccomp dependency, embedded upstream dependency/toolchain notices, isolated extraction without cache/toolchain or profile creation, and no invocation of prohibited Node/npm/Electron/Debian tools. `reports/go-only-build.json` and `dist/portable-build-verification.json` retain exact hashes. |
| PASS, local regression scope | 58 build/installation regressions, preserving 48 legacy cases and adding 10 native cases: malformed/link/extra archive entries, runtime/license/version/hash mismatch, managed upgrades, interrupted switches, unsafe destinations/markers, unrelated-file refusal and prior-data preservation. `reports/build-tools.json` binds the scripts/workflow inputs. |
| PASS, recorded binary | Six actual temporary-directory installation checks; `reports/portable-install.json` records the exact tested executable SHA256. This temporary-directory proof does not touch the repository-root executable or a real profile. A rebuilt executable with different bytes requires its own rerun. |
| PASS, headless recorded candidate | Three fresh Go application copies recover all nine network wallets at account indices 0, 1 and 127 from the recorded phrase, including changed local-password restoration and tested secret-canary containment. `reports/portable-phrase-recovery.json` records its binary hash. This is not a second physical machine or native GUI acceptance result. |
| PASS, narrowly scoped tests | CLI/root/profile/internal-role/error boundaries pass with race detection; command statement coverage measured 97.4%. Controller routing/lifecycle tests pass with synthetic backend replies; they are not transaction/network acceptance. |
| PASS floors; aggregate FAIL/BLOCKED | The final source-stable five-profile measurement passes all 31 unchanged 90%/95% floors: native GTK 95.61%, controller 96.66%, session monitor 95.87%, supervisor 97.47%, isolation 100%. All required base/native/memory/runtime suites pass. The original Decred simnet prerequisite remains BLOCKED, so `reports/go-coverage.json` honestly remains FAIL; TON and local USDT execution gates PASS. [COVERAGE.md](COVERAGE.md) records skips and all unexecuted blocks. |
| PASS, actual native GUI scope | `reports/native-wallet.json` passes all nine ordinary-candidate scenarios with sourceStable=true: password/generated phrase/recorded-word setup, all nine wallet addresses and copy controls, tab closing, Receive QR, Send presentation/units, authenticated phrase backup/copy, independent fresh-profile phrase restore with a different app password, and locked restart. A separate instrumented GUI run passes for coverage. These are isolated private-X11 workflows, not public-network acceptance. |
| PASS, headless private-node integration | `reports/portable-send.json` binds the final binary and passes eight real private-geth scenarios with actual receipts, amount/fee/nonce checks and no external route. It does not operate the graphical Send/approval path. |
| NOT_RUN / unfinished integration gate | A native production-evidence verifier and combined native GUI/local-node Send campaign remain unfinished. The retained `make verify-release` checks the legacy Electron layout and cannot certify this native build. |
| NOT_RUN until separately evidenced | Clean Ubuntu 24.04/26.04 X11/Wayland matrix, OS suspend/session-lock acceptance, full upgrade/uninstall recovery, public funded testnets, full 24-hour soak, independent security/license review and funded private Bybit acceptance. |

`make test-portable` inspects the actual binary/archive. `make coverage` measures all shipped Go packages and merges original base, retained GTK prompt, new native GTK adapter, separately instrumented full native GUI runtime and targeted portable memory-kernel profiles without excluding shipped production paths. Only tagged test automation/reference generators are explicitly excluded; C ABI statement coverage is not measured by Go. Missing profiles, changed source/counter inputs or below-floor packages remain failures. A successful portable build does not establish production readiness.

## Retained legacy tooling

`make build-components` compiles the previous Go backend helpers. `make build-desktop`, `make package-desktop` and `make install-desktop` retain the historical Go + Electron package for explicit regressions. `make test-packaged-desktop` and `make test-packaged-send-desktop` operate that legacy layout and keep its sandbox requirements. These targets do not produce the default native release. Legacy evidence documents remain useful historical records, not certification of the native implementation.
