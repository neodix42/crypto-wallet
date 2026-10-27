# LocalWallet

Go + Electron multichain desktop wallet for Ubuntu amd64. Implemented domains are Ethereum ETH and USDT, NEAR, HyperEVM and HyperCore HYPE, Aptos, canonical ZEN on Base and Horizen L3 (plus ETH gas), Decred and TON Wallet V5R1. The optional **Exchange** tab connects to your custodial Bybit account for Deposit, Swap and Withdraw. [Repository implementation instructions](START_HERE.txt).

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

A new NEAR implicit address is not an on-chain account until it receives NEAR. A confirmed missing account now displays zero with activation guidance; transport, authority and malformed-response failures remain unknown. Decred without a configured node displays connection guidance. Its current bounded scan also limits historical mainnet recovery: see [balance troubleshooting and verification](docs/BALANCE_CONNECTIVITY.md) before configuring a scan.

## Tests

Use public synthetic fixtures only. No test automatically spends mainnet funds. The default tests run offline after dependencies/toolchains have been installed; optional node integrations explicitly report a skip when their prerequisites are absent.

```sh
make test-go              # Go unit/corpus tests without Node/Electron build
make test-exchange        # Bybit/signing/IPC/UI guards, mutations and explicit local USDT integration
make test                 # Go plus existing desktop TypeScript/component tests
make test-security        # vault/prompt/IPC/lifecycle, mutations, API surface checks
make test-race            # Go race detector
make test-interop         # independent derivation/serialization/signing fixtures
make test-recovery        # independent fresh app copies; phrase-only restore of all wallets
make test-crash           # owned-process termination, restart and synthetic secret canaries
make test-release-auth    # real offline publisher-provenance and tamper rejection
make test-release-evidence # exact candidate/source/coverage evidence checks
make test-license-notices # redistribution-text provenance and path/byte checks
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
make test-packaged-send  # actual Electron Send on an isolated real local geth chain
```

