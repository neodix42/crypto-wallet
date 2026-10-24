# Build, runtime and cache dependencies

`.tools/` is an ignored developer cache. No files under it are tracked, and production Go source does not reference it. Do not check it into Git or ship it. It holds optional pinned Node/build tooling, local chain validators, Xvfb and synthetic test profiles. Builds/tests provision what they need. A compiled or downloaded wallet does not need this directory, the source checkout, Go, npm or a build compiler at runtime.

## Go components only

```sh
make                 # same as make build or make build-go
bin/wallet-core --version
bin/wallet-signer --version
bin/wallet-launcher --version
make package-go      # Go executables + checksum/manifest + portable tar.gz
```

This path uses `scripts/with-go.sh` and Go 1.27.3. It does not invoke Node, npm, Electron, a package manager or blockchain test nodes. It requires a bootstrap Go1.21+ installation, Python3 for version/manifests, a C compiler/CGO and installed native runtime libraries. Go can download the pinned toolchain and locked modules on the first build. The native binding uses the installed GTK3 ABI and does not require GTK development headers.

These executables are the existing application's real backend components, not a complete standalone human interface. `wallet-core` accepts length-framed private stdin/stdout IPC. `wallet-signer` supplies isolated GTK credential/approval windows. Keep core, signer and `manifest.json` together. `wallet-launcher` is the Go launcher for the separate complete Electron desktop layout; copying that executable alone does not create the main wallet UI. No component accepts passwords/phrases through command arguments or environment variables.

The runtime dependencies are Linux amd64 and libc; signer also dynamically links GTK3/GObject/GLib/GDK/GdkPixbuf and libseccomp2. Launcher uses libseccomp2. Native dialogs need a normal X11/Wayland graphical session. Node, NEAR/Aptos/DCR validators, Xvfb and `.tools/` are developer/test dependencies only.

## Existing desktop without an installer

The current main wallet interface remains Electron; replacing it with an entirely Go interface is a distinct architecture change. Debian packaging has been removed. Build/install the complete portable layout explicitly:

```sh
make build-desktop
make package-desktop
# Or build, package and install the local runtime in one command:
make install
./wallet
# Optional explicit isolated developer profile:
./wallet --profile=testnet
```

`make install` needs no sudo. It copies a complete staged runtime under `wallet-runtime/` and atomically switches the repository-root `./wallet` launcher. Existing profile files are not in the copy. The installer rejects unrelated files, unsafe/symlinked runtime generations and mismatched hashes, and rechecks the staged copy before switching. Prior managed generations are retained. The installed runtime does not use `.tools/` or system Node/npm; Electron's runtime is bundled.

Pins for this optional desktop build are Node24.19.0, npm11.17.0, Electron45.0.0 and TypeScript7.0.2. The build wrapper bootstraps the SHA256-verified Node distribution in `.tools/` if necessary. Electron, Go SDKs and npm dependencies have locked versions/checksums. No command produces a `.deb` file, invokes `dpkg-deb`, installs a package-specific profile, or changes system-wide sandbox settings. A host that blocks Chromium's user-namespace sandbox remains unsupported until properly configured/tested; the launcher never uses `--no-sandbox`.

The launcher starts the network coordinator on anonymous inherited pipes, then installs process-wide IP restrictions before execing Electron. The secret signer independently applies the same restriction. Running the app as root is rejected. Mainnet is the default; `--profile=mainnet` is explicit and `--profile=testnet` opens the separate developer profile without changing mainnet data. Profile selection is consumed by the Go launcher and passed to the coordinator/signer, not chosen by renderer input. Explicit `--ozone-platform=x11` and `--ozone-platform=wayland` select matching Electron/GTK backends. Start the complete desktop with its Go launcher, rather than bypassing the process boundary by invoking Electron directly.

The desktop uses CPU rendering, selected before Electron is ready, to avoid unnecessary GPU-driver initialization for its forms and account lists. This does not disable Chromium's sandbox or change system GPU-library permissions. A `MESA-LOADER` driver warning is separate from a rejected password, phrase or recovery-confirmation operation; those failures must appear in the native dialog or public status message.

