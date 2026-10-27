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

This path uses `scripts/with-go.sh` and Go 1.27.3. It does not invoke Node, npm, Electron, a package manager or blockchain test nodes. It requires a bootstrap Go1.21+ installation, Python3 for version/manifests, a C compiler/CGO and the installed libc/libseccomp2 runtime libraries. Go can download the pinned toolchain and locked modules on the first build. The shipped Go signer uses the bounded prompt protocol and does not import the retained GTK UI implementation or require GTK for its own windows.

These executables are the existing application's real backend components, not a complete standalone human interface. `wallet-core` accepts length-framed private stdin/stdout IPC. `wallet-signer` generates authoritative prompts and validates scoped replies; it opens no Go/GTK windows. A complete Electron runtime supplies all user-facing credential, recovery, receive and approval dialogs. Keep core, signer and `manifest.json` together. `wallet-launcher` is the Go launcher for the separate complete Electron desktop layout; copying that executable alone does not create the main wallet UI. No component accepts passwords/phrases through command arguments or environment variables.

The Go runtime dependencies are Linux amd64, libc and libseccomp2 where required by the isolation components. The complete desktop also needs the normal system libraries and X11/Wayland session required by its bundled Electron runtime; this does not make it independent of host graphical libraries. Node, NEAR/Aptos/DCR validators, Xvfb and `.tools/` are developer/test dependencies only. Retained legacy GTK tests need GTK libraries; shipped Go commands do not use those windows.

## Existing desktop without an installer

Every current application window uses Electron, including password/import fields, recovery recording/challenges, receive QR and payment approval. Go still generates entropy, checks passwords/recorded words, validates plans and signs. Dedicated temporary prompt windows use a separate origin/preload/nonpersistent session; the ordinary portfolio never receives secret fields. Electron main/prompt and the transient coordinator transport are trusted with entered/displayed secrets. See [THREAT_MODEL.md](THREAT_MODEL.md). Debian packaging has been removed. Build/install the complete portable layout explicitly:

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

Pins for this optional desktop build are Node24.19.0, npm11.17.0, Electron45.0.0 and TypeScript7.0.2. The build wrapper bootstraps the SHA256-verified Node distribution in `.tools/` if necessary. Electron, Go SDKs and npm dependencies have locked versions/checksums. No local build/install command produces a `.deb` file, invokes `dpkg-deb`, installs an AppArmor profile, or changes system-wide sandbox settings. The GitHub-hosted CI workflow alone explicitly loads a temporary named userns profile for its desktop test processes and removes it afterward; see [CI_DESKTOP.md](CI_DESKTOP.md). A host that blocks Chromium's user-namespace sandbox remains unsupported until properly configured/tested; the launcher never uses `--no-sandbox`.

The launcher starts the network coordinator on anonymous inherited pipes, then installs process-wide IP restrictions before execing Electron. The secret signer independently applies the same restriction. Running the app as root is rejected. Mainnet is the default; `--profile=mainnet` is explicit and `--profile=testnet` opens the separate developer profile without changing mainnet data. Profile selection is consumed by the Go launcher and passed to the coordinator/signer, not chosen by renderer input. Explicit `--ozone-platform=x11` and `--ozone-platform=wayland` select the Electron display backend. Start the complete desktop with its Go launcher, rather than bypassing the process boundary by invoking Electron directly.

The desktop uses CPU rendering, selected before Electron is ready, to avoid unnecessary GPU-driver initialization for its forms and account lists. This does not disable Chromium's sandbox or change system GPU-library permissions. A `MESA-LOADER` driver warning is separate from a rejected password, phrase or recovery-confirmation operation; those failures must appear in the wallet dialog or public status message.

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

All generated outputs/cache directories stay out of Git. The source manifest hashes the actual working tree; a base commit alone does not identify modified source. Go paths/build IDs are stripped; bit-for-bit archive reproducibility is not yet established. Bare SHA256 manifests do not authenticate a publisher against an attacker who can replace every local file. Locally built artifacts remain unsigned. The release workflow additionally authenticates the complete checksum manifest as described below.

