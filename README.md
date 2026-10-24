# LocalWallet

Go + Electron multichain desktop wallet for Ubuntu amd64. Implemented domains are Ethereum, NEAR, HyperEVM and HyperCore HYPE, Aptos, canonical ZEN on Base and Horizen L3 (plus ETH gas), Decred and TON Wallet V5R1.

**Mainnet is the default network profile.** This remains an unsigned development candidate: public funded testnet roundtrips, the complete clean Ubuntu X11/Wayland matrix, the 24-hour soak and independent security review remain production release gates. Selecting mainnet does not establish production readiness. See the [evidence checklist](docs/STATUS.md). Never fund public test fixtures.

## Build Go binaries

```sh
sudo apt-get update
sudo apt-get install build-essential golang-go python3 libseccomp2
make
# Equivalent: make build / make build-go
```

The default build compiles **only Go executables**: `bin/wallet-core`, `bin/wallet-signer`, `bin/wallet-launcher`. It does not build a Debian package or invoke Node/npm/Electron. Go 1.27.3 is selected automatically from a bootstrap Go 1.21+ installation. Initial toolchain/module downloads need internet access. CGO uses the installed libc/libseccomp2 runtime libraries; no Go window toolkit or GTK development headers are required.

`.tools/` is an ignored developer cache and must **not** be committed. Built/downloaded executables do not need it, Go or the source repository at runtime. Go components need libc/libseccomp2. The complete Electron interface needs its normal graphical runtime libraries and an X11/Wayland session. Blockchain validators and Xvfb are optional test tooling only.

```sh
make package-go
bin/wallet-core --version
# Outputs: dist/localwallet-go-<version>-linux-amd64.tar.gz,
# raw dist/wallet-{core,signer,launcher}, GO-SHA256SUMS and manifests.
```

Go releases also include `go-sbom.cdx.json`, `go-dependency-licenses.json` and `go-source-manifest.json`, with hashes in `GO-SHA256SUMS`. The SBOM lists Go modules only; inventory and hashes do not complete the outstanding license or independent security reviews.

The Go executables are backend/signing components. The existing main wallet screen is Electron; the Go launcher alone cannot supply that interface. To build the complete portable desktop separately, without a Debian installer:

```sh
make install
./wallet
# Alternative explicit steps: make build-desktop / make package-desktop
```

`make install` uses no sudo and creates the ready-to-run `./wallet` launcher plus its complete `wallet-runtime/` directory. It retains previous managed generations and refuses unrelated files/symlinks. If cleanup removed `wallet-runtime/` but left the managed `./wallet` link, rerun `make install` or `make dev` to rebuild it safely. Runtime users need neither `.tools/` nor system Node/npm; the desktop runtime is bundled. A complete downloaded desktop archive similarly needs its bundled files kept together. Replacing this interface with a purely Go application is not achieved by shipping backend binaries.

Node 24.19.0/npm 11.17.0 are needed only for desktop builds/tests and are bootstrapped into `.tools/` when missing. Explicit display backends are `./wallet --ozone-platform=x11` and `./wallet --ozone-platform=wayland`. Always use the Go launcher for the desktop security boundary. Application execution as root and `--no-sandbox` are rejected.

The default profile stores data in `~/.config/localwallet/mainnet` or `$XDG_CONFIG_HOME/localwallet/mainnet`, separately from build caches/runtime files. Existing testnet data stays in its original directory and can be opened explicitly with `./wallet --profile=testnet`. There is no automatic conversion, copying or replacement of an existing vault. An empty mainnet profile requires its own explicit create/import/restore action. Outputs and dependencies are explained in [BUILD.md](docs/BUILD.md). Nothing in these targets creates a `.deb` package.

Public mainnet connections are configured for Ethereum, HyperEVM, HyperCore, Base, Horizen, NEAR, Aptos and TON. Decred remains available and requires an authenticated TLS `dcrd` connection configured through the **Configure trusted RPC** wallet dialog. See [mainnet connection setup](docs/MAINNET.md) for exact endpoints, API-key/rate-limit considerations and Decred setup. An unavailable connection displays an unknown balance; it is never treated as a verified zero.

