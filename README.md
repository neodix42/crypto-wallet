# LocalWallet

Go + Electron multichain desktop wallet for Ubuntu amd64. Implemented domains are Ethereum, NEAR, HyperEVM and HyperCore HYPE, Aptos, canonical ZEN on Base and Horizen L3 (plus ETH gas), Decred and TON Wallet V5R1.

**Development testnet candidate.** Public funded testnet roundtrips, the complete clean Ubuntu X11/Wayland matrix, the 24-hour soak and independent security review remain production release gates. See the [evidence checklist](docs/STATUS.md). Local protocol acceptance is tested with actual nodes/emulation; missing external evidence is never treated as a pass. Never fund public test fixtures.

## Build

Install Go 1.21 or newer for toolchain bootstrapping, Python3, Make and native dependencies:

```sh
sudo apt-get update
sudo apt-get install build-essential golang-go python3 curl xz-utils libgtk-3-0t64 libseccomp2 libnss3 libasound2t64 libgbm1 libxss1 libxtst6 libdrm2 libatspi2.0-0t64 libxcomposite1 libxdamage1 libxrandr2
make
# Equivalent: make build
```

Go 1.27.3, Node 24.19.0 and npm 11.17.0 are pinned. The build automatically selects/downloads the Go toolchain. If your Node/npm versions differ or are missing, it downloads the SHA256-verified official Node distribution into `.tools/`; system Node/npm are unchanged. The first build needs internet access for toolchains, locked dependencies and Electron. Failure messages identify the missing prerequisite or failed command. See [build details](docs/BUILD.md).

`make build` compiles `bin/wallet-core`, `bin/wallet-signer`, `bin/wallet-launcher` and the Electron application sources. **Those Go components are not standalone GUI entry points.** To build the complete wallet and put a ready-to-run launcher in this repository's root:

```sh
make install
./wallet
# Explicit display backend when needed:
./wallet --ozone-platform=x11
# or: ./wallet --ozone-platform=wayland
```

`make install` uses no sudo. It copies the entire packaged runtime into `wallet-runtime/` and atomically creates/updates `./wallet`. It refuses to overwrite an unrelated file/directory. Existing profile data stays under `~/.config/localwallet/testnet` (or `$XDG_CONFIG_HOME/localwallet/testnet`). Always start with `./wallet` or the packaged `wallet-launcher`, as that launcher establishes process isolation. Running as root is rejected.

For a Debian installer and portable complete runtime:

```sh
make package-deb
(cd dist && sha256sum -c SHA256SUMS)
sudo apt-get install ./dist/localwallet-development_0.1.0_amd64.deb
/opt/localwallet/wallet-launcher
```

The filename follows [VERSION](VERSION), or the validated `WALLET_VERSION` build override. Outputs:

| Path | Contents |
|---|---|
| `./wallet` | Ready-to-run local launcher after `make install` |
| `wallet-runtime/` | Complete installed local Electron/Go runtime |
| `bin/` | Go component binaries and SHA256 manifest |
| `dist/LocalWallet-linux-x64/` | Complete relocatable packaged application |
| `dist/localwallet-development_<version>_amd64.deb` | Ubuntu installer |
| `dist/localwallet-<version>-linux-amd64.tar.gz` | Portable complete runtime |
| `dist/build-manifest.json`, `source-manifest.json`, `SHA256SUMS`, `sbom.cdx.json` | Build identity, source hashes, artifact hashes and dependency inventory |

## Tests

Use public synthetic fixtures only. No test automatically spends mainnet funds. The default tests run offline after dependencies/toolchains have been installed; optional node integrations explicitly report a skip when their prerequisites are absent.

```sh
make test                 # Go unit/corpus tests, TypeScript build, Electron component tests
make test-security        # vault/IPC/lifecycle, real native X11 tests, mutations, API surface checks
make test-race            # Go race detector
make test-interop         # independent derivation/serialization/signing fixtures
make coverage             # enforced per-package Go and Electron/UI floors
make check-deps           # checksums, vet, reachable Go vulnerabilities, npm audit
make test-stress          # bounded malformed input, concurrent scheduling, production KDF lifecycle
```

Native tests and full coverage require isolated Xvfb and XTest, with Pillow for packaged screenshots:

```sh
sudo apt-get install xvfb libxtst6 python3-pil
make test-native
```

Real local node/emulator integration also requires Docker and explicitly provisioned test dependencies:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
make test-e2e             # package recovery on private X11, then disposable Ubuntu install probes
```

`make test-e2e` does not certify real Wayland/OS-lock acceptance; its container probes can remain blocked by container sandbox policy. No sandbox bypass is applied. `make test-soak` runs the actual 24-hour workload; a short smoke is reported separately. `make test-testnet` records the missing independently verified funded public testnet evidence and fails until that external gate is supplied. `make verify-release` deliberately fails on incomplete mandatory evidence. Reports go to `reports/`, with retained chain fixtures/results in `tests/oracles/` and readable interpretation in `docs/`. Passing unit tests do not imply that skipped integration scenarios ran.

## Automatic versioned releases

[The release workflow](.github/workflows/release.yml) runs on pushes to `main`/`master` when that branch is the repository default, and can be run manually from GitHub Actions. It serializes releases, reads all canonical `vX.Y.Z` tags and [VERSION](VERSION), increments the greatest patch version, runs local/security/coverage gates, builds and checks consistent versions, then publishes a **development prerelease** named/tagged `vX.Y.Z`. The wallet header, Electron metadata, Go runtime identity, Debian package and manifests carry that same version. It uploads the installer, portable runtime, checksums, SBOM/manifests and test evidence. The workflow uses the repository's `GITHUB_TOKEN` with job-scoped `contents: write`; repository policy must allow that permission. No personal token is needed.

For example, after `v0.1.4`, the next successful workflow publishes `v0.1.5`. A failed build creates no release or tag. Raising `VERSION` sets a new minimum before the next increment. No workflow was remotely triggered by implementing this file. The production release gate remains separate and cannot be satisfied by publishing a development prerelease. GitHub documents [manual workflow dispatch](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow) and [workflow token permissions](https://docs.github.com/en/actions/tutorials/authenticate-with-github_token).

## Security and recovery

Only the isolated Go signer receives passwords, phrases, passphrases and keys. Electron supplies the ordinary interface; masked native Go windows supply credential input and authoritative receive/send confirmation. Generated phrases are never displayed/exported. The vault and portable backup use Argon2id (256 MiB/3/4 default) and XChaCha20-Poly1305. Recovery requires a verified encrypted backup and its password; there is no password reset. Read [recovery instructions](docs/RECOVERY.md), [vault format](docs/VAULT_FORMAT.md), [threat model](docs/THREAT_MODEL.md) and [evidence](docs/STATUS.md) before use.

The original normative specifications remain [TASK.md](TASK.md), [ARCHITECTURE.md](ARCHITECTURE.md), [SECURITY.md](SECURITY.md), [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md), [TEST_PLAN.md](TEST_PLAN.md) and [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md). Their requirements are not an audit attestation.