Real local node/emulator integration also requires Docker and explicitly provisioned test dependencies:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
make test-e2e             # package recovery on private X11, then disposable Ubuntu portable-runtime probes
```

`make test-e2e` does not certify real Wayland/OS-lock acceptance; its container probes can remain blocked by container sandbox policy. No sandbox bypass is applied. `make test-soak` runs the actual 24-hour workload; a short smoke is reported separately. `make test-testnet` records the missing independently verified funded public testnet evidence and fails until that external gate is supplied. `make verify-release` deliberately fails on incomplete mandatory evidence. Reports go to `reports/`, with retained chain fixtures/results in `tests/oracles/` and readable interpretation in `docs/`. Passing unit tests do not imply that skipped integration scenarios ran.

The current owner excludes Decred implementation work and public testnet campaigns. Longer local fuzz and soak runs have explicit narrower commands:

```sh
make test-fuzz-long FUZZ_SECONDS=60  # discover current targets; Decred explicitly deferred
make test-soak-non-decred           # fixed test binary, 24 hours, local fake ledger only
# Short runner check, which never passes the 24-hour gate:
make test-soak-non-decred SOAK_SECONDS=1
```

The longer fuzz workflow uses 300 seconds per target on its schedule. Excluded targets remain NOT_RUN in the aggregate report. The non-Decred soak writes live progress to `reports/soak.json` and its pinned source/binary identity to `reports/nondecred-soak-run.json`; RUNNING is not PASS. Its EVM fake-ledger workload does not certify other-chain consensus, desktop or all-chain soak acceptance. `make test-packaged-send` needs Xvfb/XTest, Tesseract, user/network namespaces and the exact cached geth image documented by its test helper; it never reaches public networks or uses an existing wallet profile.

## First run

The main window initially opens at 1440×960, bounded by your display’s usable area. Unlock uses a compact password dialog with its action buttons directly below the input; longer phrase, approval and Receive dialogs retain their scrollable layouts.

A verified empty profile offers **Create app password & wallet** and **Restore from recovery phrase**. Choose and repeat an app password of **12–4096 UTF-8 bytes** in the separate Electron wallet dialog. Go then generates one random English BIP39 recovery phrase and derives every supported network wallet. The separate Electron wallet dialog shows the phrase for you to write down offline. Confirm the requested words before receiving or sending. Cancelling leaves the same encrypted wallet pending; **Finish recovery phrase setup** resumes it after password reauthentication in an Electron wallet dialog. There is no encrypted backup file to create or verify.

On a fresh installation, choose **Restore from recovery phrase**, specify the app password and enter the recorded phrase only in the separate Electron wallet dialog. Include the original optional BIP39 passphrase if one was used. The app password protects local storage and does not change your wallet addresses: either the same or a new app password restores the same canonical wallets. Phrase restoration reconstructs account index0; the Settings recovery-range control reconstructs additional indices. Named/rotated associations, Decred discovery history, RPC settings and the local payment journal are separate metadata and need explicit recovery/reconciliation.

Deterministic account groups appear as tabs across the top, starting with **Main account**. Each group's table contains nine mainnet network wallets and twelve asset identities, with a native-currency balance in every row (ETH on Base/Horizen and separate HYPE balances on HyperEVM/Core). Ethereum also supports six-decimal USDT at its pinned Ethereum contract; USDT payments require Ethereum ETH for gas. This adds no TON USDT jetton or other stablecoin network. The overview queries configured providers once per visible wallet; **Refresh balances** requests a new observation. Verified amounts display only their exact decimal value; observation state, block and provider details appear on hover. Failed or incomplete queries stay unknown/unavailable. Click a row to open its wallet tab. Keys follow each chain's pinned derivation profile; EVM networks can share an address while their balances remain separate. Receive addresses occupy their complete single-line width; narrow windows scroll the table instead of truncating them. The copy icon requests the full verified address from Go without opening the wallet.

Each wallet tab always shows its receive address and copy icon. Its selected asset appears in the title and tab name. Multi-asset wallets use asset buttons instead of a dropdown, preserving ETH/USDT and ETH/ZEN choices; new views start with their native asset. Balances show the amount and currency, with observation metadata on hover. **Send** reveals the otherwise hidden payment form, without a QR-image scan control. **Amount** uses whole asset units: entering `1` means 1 ETH or 1 TON, with exact decimal conversion in Go. **Receive** opens the Go-authored Electron wallet dialog with QR code, full address and an explicit **Copy address** button. These actions are enabled after recording and confirming the phrase; pending setup offers a completion action in the bottom status bar. Middle-click a wallet tab, or focus it and press Delete, to close it. Closing a tab or form preserves durable reservations. Go-validated approval and password checks use the existing real transaction pipeline.

The **Settings** tab sits immediately to the right of **Exchange** in the main tab bar. It opens in the main window and contains **Wallet**, **Exchange** and **Journal** tabs. Wallet settings contain phrase restoration for an empty profile, pending phrase-setup completion, password change, deterministic accounts and recovery ranges. **Settings → Journal** contains the **Local payment journal** with independent wallet/asset selectors. Select an account or wallet tab to return to it. Information and error feedback stays in a compact 52-pixel bottom status bar, including validation failures. Messages clear automatically after seven seconds with no Dismiss button; new messages start a fresh interval, while idle lock polling cannot revive expired notices. Long messages scroll inside the bar without moving the page. Tab navigation focuses its tab, without outlining the page heading. Password, recovery, payment approval and Receive QR still use separate isolated Electron dialogs. The confirmed current vault has no repeated phrase-display/export API. Existing profiles keep their keys and encrypted data; older vaults require phrase recording once in an Electron wallet dialog under the new policy. See [the current interface and evidence](docs/WALLET_STATUS.md), [phrase recovery](docs/PHRASE_RECOVERY.md) and [recovery instructions](docs/RECOVERY.md).

[Electron interface evidence and limits](docs/ELECTRON_INTERFACE.md) records the current window/protocol tests and the changed trust boundary.

The implemented hardening and its exact test scopes are tracked in the [hardening evidence checklist](docs/PRODUCTION_HARDENING.md). Outstanding implementation, public-network, installed-OS, soak and independent-review work remains in the [production-readiness checklist](docs/PRODUCTION_READINESS.md) and [release evidence](docs/STATUS.md).

Accounts and the local payment journal now use bounded pages, including large encrypted archives. **Settings → Journal** exposes **Load older payments** and **Load older chain history**; continuations expire on lock or context/generation change. TON traverses exact hash-linked archival transactions and Core traverses inclusive timestamp pages within their documented budgets. Incomplete or saturated queries remain explicit. See [history limits and tests](docs/HISTORY_PAGINATION.md).

## Exchange with Bybit

Open the main **Settings** tab, choose its **Exchange** tab, select your Bybit API region and use **Connect Bybit API key**. Enter the **API key** and matching **API secret** from a Bybit system-generated key only in the separate masked wallet dialogs. The API key identifies the connection; the secret signs authenticated requests using HMAC. A key alone cannot authorize trading or withdrawals. “System-generated” refers to the key type created in Bybit, not a new wallet-generated credential; self-generated RSA keys are not supported. Credentials are stored in the encrypted local vault; the ordinary portfolio receives connection status and a key suffix. Configure only the required spot, account-transfer and withdrawal permissions, restrict the key to an appropriate IP, and whitelist the exact owned withdrawal addresses in Bybit. Withdrawals require a master-account key. Connecting or refreshing performs no trade or transfer. Deposit, Swap and Withdraw stay in the main **Exchange** tab.

Use a **Bybit Unified Trading Account**. This flow uses Funding and Unified balances; legacy Classic-only spot accounts are not supported.

“Confirmation” means reviewing the exact operation in this wallet and entering your app password. The wallet then sends the approved payment or Bybit API request; no separate manual confirmation API is needed. Blockchain deposit confirmations are a different process and accumulate as blocks are produced. Bybit account, whitelist and compliance restrictions still apply.

1. **Deposit:** select a wallet/network and asset, refresh the market, enter the amount, then approve the existing wallet payment. Go checks the exact Bybit chain and full token identity and includes the required TON memo. Track this on-chain payment in **Settings → Journal**; chain submission does not prove Bybit credit.
2. **Swap:** choose Sell for asset → USDT or Buy for USDT → asset, refresh the latest spot price, enter the exact spending amount and a 0.01–1% slippage limit, then approve. An internal Funding ↔ Unified transfer, when needed, has its own confirmation. Orders disable leverage and borrowing; the last price is an observation and execution can be partial.
3. **Withdraw:** independently select the destination wallet and asset, check its exact network and fee, then approve a withdrawal to that wallet's owned address. The amount includes Bybit's fee. Bybit controls custody, availability and fees; a completed exchange report does not prove on-chain receipt or finality.

UAE withdrawals collect eight documented beneficiary fields through masked wallet dialogs and transmit them only to the selected UAE API endpoint. Türkiye, Kazakhstan, EU and Indonesia withdrawals remain gated before any funding transfer because their required questionnaires are not implemented. Unsupported or ambiguous asset/network routes stay unavailable; the existing wallets remain available. A lost withdrawal acknowledgment stays UNKNOWN because Bybit's record API cannot look up its request ID; never submit a replacement to resolve a timeout.

The Exchange asset selector names the asset. Its **Token contract** identifies the token, while the separately labeled wallet address identifies your account. Ethereum USDT uses Tether's contract `0xdAC17F958D2ee523a2206206994597C13D831ec7`; that contract is not your receive address. ETH and supported USDT in the same Ethereum account use the same owned receive address, on the Ethereum network. Never use the token contract as a deposit destination.

The recovery phrase reconstructs wallet keys, including the Ethereum address used for USDT. It cannot restore Bybit API credentials, beneficiary data, exchange account access or the local exchange journal. See the [Exchange guide and evidence checklist](docs/EXCHANGE.md), [Settings layout and evidence scope](docs/EXCHANGE_SETTINGS.md) and [API contract](docs/BYBIT_API.md). Local unit, race, fuzz and mutation evidence passes in the documented scopes; private Bybit acceptance and funded deposit/trade/withdrawal roundtrips are **NOT_RUN**. No real credentials or mainnet funds were used during development.

## Automatic versioned releases

[The release workflow](.github/workflows/release.yml) runs on pushes to `main`/`master` when that branch is the repository default, and can be run manually from GitHub Actions. It serializes releases, reads all canonical `vX.Y.Z` tags and [VERSION](VERSION), increments the greatest patch version, runs local/security/coverage gates, builds and checks consistent versions, then publishes a **development prerelease** named/tagged `vX.Y.Z`. Electron and all Go executables display that same version. Releases include the complete ready-to-run Electron/Go portable wallet, separate Go components, checksums, dependency/source inventories, actual third-party license texts and test evidence. No Debian package is built or uploaded.

The workflow authenticates the exact complete checksum manifest using GitHub OIDC/Sigstore provenance and verifies its publisher repository, immutable repository ID, workflow, branch and full source commit before publication. Missing authentication or payloads fail the job. Follow [publisher verification instructions](docs/BUILD.md) before extracting or running a downloaded wallet. Local builds remain unsigned and need no publishing credentials. Actions and the verification CLI are pinned; remote workflow/signing/publication has not been run by this implementation. The workflow uses job-scoped GitHub contents, attestation and OIDC permissions; no personal token or long-lived signing key is required. [License notice collection](docs/LICENSE_NOTICES.md) is technical evidence, while independent legal review remains separate.

For example, after `v0.1.4`, the next successful workflow publishes `v0.1.5`. A failed build creates no release or tag. Raising `VERSION` sets a new minimum before the next increment. No workflow was remotely triggered by implementing this file. The production release gate remains separate and cannot be satisfied by publishing a development prerelease. GitHub documents [manual workflow dispatch](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow) and [workflow token permissions](https://docs.github.com/en/actions/tutorials/authenticate-with-github_token).

## Security and recovery

The launcher filters three known Chromium X11 atom-cache messages (`_NET_WM_STATE_MODAL`, `application/vnd.portal.filetransfer` and `application/vnd.portal.files`). These report successful lazy registration of modal/clipboard identifiers. Other diagnostics remain visible; modal dialogs and sandboxing remain enabled. Run `make install` and restart `./wallet` to update an older installation. See the [X11 diagnostic explanation and measured checks](docs/X11_DIAGNOSTICS.md).

Go generates the root, checks passwords/recovery words, validates plans and signs. Every application window now uses Electron. A dedicated temporary prompt has a separate origin, preload and nonpersistent session; the ordinary portfolio never receives secrets. Prompted passwords/phrases pass through the coordinator and Electron main to that dialog, expanding the trusted secret boundary. Compromised Electron main or prompt code can capture them; generating the phrase in Go does not remove that risk. Binary seeds, entropy and private/extended keys remain in Go. Inputs are masked, and secret fields clear before reply/cancel/lock. No secret clipboard, QR, browser storage, plaintext files, logs or network requests are allowed. The local vault remains encrypted with Argon2id (256 MiB/3/4 default) and XChaCha20-Poly1305. Phrase recovery replaces encrypted-file backup/export/restore. OS/display compromise and whole-directory rollback remain limitations; see the [threat model](docs/THREAT_MODEL.md), [vault format](docs/VAULT_FORMAT.md) and [evidence](docs/STATUS.md).

The original normative specifications remain [TASK.md](TASK.md), [ARCHITECTURE.md](ARCHITECTURE.md), [SECURITY.md](SECURITY.md), [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md), [TEST_PLAN.md](TEST_PLAN.md) and [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md). Their requirements are not an audit attestation.