## Tests

Use public synthetic fixtures only. No test automatically spends mainnet funds. The default tests run offline after dependencies/toolchains have been installed; optional node integrations explicitly report a skip when their prerequisites are absent.

```sh
make test-go              # Go unit/corpus tests without Node/Electron build
make test                 # Go plus existing desktop TypeScript/component tests
make test-security        # vault/prompt/IPC/lifecycle, mutations, API surface checks
make test-race            # Go race detector
make test-interop         # independent derivation/serialization/signing fixtures
make test-recovery        # independent fresh app copies; phrase-only restore of all wallets
make coverage             # enforced per-package Go and Electron/UI floors
make check-deps           # checksums, vet, reachable Go vulnerabilities, npm audit
make test-stress          # bounded malformed input, concurrent scheduling, production KDF lifecycle
```

The fresh-install Go protocol recovery test is headless. The actual packaged Electron drill requires isolated Xvfb/XTest, Pillow and Tesseract; the retained legacy GTK tests also use Xvfb:

```sh
sudo apt-get install xvfb libxtst6 python3-pil tesseract-ocr
make test-dialogs        # isolated Electron prompt, bridge and portfolio tests
make test-native         # retained non-shipped GTK regression tests only
make test-recovery       # headless real Go protocol recovery across fresh app copies
make test-packaged       # actual packaged app: Electron phrase setup, validation, restart/restore
```

