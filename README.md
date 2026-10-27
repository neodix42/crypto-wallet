# CryptoWallet

Portable Go multichain desktop wallet for Ubuntu amd64, with native GTK3 windows. The downloaded wallet is **one executable**, `cryptowallet`. It contains the UI, coordinator and signer roles and embedded third-party notices. There is no Electron, Chromium, browser runtime or sandbox helper in this build. Implemented domains remain Ethereum ETH and USDT, NEAR, HyperEVM and HyperCore HYPE, Aptos, canonical ZEN on Base and Horizen L3 (plus ETH gas), Decred and TON Wallet V5R1. The optional **Exchange** tab connects to Bybit for Deposit, Swap and Withdraw. [Repository implementation instructions](START_HERE.txt).

**Mainnet is the default profile.** Public funded testnets, the full supported X11/Wayland OS matrix, the 24-hour soak and independent security review remain production gates. The new native UI needs its own acceptance evidence; historical Electron results do not certify it. See [PORTABLE_GO.md](docs/PORTABLE_GO.md) and the [evidence checklist](docs/STATUS.md). Never fund public test fixtures.

## Download and run on Ubuntu 24.04 / 26.04

Use an **amd64/x86_64 Ubuntu desktop** with an X11 or Wayland session. Install runtime prerequisites on Ubuntu **24.04 LTS** or **26.04 LTS**:

```sh
sudo apt update
sudo apt install -y ca-certificates libgtk-3-0t64
```

GTK3's system dependencies are installed by apt. The executable needs no Go, Node.js, npm, Python, `.tools/`, compiler, bundled application directory or blockchain node. System GTK/libc libraries remain runtime prerequisites: portable means one application file, not a static copy of an operating system.

