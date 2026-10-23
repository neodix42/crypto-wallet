# LocalWallet

Go + Electron multichain desktop wallet for Ubuntu amd64. Implemented domains are Ethereum, NEAR, HyperEVM and HyperCore HYPE, Aptos, canonical ZEN on Base and Horizen L3 (plus ETH gas), Decred and TON Wallet V5R1.

**Development testnet candidate.** Public funded testnet roundtrips, the complete clean Ubuntu X11/Wayland matrix, the 24-hour soak and independent security review remain production release gates. See the [evidence checklist](docs/STATUS.md). Local protocol acceptance is tested with actual nodes/emulation; missing external evidence is never treated as a pass. Never fund public test fixtures.

## Build Go binaries

```sh
sudo apt-get update
sudo apt-get install build-essential golang-go python3 libgtk-3-0t64 libseccomp2
make
# Equivalent: make build / make build-go
```

The default build compiles **only Go executables**: `bin/wallet-core`, `bin/wallet-signer`, `bin/wallet-launcher`. It does not build a Debian package or invoke Node/npm/Electron. Go 1.27.3 is selected automatically from a bootstrap Go 1.21+ installation. Initial toolchain/module downloads need internet access. CGO uses installed native shared libraries; no GTK development headers are required.

`.tools/` is an ignored developer cache and must **not** be committed. Built/downloaded executables do not need it, Go or the source repository at runtime. Signer dialogs need installed GTK3/libseccomp and an X11/Wayland session. Blockchain validators and Xvfb are optional test tooling only.

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

`make install` uses no sudo and creates the ready-to-run `./wallet` launcher plus its complete `wallet-runtime/` directory. It retains previous managed generations and refuses unrelated files/symlinks. Runtime users need neither `.tools/` nor system Node/npm; the desktop runtime is bundled. A complete downloaded desktop archive similarly needs its bundled files kept together. Replacing this interface with a purely Go application is not achieved by shipping backend binaries.

Node 24.19.0/npm 11.17.0 are needed only for desktop builds/tests and are bootstrapped into `.tools/` when missing. Explicit display backends are `./wallet --ozone-platform=x11` and `./wallet --ozone-platform=wayland`. Always use the Go launcher for the desktop security boundary. Application execution as root and `--no-sandbox` are rejected.

Profile data remains in `~/.config/localwallet/testnet` or `$XDG_CONFIG_HOME/localwallet/testnet`; it is separate from build caches/runtime files. Outputs and dependencies are explained in [BUILD.md](docs/BUILD.md). Nothing in these targets creates a `.deb` package.

## Tests

Use public synthetic fixtures only. No test automatically spends mainnet funds. The default tests run offline after dependencies/toolchains have been installed; optional node integrations explicitly report a skip when their prerequisites are absent.

```sh
make test-go              # Go unit/corpus tests without Node/Electron build
make test                 # Go plus existing desktop TypeScript/component tests
make test-security        # vault/IPC/lifecycle, real native X11 tests, mutations, API surface checks
make test-race            # Go race detector
make test-interop         # independent derivation/serialization/signing fixtures
make coverage             # enforced per-package Go and Electron/UI floors
make check-deps           # checksums, vet, reachable Go vulnerabilities, npm audit
make test-stress          # bounded malformed input, concurrent scheduling, production KDF lifecycle
```

Native tests and full coverage require isolated Xvfb and XTest. Packaged recovery also uses Pillow and Tesseract to operate the public screen:

```sh
sudo apt-get install xvfb libxtst6 python3-pil tesseract-ocr
make test-native
```

Real local node/emulator integration also requires Docker and explicitly provisioned test dependencies:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
make test-e2e             # package recovery on private X11, then disposable Ubuntu portable-runtime probes
```

`make test-e2e` does not certify real Wayland/OS-lock acceptance; its container probes can remain blocked by container sandbox policy. No sandbox bypass is applied. `make test-soak` runs the actual 24-hour workload; a short smoke is reported separately. `make test-testnet` records the missing independently verified funded public testnet evidence and fails until that external gate is supplied. `make verify-release` deliberately fails on incomplete mandatory evidence. Reports go to `reports/`, with retained chain fixtures/results in `tests/oracles/` and readable interpretation in `docs/`. Passing unit tests do not imply that skipped integration scenarios ran.

## First run

A verified empty profile offers **Create app password & wallet**. The password is entered and confirmed only in the native Go window. After that, Go draws random entropy, generates one shared English BIP39 recovery root, encrypts it and derives the initial wallet for every configured supported network at index0. The generated phrase is hidden; it is not a common fixed phrase or a password-derived seed.

The network wallets appear together under **Main account** with separate network/asset identities and public addresses. Keys follow each chain's pinned derivation profile; EVM networks can share an address. Balances remain unknown until queried, and deriving an address does not activate its network account.

Create and verify an encrypted backup after setup. Both the backup and its password are needed for recovery; no password reset or seed reveal exists. Existing profiles open an unlock screen. Unreadable storage or an orphaned previous vault opens a storage-unavailable state instead of offering a new root. The current desktop profile is explicitly labeled testnet; **Main account** is the name for index0, not a mainnet claim.

The release-status panel lists the outstanding public-network, installed-OS, soak and independent-review gates. Passing local tests does not establish production readiness, so the application continues to state its actual verification status.

## Automatic versioned releases

[The release workflow](.github/workflows/release.yml) runs on pushes to `main`/`master` when that branch is the repository default, and can be run manually from GitHub Actions. It serializes releases, reads all canonical `vX.Y.Z` tags and [VERSION](VERSION), increments the greatest patch version, runs local/security/coverage gates, builds and checks consistent versions, then publishes a **development prerelease** named/tagged `vX.Y.Z`. The published Go executable versions and manifests carry that same version. It uploads raw Go component executables, their portable archive, checksums and test evidence. No Debian package is built or uploaded. The Go component assets do not include the existing desktop interface. The workflow uses the repository's `GITHUB_TOKEN` with job-scoped `contents: write`; repository policy must allow that permission. No personal token is needed.

For example, after `v0.1.4`, the next successful workflow publishes `v0.1.5`. A failed build creates no release or tag. Raising `VERSION` sets a new minimum before the next increment. No workflow was remotely triggered by implementing this file. The production release gate remains separate and cannot be satisfied by publishing a development prerelease. GitHub documents [manual workflow dispatch](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow) and [workflow token permissions](https://docs.github.com/en/actions/tutorials/authenticate-with-github_token).

## Security and recovery

Only the isolated Go signer receives passwords, phrases, passphrases and keys. Electron supplies the ordinary interface; masked native Go windows supply credential input and authoritative receive/send confirmation. Generated phrases are never displayed/exported. The vault and portable backup use Argon2id (256 MiB/3/4 default) and XChaCha20-Poly1305. Recovery requires a verified encrypted backup and its password; there is no password reset. Read [recovery instructions](docs/RECOVERY.md), [vault format](docs/VAULT_FORMAT.md), [threat model](docs/THREAT_MODEL.md) and [evidence](docs/STATUS.md) before use.

The original normative specifications remain [TASK.md](TASK.md), [ARCHITECTURE.md](ARCHITECTURE.md), [SECURITY.md](SECURITY.md), [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md), [TEST_PLAN.md](TEST_PLAN.md) and [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md). Their requirements are not an audit attestation.