## Authenticate a downloaded release before running it

The release workflow builds the complete portable Electron/Go wallet **and** separate Go components with the same auto-incremented `vX.Y.Z` tag, runtime versions and manifests. Build and publishing use separate jobs: the first compiles/packages once with Go/npm caches and no publishing credentials; the second downloads its exact artifact ID from the same workflow run, rejects an artifact digest mismatch, checks the complete manifest and authenticates it before publication. The publishing job installs no wallet compiler and does not execute downloaded wallet binaries. Its pinned Node runtime and separate locked npm dependencies are used only for detached provenance signing; it never rebuilds the wallet. `RELEASE-SHA256SUMS` names every required desktop/component archive, raw Go executable, build/dependency/source inventory, third-party notice text/provenance, release metadata, notes and evidence archive. An omitted complete desktop or any missing, duplicate, unsafe or changed payload fails verification.

Tests run independently in `ci.yml`; the development prerelease does not depend on their result. No node/emulator provisioning, Xvfb, UI/recovery, race, fuzz or coverage campaign runs in `release.yml`. Versions and packaged fuses/payload checks still run during build. Release notes and `reports/release-build.json` explicitly say tests were NOT_RUN in this build workflow and link the source commit's checks. `test-evidence.tar.gz` includes this build status and artifact inspection plus documentation. Old `docs/evidence` reports remain historical and do not certify the current candidate. See [RELEASE_PIPELINE.md](RELEASE_PIPELINE.md). This owner-authorized development publishing policy leaves production acceptance requirements intact.