1. Open [CryptoWallet Releases](https://github.com/neodix42/crypto-wallet/releases/latest) and sign in with repository access if required.
2. Download **`cryptowallet-<version>-linux-amd64.tar.gz`**, the sole uploaded asset. GitHub's automatic source archives are for building. Existing releases retain their previous Electron payloads; use a release whose notes explicitly identify the **one-file native Go** build.
3. Authenticate the archive using [publisher verification](docs/BUILD.md#authenticate-a-downloaded-release-before-running-it). Detached verification records remain in that run's `wallet-release-verification-<version>` Actions artifact for 90 days, subject to earlier cleanup; retain them for later/offline verification.
4. Extract and run as your ordinary user:

```sh
# Set the exact version of a release identified as the one-file native Go build.
release_version='X.Y.Z'
release_dir="$HOME/Downloads/cryptowallet-$release_version"
mkdir -p "$release_dir"
gh release download "$release_version" --repo neodix42/crypto-wallet \
  --pattern "cryptowallet-$release_version-linux-amd64.tar.gz" --dir "$release_dir"
# Complete publisher verification before continuing.
cd "$release_dir"
tar -xzf "cryptowallet-$release_version-linux-amd64.tar.gz"
./cryptowallet
```

The archive contains only **`cryptowallet`**, directly at its root. `./cryptowallet --version` prints the app version; `./cryptowallet --licenses` prints embedded redistribution notices. There is no `cryptowallet-ui`, `chrome-sandbox` or helper installation. Do not change root ownership, setuid modes, AppArmor or user-namespace settings. The native Go build deliberately applies **no syscall sandbox**, as requested. It keeps coordinator and signer in separate child processes using private inherited pipes; memory protection, encrypted storage, exact approvals and password checks remain required. Without syscall confinement, the OS does not block signer network access; see [the trust boundary](docs/PORTABLE_GO.md).

## Build, install and find the binary

```sh
sudo apt update
sudo apt install -y build-essential golang-go python3 ca-certificates libgtk-3-0t64
make                     # same as make build / make build-go
bin/cryptowallet --version
make install             # atomically install ./cryptowallet in the repository root
./cryptowallet
# Or build, install and launch:
make dev
# Create the sole portable release payload:
make package-go          # alias: make package-portable
```

The compiler is pinned to Go **1.27.3**, bootstrapped from Go 1.21+; first-time toolchain/module downloads require internet access. CGO uses a C compiler and the system GTK3 ABI; GTK development headers, Node/npm and Electron are not needed. The built executable is **`bin/cryptowallet`**; the package is **`dist/cryptowallet-<version>-linux-amd64.tar.gz`**. Build records stay under `bin/` and `dist/` and are not required at runtime. Upstream dependency/Go notices are generated from checksum-verified sources and embedded before compilation; independent license review remains outstanding.

`make install` uses no sudo. It copies the verified binary to **`./cryptowallet`** and records ownership in `.cryptowallet-install.json` for safe later upgrades. A prior managed Electron launcher symlink can be replaced atomically; old `wallet-runtime/` generations are left intact. An unrelated regular file or symlink is rejected rather than overwritten. Profile data is never part of installation. Downloaded wallets need no installation script: extract and execute the one file.

`.tools/` is an ignored developer/test cache: **do not commit it**. Neither local nor downloaded executables need it. Legacy Electron code and its isolated regressions remain available through explicitly named `make build-desktop`, `make package-desktop`, `make install-desktop` and `make test-packaged-desktop`; they are not part of the default build/install/release. `make build-components` and `make package-components` retain the earlier backend-only development layout. No target creates a Debian package.

## Data and network connections

Wallet data remains in **`~/.config/localwallet/mainnet`**, or **`$XDG_CONFIG_HOME/localwallet/mainnet`**. The retained `localwallet` storage name ensures existing keys reopen without a migration or a replacement wallet. The default network is mainnet; `./cryptowallet --profile=testnet` opens the separate existing testnet profile. Replacing the executable does not copy, convert or delete a vault.

App passwords protect the encrypted local vault; BIP39 recovery phrases deterministically reconstruct keys, while any original optional BIP39 passphrase must also be retained. Local payment/exchange journals, account associations, RPC credentials and Bybit credentials/beneficiary metadata are separate recovery concerns. See [vault format](docs/VAULT_FORMAT.md) and [recovery instructions](docs/RECOVERY.md).

Public mainnet providers are configured for Ethereum, HyperEVM, HyperCore, Base, Horizen, NEAR, Aptos and TON. Decred requires an authenticated TLS `dcrd` connection through **Configure trusted RPC**. Unknown/failed balances never become verified zero. A NEAR implicit address needs funding to exist on-chain; a verified missing account shows zero with activation guidance. See [mainnet configuration](docs/MAINNET.md) and [balance troubleshooting](docs/BALANCE_CONNECTIVITY.md).

## Tests

Use public synthetic fixtures only. No test automatically spends mainnet funds. The default tests run offline after dependencies/toolchains have been installed; optional node integrations explicitly report a skip when their prerequisites are absent.

CI-only signing regressions require `./scripts/with-toolchain.sh npm --prefix .github/provenance ci --ignore-scripts` once before `make test` or `make test-release-auth`. Those dependencies are not shipped with or needed by the wallet.

The public-pixel OCR regression in `make test` requires `python3-pil` and `tesseract-ocr` (`sudo apt-get install python3-pil tesseract-ocr`). It uses a committed public tooltip fixture and does not open a wallet or display.

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
make coverage             # Go/native GTK and retained Electron floors; incomplete gates stay explicit
make check-deps           # checksums, vet, reachable Go vulnerabilities, npm audit
make test-stress          # bounded malformed input, concurrent scheduling, production KDF lifecycle
```

The fresh-install Go protocol recovery test is headless. Native and legacy graphical acceptance uses owned private test displays, never your real profile. The retained Electron drill requires isolated Xvfb/XTest, Pillow and Tesseract:

```sh
sudo apt-get install xvfb libxtst6 python3-pil tesseract-ocr util-linux mount dbus libseccomp2
make test-dialogs        # isolated Electron prompt, bridge and portfolio tests
make test-native         # retained GTK prompt regressions
make test-recovery       # headless real Go protocol recovery across fresh app copies
make test-recovery-portable # phrase restore across three independent single-binary app copies
make test-native-wallet   # actual native GUI creation, backup, restore, receive and restart
./scripts/with-go.sh python3 internal/nativeui/test-x11.py # private native GTK adapter tests
make test-portable       # ELF/version/licenses/dependencies and exact one-file archive
make test-packaged       # native packaged GUI acceptance (isolated synthetic profile)
make test-send-portable   # actual isolated-geth execution through the Go signer protocol
make test-packaged-desktop # explicitly retained Electron packaged regression
make test-packaged-send-desktop # retained Electron Send on an isolated actual geth chain
```

Native acceptance uses disposable profiles and private X11, user, mount and network namespaces. Its owned hosts fixture makes public RPC endpoints unreachable promptly; no RPC server or successful balance is mocked. It cannot reach mainnet and changes no host configuration. `make test-send-portable` requires the exact cached geth image pinned in [the local-node fixture](tests/e2e/local_geth.py); its receipt, balances, fees and nonce are checked independently. These test namespaces are separate from the application, which installs no sandbox.

Real local node/emulator integration also requires Docker and explicitly provisioned test dependencies:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
make test-e2e             # package recovery on private X11, then disposable Ubuntu portable-runtime probes
```

`make test-e2e` does not certify real Wayland/OS-lock acceptance; its container probes can remain blocked by container sandbox policy. No sandbox bypass is applied. `make test-soak` runs the actual 24-hour workload; a short smoke is reported separately. `make test-testnet` records the missing independently verified funded public testnet evidence and fails until that external gate is supplied. `make verify-release` deliberately fails on incomplete mandatory evidence. Reports go to `reports/`, with retained chain fixtures/results in `tests/oracles/` and readable interpretation in `docs/`. Passing unit tests do not imply that skipped integration scenarios ran.

`make verify-release` still targets the retained Electron candidate and cannot certify a native Go release. Native production-verifier migration and an actual native-GUI-approved local-node Send drill remain unfinished. The new headless Go Send test proves real signing/execution; the native GUI drill independently covers setup, Receive, the Send form and recovery. Neither is presented as the missing combined GUI payment acceptance. Development publication verifies the native archive and publisher identity separately.

The scheduled GitHub Actions soak workflow has been removed. Soak tests remain available manually; removing the workflow does not record a passing soak result.

The current owner excludes Decred implementation work and public testnet campaigns. Longer local fuzz and soak runs have explicit narrower commands:

```sh
make test-fuzz-long FUZZ_SECONDS=60  # discover current targets; Decred explicitly deferred
make test-soak-non-decred           # fixed test binary, 24 hours, local fake ledger only
# Short runner check, which never passes the 24-hour gate:
make test-soak-non-decred SOAK_SECONDS=1
```

The longer fuzz workflow uses 300 seconds per target on its schedule. Excluded targets remain NOT_RUN in the aggregate report. The non-Decred soak writes live progress to `reports/soak.json` and its pinned source/binary identity to `reports/nondecred-soak-run.json`; RUNNING is not PASS. Its EVM fake-ledger workload does not certify other-chain consensus, desktop or all-chain soak acceptance. `make test-packaged-send-desktop` needs Xvfb/XTest, Tesseract, user/network namespaces and the exact cached geth image documented by its test helper; it never reaches public networks or uses an existing wallet profile.

## First run

The native main window and credential dialogs use GTK3. Password fields are masked and recovery words are presented through the private prompt path. Historical Electron layout measurements do not certify the new interface.

A verified empty profile offers **Create wallet · 12 words**, **Create wallet · 24 words** and **Restore from recovery phrase**. Choose and repeat an app password of **12–4096 UTF-8 bytes** in the separate native wallet dialog. Go then generates one random English BIP39 recovery phrase and derives every supported network wallet. The separate native wallet dialog shows the phrase for you to write down offline. Confirm the requested words before receiving or sending. Cancelling leaves the same encrypted wallet pending; **Record and confirm recovery phrase** resumes it after password reauthentication in a native wallet dialog. There is no encrypted backup file to create or verify.

On a fresh installation, choose **Restore from recovery phrase**, specify the app password and enter the recorded phrase only in the separate native wallet dialog. Include the original optional BIP39 passphrase if one was used. The app password protects local storage and does not change your wallet addresses: either the same or a new app password restores the same canonical wallets. Phrase restoration reconstructs account index0; the Settings recovery-range control reconstructs additional indices. Named/rotated associations, Decred discovery history, RPC settings and the local payment journal are separate metadata and need explicit recovery/reconciliation.

Deterministic account groups appear as tabs across the top, starting with **Main account**. Each group's table contains nine mainnet network wallets and twelve asset identities, with a native-currency balance in every row (ETH on Base/Horizen and separate HYPE balances on HyperEVM/Core). Ethereum also supports six-decimal USDT at its pinned Ethereum contract; USDT payments require Ethereum ETH for gas. This adds no TON USDT jetton or other stablecoin network. **Refresh wallet state** reloads the encrypted account list and queues configured-provider balance observations for nonhidden wallets. Verified amounts display only their exact decimal value; observation state, block and provider details appear on hover. Failed or incomplete queries stay unknown/unavailable. Click a row to open its wallet tab. Keys follow each chain's pinned derivation profile; EVM networks can share an address while their balances remain separate. Receive addresses occupy their complete single-line width; narrow windows scroll the table instead of truncating them. The copy icon requests the full verified address from Go without opening the wallet.

Each wallet tab always shows its receive address and copy icon. Its selected asset appears in the title and tab name. Multi-asset wallets use asset buttons instead of a dropdown, preserving ETH/USDT and ETH/ZEN choices; new views start with their native asset. Wallet tabs show the amount and currency. Observation metadata appears only on the account table’s balance hover. **Send** reveals the otherwise hidden payment form, without a QR-image scan control. **Amount** uses whole asset units: entering `1` means 1 ETH or 1 TON, with exact decimal conversion in Go. **Receive** opens the Go-authored native wallet dialog with QR code, full address and an explicit **Copy address** button. These actions are enabled after recording and confirming the phrase; pending setup offers **Record and confirm recovery phrase** on the account and wallet pages. Middle-click a wallet tab to close it. Closing a tab or form preserves durable reservations. Go-validated approval and password checks use the existing real transaction pipeline.

The **Settings** tab sits immediately to the right of **Exchange** in the main tab bar. It opens in the main window and contains **Wallet**, **Exchange** and **Journal** tabs. Wallet settings contain phrase restoration for an empty profile, pending phrase-setup completion, **Back up recovery phrase**, password change, deterministic accounts and recovery ranges. To back up an existing phrase, confirm the request and enter the current app password, then write down the numbered words in the separate private dialog or use **Copy phrase**, and choose **Done**. Copying places the words on the system clipboard; other apps/history may retain them, so clear the clipboard after use. Every view requires authentication; an optional original BIP39 passphrase must be retained separately. **Settings → Journal** contains the **Local payment journal**, with previous/next controls over bounded pages of 128 operations. Select an account or wallet tab to return to it. Information and error feedback stays in a compact 52-pixel bottom status bar, including validation failures. Messages clear automatically after seven seconds with no Dismiss button; new messages start a fresh interval, while idle lock polling cannot revive expired notices. Long messages scroll inside the bar without moving the page. Tab navigation focuses its tab, without outlining the page heading. Password, recovery, payment approval and Receive QR still use separate private native dialogs. Phrase backup is a read-only private display; the ordinary portfolio receives no words and there is no file, print or QR export. Clipboard copying is available only through the authenticated backup dialog’s explicit Copy phrase button. Existing profiles keep their keys and encrypted data; older vaults require phrase recording once in a native wallet dialog under the new policy. See [the current interface and evidence](docs/WALLET_STATUS.md), [phrase recovery](docs/PHRASE_RECOVERY.md) and [recovery instructions](docs/RECOVERY.md).

[Authenticated phrase-backup behavior and evidence](docs/PHRASE_BACKUP.md) documents the Settings action and first-run import checks. The historical Electron sandbox prerequisite applies only to retained legacy tests; see [CI_DESKTOP.md](docs/CI_DESKTOP.md). Native evidence is separate in [PORTABLE_GO.md](docs/PORTABLE_GO.md).

[Historical Electron interface evidence](docs/ELECTRON_INTERFACE.md) applies only to the retained legacy runtime. [The native Go migration](docs/PORTABLE_GO.md) describes the new trust boundary and its separate acceptance evidence.

The implemented hardening and its exact test scopes are tracked in the [hardening evidence checklist](docs/PRODUCTION_HARDENING.md). Outstanding implementation, public-network, installed-OS, soak and independent-review work remains in the [production-readiness checklist](docs/PRODUCTION_READINESS.md) and [release evidence](docs/STATUS.md).

Accounts and the local payment journal now use bounded pages, including large encrypted archives. **Settings → Journal** exposes previous/next journal pages; wallet tabs expose **Next chain-history page**. Their continuations expire on lock or context/generation change. TON traverses exact hash-linked archival transactions and Core traverses inclusive timestamp pages within their documented budgets. Incomplete or saturated queries remain explicit. See [history limits and tests](docs/HISTORY_PAGINATION.md).

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

[The release workflow](.github/workflows/release.yml) runs on default-branch pushes or manual dispatch. The **build** job compiles/packages the one-file Go desktop once and checks its ELF architecture, version, embedded notices, dynamic dependencies and exact archive contents. The **release** job downloads that exact immutable artifact, verifies integrity and publisher provenance, and publishes one asset: **`cryptowallet-<version>-linux-amd64.tar.gz`**. Separate Go executables, launchers, JSON files, evidence archives and notices are not uploaded as release assets. The only archived file is `cryptowallet`; required redistribution notices are embedded and printable with `--licenses`. GTK3 is supplied by the operating system.

Publications are regular GitHub Releases without the **Pre-release** flag. Release name, tag, archive version and displayed app version always match. The workflow fetches all tags and increments above the greatest numeric version across both `X.Y.Z` and legacy `vX.Y.Z` tags, with [VERSION](VERSION) as a floor. A numeric version is consumed regardless of its old prefix; changing archive/UI implementation never resets numbering. New tags have no `v` prefix. Existing releases are left intact; failed builds create no release/tag and publication does not overwrite one.

[Wallet tests](.github/workflows/ci.yml) run independently and do not block development releases. Release notes mark tests **NOT_RUN in the build workflow** and link the exact commit's checks. No scheduled soak workflow is enabled; `make test-soak` remains available manually. See [the build/publication pipeline](docs/RELEASE_PIPELINE.md).

The workflow authenticates the archive and release identity with GitHub OIDC/Sigstore before publication. The detached verification records are retained in an Actions artifact linked from the release notes, rather than extra release assets. Private repositories use the official toolkit's `skipWrite: true` mode and private Sigstore service, avoiding unsupported attestation storage and public transparency uploads. Signing or verification failure blocks publication. Local builds remain unsigned and need no publishing credentials. See [verification instructions](docs/BUILD.md) and [private-repository signing](docs/PRIVATE_RELEASES.md). The production acceptance gates remain separate.

## Security and recovery

The native executable contains the UI, coordinator and signer roles, launched in separate processes with bounded inherited-pipe IPC. No Electron/Chromium process or syscall sandbox is used. The native UI and coordinator join the secret trust boundary because they handle entered credentials and displayed recovery words; generation in Go does not defend against a compromised trusted UI, operating system or display server. The unconfined signer must be trusted not to make unauthorized network calls. The previous seccomp-enforced network isolation property is deliberately absent in this owner-requested mode.

Go generates the root, verifies passwords/recovery words, validates exact plans and signs. Binary seeds, entropy and private/extended keys remain in the signer. Secret memory protections, Argon2id (256 MiB/3/4 default), XChaCha20-Poly1305 encrypted storage, password-authenticated approvals and recovery remain required. No secret logs, plaintext files, QR export or network requests are allowed. Clipboard export is limited to the explicitly authenticated backup **Copy phrase** action; clipboard history and other applications may retain it. See [native build security and evidence](docs/PORTABLE_GO.md), [vault format](docs/VAULT_FORMAT.md) and the [evidence checklist](docs/STATUS.md).

The original normative specifications remain [TASK.md](TASK.md), [ARCHITECTURE.md](ARCHITECTURE.md), [SECURITY.md](SECURITY.md), [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md), [TEST_PLAN.md](TEST_PLAN.md) and [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md). Their requirements are not an audit attestation.