After `make install`, run `python3 scripts/display-startup.py` to probe the actual portable runtime on disposable X11 and virtual Wayland displays, with private home/config/runtime directories and a private session bus. The optional test prerequisites are Xvfb (system or provisioned test cache), Python/Pillow, libX11/libXtst, `dbus-daemon` and `kwin_wayland`; missing prerequisites report **BLOCKED**. `--backend x11` or `--backend wayland` selects one probe. Results are in `reports/display-startup.json`. The probes do not attach to your desktop, enter credentials or access your wallet profile. X11 checks a mapped public window; virtual Wayland checks process startup only. Passing these probes leaves the real supported OS/display acceptance matrix **NOT_RUN**.

Artifacts:

| Path | Contents |
|---|---|
| `bin/wallet-{core,signer,launcher}` | Compiled Go executables |
| `bin/manifest.json`, `bin/go-build-manifest.json` | Binary hashes, version and Go build identity |
| `dist/localwallet-go-<version>-linux-amd64.tar.gz` | Go components only |
| `dist/wallet-{core,signer,launcher}`, `dist/GO-SHA256SUMS` | Raw Go release executables and checksums |
| `dist/go-{sbom.cdx,dependency-licenses,source-manifest}.json` | Go-only module/license/source inventories, hashed by `GO-SHA256SUMS` |
| `dist/LocalWallet-linux-x64/` | Optional full portable desktop |
| `dist/localwallet-<version>-linux-amd64.tar.gz` | Optional complete desktop archive |
| `wallet-runtime/`, `./wallet` | Locally installed complete desktop |

All generated outputs/cache directories stay out of Git. The source manifest hashes the actual working tree; a base commit alone does not identify modified source. Go paths/build IDs are stripped; bit-for-bit archive reproducibility is not yet established. SHA256 manifests do not authenticate a publisher against an attacker who can replace every local file.

## Tests

`make test-go` runs Go unit/corpus tests. `make test` also builds/types/tests the existing desktop. `make test-security`, `make test-race`, `make coverage`, `make test-fuzz`, and `make check-deps` enforce their respective boundaries. The native X11 suite needs Xvfb and XTest. Packaged recovery additionally uses Python/Pillow and `tesseract-ocr` to operate the public screen. Native phrase setup stays inside the Go signer; the disposable test driver reads only its generated synthetic root in memory and refuses saved credential/phrase screenshots. Go command tests explicitly report a skip if an isolated display cannot be provided. A skip is not a pass.

The dedicated recovery drill builds the production Go components and runs a fresh-install phrase restoration test:

```sh
make test-recovery
```

It creates disposable independent application profiles, records a generated phrase only in test memory, restores it under the chosen app password and compares the supported canonical wallets. No original vault, machine keyring or encrypted portable backup file is provided to the destination. The app password encrypts the destination; a different app password must preserve the same derived wallets. Any explicitly used BIP39 passphrase remains a separate recovery input. See [PHRASE_RECOVERY.md](PHRASE_RECOVERY.md) for exact executed cases and whether they passed; a same-host isolated-profile drill is not a second-physical-machine or clean-OS acceptance result.

`make test-packaged` rebuilds/installs and operates the real portable desktop on a private display, including native phrase setup/confirmation, phrase restoration, receive QR/copy and public navigation. It does not create or verify encrypted backup files. Dependencies for those GUI tests are Xvfb/XTest, Pillow and Tesseract. Passing local drills does not establish native Wayland, OS lock/suspend or independent review.

Protocol tests need explicit developer provisioning, not runtime user installation:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
```

The setup pins actual NEAR/Aptos node binaries, dcrd's Go module identity, TON TVM dependencies and a geth image digest. Test workloads run against private chains and use public synthetic material. `make test-e2e` exercises the optional portable desktop; its container probes do not certify clean real X11/Wayland or OS-session events. `make test-soak` runs the24-hour campaign. No script automatically spends mainnet funds.

The exact version comes from VERSION or the validated WALLET_VERSION override. All Go executables support `--version` without opening a profile/native window. `scripts/verify-version.py --go-only` checks Go manifests/runtime versions and any generated component archive; its default mode checks the complete optional desktop archive as well. Controlled tiny ELF/tar fixtures test numeric version bounds, hash failures and atomic installation without creating a Debian package or a remote release.

Production release remains blocked by the actual evidence in [STATUS.md](STATUS.md), including external testnet, installed OS/display, long-running and independent review obligations. A cleaner first-run screen or a development prerelease does not satisfy those gates.