GitHub OIDC/Sigstore provenance cryptographically authenticates this manifest to the exact publishing repository, `.github/workflows/release.yml`, protected default branch, immutable numeric repository ID and source commit. The workflow has job-scoped `id-token: write` and `contents: write` permissions; no long-lived publisher private key, personal token or wallet key is used. Attestation or self-verification failure stops publication. Local `make install` neither signs nor needs these permissions. User-owned private repositories can publish releases without GitHub attestation storage. The CI-only signer uses the official `@actions/attest` library with `skipWrite: true` and `sigstore: github`: it obtains the signed provenance bundle from GitHub's private certificate/timestamp services and attaches it directly to the release. It makes no attestation-storage request and sends no private metadata to the public Rekor log. The existing `gh attestation verify --bundle` policy still verifies the complete signature, timestamp, repository/workflow/ref/commit and repository ID before publication. A signing or verification failure blocks publication; there is no unsigned fallback. See [PRIVATE_RELEASES.md](PRIVATE_RELEASES.md) and the official toolkit's [detached signing option](https://github.com/actions/toolkit/tree/main/packages/attest#storage). See GitHub's [artifact attestation documentation](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations) and [verification policy options](https://cli.github.com/manual/gh_attestation_verify).

Use GitHub CLI **2.103.2 or later** from a separately trusted installation. The repository's optional developer helper `python3 scripts/bootstrap-gh.py` downloads the pinned official Linux amd64 CLI into ignored `.tools/`; neither downloaded wallets nor running wallets need it. Choose the expected publisher repository, version, protected source branch, immutable numeric repository ID and full source commit from an independently trusted project/release reference. Do not infer the trusted publisher from the downloaded `release.json`, bundle or a link supplied by an unknown mirror.

Download every release payload plus `RELEASE-SHA256SUMS` and `release-provenance.sigstore.json` into one directory. From a separately trusted source checkout, verify them before extracting or executing anything:

```sh
# Replace these example values with independently checked project identity.
release_repo='OWNER/REPOSITORY'
release_repo_id='INDEPENDENTLY_CHECKED_NUMERIC_REPOSITORY_ID'
release_version='0.1.1'
release_ref='refs/heads/main' # refs/heads/master when that is the default branch
release_commit='FULL_40_CHARACTER_LOWERCASE_COMMIT_SHA'
python3 scripts/release_assets.py verify --directory /path/to/downloads \
  --repo "$release_repo" --repo-id "$release_repo_id" --version "$release_version" \
  --ref "$release_ref" --commit "$release_commit"
```

Check the permanent repository ID on a trusted connected machine with `gh api repos/OWNER/REPOSITORY --jq .id`; pin that ID separately from the downloaded files to reject ownership/name reuse. The command authenticates the manifest with `gh attestation verify`, exact certificate identity `https://github.com/OWNER/REPOSITORY/.github/workflows/release.yml@refs/heads/main`, GitHub's OIDC issuer, exact source ref/commit and certificate repository ID, SLSA v1 predicate and GitHub-hosted runner policy. It then validates the complete fixed inventory, all hashes and exact release identity/version. A missing CLI, old CLI, missing/invalid bundle, wrong publisher, branch, commit or version fails. It never executes or extracts a downloaded artifact. `prepare` and `check` subcommands only generate/check integrity; their success explicitly does **not** authenticate a publisher.

For an offline machine, obtain a Sigstore trusted-root file using `gh attestation trusted-root` on a separately trusted connected machine, transfer it along with the bundle, and add `--trusted-root /trusted/path/trusted_root.jsonl`. The root file must come from that independent trust step; never accept a replacement trust root from an untrusted release mirror. GitHub documents [offline verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline). Once verification passes, extract `localwallet-<version>-linux-amd64.tar.gz`, keep its files together and start `LocalWallet-linux-x64/wallet-launcher` as an ordinary user. `localwallet-go-...` contains only backend components.

This proves workflow provenance and downloaded-byte integrity, not source correctness, independent review, production readiness, OS code signing or rollback protection. The trusted repository/workflow, GitHub-hosted build environment, GitHub OIDC and Sigstore trust roots remain part of the supply-chain trust model. An older authentic release can still verify if the consumer explicitly requests its version/commit; there is no automatic updater.

`make test-release-auth` runs the strict inventory/policy tests and real offline cryptographic verification using frozen public upstream fixtures. Provision its pinned CLI first if it is absent. Tests reject missing/changed assets, identity mismatch, signature tampering and an independently generated ephemeral wrong verification key. Fixture keys never sign a LocalWallet release. These local tests passed; actual GitHub OIDC issuance, complete remote workflow execution and publication remain **NOT_RUN** until an authorized release run. The workflow pins all actions to exact commits and verifies its freshly issued bundle before `gh release create`.

## Tests

`make test-go` runs Go unit/corpus tests, including the shipped prompt protocol without a display dependency. `make test` also builds/types/tests the desktop. `make test-dialogs` runs the Electron main/preload/prompt/portfolio component tests. `make test-security`, `make test-race`, `make coverage`, `make test-fuzz`, and `make check-deps` enforce their respective boundaries. `make test-native` retains legacy GTK/X11 tests and needs GTK, Xvfb and XTest; it does not establish evidence for the new shipped Electron interface. Packaged recovery uses Xvfb/XTest, Python/Pillow and `tesseract-ocr`; its disposable driver reads only a synthetic generated phrase in memory and refuses saved credential/phrase screenshots. A missing prerequisite or skipped test is not a pass.

The dedicated headless recovery drill builds the production Go components and exercises their actual prompt protocol with a synthetic client:

```sh
make test-recovery
```

It creates disposable independent application profiles, records a generated phrase only in test memory through the production prompt transport, restores it under the chosen app password and compares the supported canonical wallets. This headless client tests the real Go protocol, encryption and recovery; it does not test Electron rendering or user input. No original vault, machine keyring or encrypted portable backup file is provided to the destination. The app password encrypts the destination; a different app password must preserve the same derived wallets. Any explicitly used BIP39 passphrase remains a separate recovery input. See [PHRASE_RECOVERY.md](PHRASE_RECOVERY.md) for exact executed cases and whether they passed; a same-host isolated-profile drill is not a second-physical-machine or clean-OS acceptance result.

`make test-packaged` rebuilds/installs and operates the real portable desktop on a private display, including Electron phrase recording with Go word verification, phrase restoration, receive QR/copy and public navigation. It does not create or verify encrypted backup files. Dependencies for those GUI tests are Xvfb/XTest, Pillow and Tesseract. Passing local drills does not establish real supported-OS/Wayland acceptance, OS lock/suspend or independent review.

`make test-packaged-send` additionally operates the actual Electron Send form, Go-authored approval and masked password reauthentication against a real private geth 1.17.9 chain. It verifies an independently queried successful receipt, exact 18-decimal recipient delta, source debit including the actual execution fee and delayed nonce state proving one payment. The only funds are public fixture allocations in fresh genesis. Geth and the app run as an ordinary UID inside a fresh network namespace containing only loopback, so no public testnet/mainnet endpoint is reachable. It preserves the production sandbox and uses no debug endpoint, fixture hook or mocked transaction/RPC implementation. Its test-only Engine API drives real EVM blocks; this does not certify beacon/public-chain consensus or the installed OS matrix.

The Send drill needs `python3-tk`, `iproute2`, `util-linux` (unprivileged user/network namespaces), Xvfb/XTest, Pillow and Tesseract. The tiny public Tk text field checks the test driver's punctuation entry before app credentials; it is separate test tooling, never wallet UI. It extracts the static geth binary only from the already cached, pinned official image provisioned by `scripts/provision-tests.py`; it never pulls an image or falls back to a public network. Its exact binary hash is frozen in `tests/e2e/local_geth.py`. Missing namespace/image/display prerequisites remain BLOCKED. Reports are `reports/packaged-send.json` and its caller's log; no credential/phrase screenshots or OCR files are written. The actual local Send drill passed against the final recorded ASAR/Go identities; supported-OS sends and public-network acceptance remain separate gates.

Protocol tests need explicit developer provisioning, not runtime user installation:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
```

The setup pins actual NEAR/Aptos node binaries, dcrd's Go module identity, TON TVM dependencies and a geth image digest. Test workloads run against private chains and use public synthetic material. `make test-e2e` exercises the optional portable desktop; its container probes do not certify clean real X11/Wayland or OS-session events. `make test-soak` runs the24-hour campaign. No script automatically spends mainnet funds.

The exact version comes from VERSION or the validated WALLET_VERSION override. All Go executables support `--version` without opening a profile/window. `scripts/verify-version.py --go-only` checks Go manifests/runtime versions and any generated component archive; its default mode checks the complete optional desktop archive as well. Controlled tiny ELF/tar fixtures test numeric version bounds, hash failures and atomic installation without creating a Debian package or a remote release.

Production release remains blocked by the actual evidence in [STATUS.md](STATUS.md), including external testnet, installed OS/display, long-running and independent review obligations. A cleaner first-run screen or a development prerelease does not satisfy those gates.

`make test-release-evidence` tests rejection of stale or incomplete reports. `make verify-release` now binds packaged recovery to the current version, all three Go binary hashes and ASAR; artifact inspection additionally binds the Electron runtime and compares all 12 production files against current compiled/UI bytes. Frontend measurement hashes the full source/UI/test/package-pin set before and after execution, retains compiled-JS and raw-summary hashes, and rejects added/deleted files or concurrent edits. Go coverage requires its complete current source/module snapshot and both frozen raw profiles, reconstructs their exact coverage union and recalculates per-package floors. Authentication/verifier regression reports are also bound to the scripts/workflow they tested. An old PASS is never relabeled for a rebuilt candidate: rerun the affected suite. All original Decred, public-testnet, installed-OS, full-soak and independent-review requirements remain mandatory.
