# Build and run

This is an unsigned development candidate. The desktop launcher selects the isolated testnet profile. Mainnet adapter code exists, but a production release is blocked by the explicit gates in [STATUS.md](STATUS.md). Do not deposit funds into public synthetic fixture addresses.

The recorded build host is Ubuntu 26.04 amd64. Pins: Go 1.27.3 (the `go` directive also selects the downloaded toolchain), Node 24.19.0, npm 11.17.0, Electron 45.0.0, TypeScript 7.0.2. `go.sum` and every npm package-lock are committed source inputs. Go builds require CGO, a C compiler and installed GTK3/libseccomp runtime shared libraries. The small native binding uses the stable GTK3 ABI directly; it does not require development headers. The runtime package dependencies are generated in `scripts/package.cjs`.

```sh
sudo apt-get update
sudo apt-get install build-essential libgtk-3-0t64 libseccomp2 libnss3 libasound2t64 libgbm1 libxss1 libxtst6 libdrm2 libatspi2.0-0t64 libxcomposite1 libxdamage1 libxrandr2
make package-deb
python3 scripts/sbom.py
sha256sum -c dist/SHA256SUMS
sudo apt-get install ./dist/localwallet-development_0.1.0_amd64.deb
/opt/localwallet/wallet-launcher
```

Use the desktop entry or fixed `wallet-launcher`. Launching Electron directly is rejected. Explicit `--ozone-platform=x11` and `--ozone-platform=wayland` are the only accepted launcher arguments. Each also selects the corresponding GTK backend. The launcher starts the network core, pins private pipes on descriptors 3/4, then installs process-wide IP restrictions before execing Electron. The signer separately installs the same restriction. Application execution as root is rejected.

The package-specific AppArmor profile permits user namespaces for `/opt/localwallet/LocalWallet`. No script disables AppArmor, changes system-wide user-namespace restrictions, uses `--no-sandbox`, installs a secret service, or publishes an artifact. Installation needs the normal package-manager authority. Ordinary removal leaves the user's profile; separately deleting the profile without a verified portable backup destroys recovery data.

Build outputs are ignored by git: `dist/localwallet-development_0.1.0_amd64.deb`, extracted application, `SHA256SUMS`, `build-manifest.json`, `source-manifest.json`, CycloneDX dependency inventory, Go module list and npm lock copy. The source manifest hashes the actual uncommitted implementation and owner specifications; the base commit alone does not identify this candidate. Builds pin dependencies and remove Go build IDs/absolute paths; Electron packaging timestamps and archives mean bit-for-bit reproducibility has not been established. Repeating the build is supported; identical package hashes are a separate NOT_RUN claim. There is no publisher signature or automatic updater. A local binary manifest detects accidental/substituted child files relative to a trusted parent; a same-user attacker who replaces the parent and manifest defeats that check.

Development commands are listed in the Makefile. `make dev` first packages then starts the network-isolating launcher. Tests use public deterministic synthetic inputs and temporary private profiles. Never run a fixture generator against personal wallet data.

Local protocol prerequisites are installed explicitly, before the hermetic gate:

```sh
for name in evm near aptos ton; do npm ci --ignore-scripts --prefix tests/oracles/$name; done
sh tests/oracles/near/install-sandbox.sh
sh tests/oracles/aptos/install-cli.sh
mkdir -p .tools/dcrd
GOBIN="$PWD/.tools/dcrd" go install github.com/decred/dcrd@v1.10.9
docker pull ethereum/client-go:v1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109
make test-integration
```

The validator binaries have hash checks in their harnesses. Geth uses a pinned container digest, synthetic genesis, loopback-only RPC and zero peers. NEAR/Aptos/DCR have temporary chains; TON executes actual TVM messages in the official emulator. ZEN tests execute the pinned canonical contract runtime on a local EVM. These are real protocol tests but do not establish public testnet acceptance. HyperCore has independent official SDK fixtures and adversarial HTTP tests; no official local validator is available in this repository.

`make verify-release` must fail while required evidence is incomplete. Passing unit tests, an installed package, or a high coverage number cannot override it. See [RECOVERY.md](RECOVERY.md), [VAULT_FORMAT.md](VAULT_FORMAT.md), [THREAT_MODEL.md](THREAT_MODEL.md) and the chain evidence documents.