Real local node/emulator integration also requires Docker and explicitly provisioned test dependencies:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
make test-e2e             # package recovery on private X11, then disposable Ubuntu portable-runtime probes
```

`make test-e2e` does not certify real Wayland/OS-lock acceptance; its container probes can remain blocked by container sandbox policy. No sandbox bypass is applied. `make test-soak` runs the actual 24-hour workload; a short smoke is reported separately. `make test-testnet` records the missing independently verified funded public testnet evidence and fails until that external gate is supplied. `make verify-release` deliberately fails on incomplete mandatory evidence. Reports go to `reports/`, with retained chain fixtures/results in `tests/oracles/` and readable interpretation in `docs/`. Passing unit tests do not imply that skipped integration scenarios ran.

## First run

A verified empty profile offers **Create app password & wallet** and **Restore from recovery phrase**. Choose and repeat an app password of **12–4096 UTF-8 bytes** in the separate Electron wallet dialog. Go then generates one random English BIP39 recovery phrase and derives every supported network wallet. The separate Electron wallet dialog shows the phrase for you to write down offline. Confirm the requested words before receiving or sending. Cancelling leaves the same encrypted wallet pending; **Finish recovery phrase setup** resumes it after password reauthentication in an Electron wallet dialog. There is no encrypted backup file to create or verify.

On a fresh installation, choose **Restore from recovery phrase**, specify the app password and enter the recorded phrase only in the separate Electron wallet dialog. Include the original optional BIP39 passphrase if one was used. The app password protects local storage and does not change your wallet addresses: either the same or a new app password restores the same canonical wallets. Phrase restoration reconstructs account index0; the Settings recovery-range control reconstructs additional indices. Named/rotated associations, Decred discovery history, RPC settings and the local payment journal are separate metadata and need explicit recovery/reconciliation.

Deterministic account groups appear as tabs across the top, starting with **Main account**. Each group's table contains nine mainnet network wallets and eleven asset identities, with a native-currency balance in every row (ETH on Base/Horizen and separate HYPE balances on HyperEVM/Core). The overview queries configured providers once per visible wallet; **Refresh balances** requests a new observation. Failed or incomplete queries stay unknown/unavailable. Click a row to open its wallet tab. Keys follow each chain's pinned derivation profile; EVM networks can share an address while their balances remain separate. Table addresses stay on one line; the copy-icon button requests the full verified address from the Go signer through an Electron wallet dialog without opening the wallet.

Each wallet tab contains its own balances, asset selector, Send/Receive buttons and network settings. **Send** reveals the otherwise hidden payment form. **Receive** opens the Go-authored Electron wallet dialog with QR code, full address and an explicit **Copy address** button. These actions are enabled after recording and confirming the phrase; pending setup offers a wallet-dialog completion action. Middle-click a wallet tab, or focus it and press Delete, to close it. Closing a tab or form preserves durable reservations. Amounts, network state and Go-validated approval and password checks in the Electron wallet dialog use the existing real transaction pipeline.

The top-right **Settings** button contains phrase restoration for an empty profile, pending phrase-setup completion, password change, deterministic accounts, recovery ranges and the **Local payment journal** with independent wallet/asset selectors. Escape or Close dismisses it. The confirmed current vault has no repeated phrase-display/export API. Existing profiles keep their keys and encrypted data; older vaults require phrase recording once in an Electron wallet dialog under the new policy. See [phrase recovery and verification](docs/PHRASE_RECOVERY.md), [recovery instructions](docs/RECOVERY.md) and the [layout checklist](docs/WALLET_LAYOUT.md).

[Electron interface evidence and limits](docs/ELECTRON_INTERFACE.md) records the current window/protocol tests and the changed trust boundary.

The outstanding implementation, public-network, installed-OS, soak and independent-review work is tracked in the [production-readiness checklist](docs/PRODUCTION_READINESS.md) and [release evidence](docs/STATUS.md).

## Automatic versioned releases

[The release workflow](.github/workflows/release.yml) runs on pushes to `main`/`master` when that branch is the repository default, and can be run manually from GitHub Actions. It serializes releases, reads all canonical `vX.Y.Z` tags and [VERSION](VERSION), increments the greatest patch version, runs local/security/coverage gates, builds and checks consistent versions, then publishes a **development prerelease** named/tagged `vX.Y.Z`. The published Go executable versions and manifests carry that same version. It uploads raw Go component executables, their portable archive, checksums and test evidence. No Debian package is built or uploaded. The Go component assets do not include the Electron interface or any standalone Go windows. The workflow uses the repository's `GITHUB_TOKEN` with job-scoped `contents: write`; repository policy must allow that permission. No personal token is needed.

For example, after `v0.1.4`, the next successful workflow publishes `v0.1.5`. A failed build creates no release or tag. Raising `VERSION` sets a new minimum before the next increment. No workflow was remotely triggered by implementing this file. The production release gate remains separate and cannot be satisfied by publishing a development prerelease. GitHub documents [manual workflow dispatch](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow) and [workflow token permissions](https://docs.github.com/en/actions/tutorials/authenticate-with-github_token).

## Security and recovery

Go generates the root, checks passwords/recovery words, validates plans and signs. Every application window now uses Electron. A dedicated temporary prompt has a separate origin, preload and nonpersistent session; the ordinary portfolio never receives secrets. Prompted passwords/phrases pass through the coordinator and Electron main to that dialog, expanding the trusted secret boundary. Compromised Electron main or prompt code can capture them; generating the phrase in Go does not remove that risk. Binary seeds, entropy and private/extended keys remain in Go. Inputs are masked, and secret fields clear before reply/cancel/lock. No secret clipboard, QR, browser storage, plaintext files, logs or network requests are allowed. The local vault remains encrypted with Argon2id (256 MiB/3/4 default) and XChaCha20-Poly1305. Phrase recovery replaces encrypted-file backup/export/restore. OS/display compromise and whole-directory rollback remain limitations; see the [threat model](docs/THREAT_MODEL.md), [vault format](docs/VAULT_FORMAT.md) and [evidence](docs/STATUS.md).

The original normative specifications remain [TASK.md](TASK.md), [ARCHITECTURE.md](ARCHITECTURE.md), [SECURITY.md](SECURITY.md), [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md), [TEST_PLAN.md](TEST_PLAN.md) and [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md). Their requirements are not an audit attestation.
