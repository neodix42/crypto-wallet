# Build, runtime and cache dependencies

The default application is now the [one-file Wails Go wallet](PORTABLE_GO.md). Ubuntu 24.04/26.04 amd64 runtime prerequisites are `ca-certificates libgtk-3-0t64 libwebkit2gtk-4.1-0`; there is no Electron, Chromium or sandbox helper. The Go roles deliberately apply no syscall sandbox under the latest owner instruction. Encrypted keys remain unchanged; current application startup atomically migrates the legacy storage root to `cryptowallet`. See [storage paths and upgrade safeguards](STORAGE_DIRECTORY.md).

```sh
sudo apt update
sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev golang-go python3 ca-certificates
make                     # bin/cryptowallet
make install             # ./cryptowallet, no sudo
./cryptowallet
make package-go          # alias: make package-portable
make test-portable       # actual ELF/version/licenses/dependency/archive inspection
```

Go 1.27.3 is pinned, bootstrapped from Go 1.21+. CGO needs a C compiler, pkg-config and GTK3/WebKitGTK 4.1 development headers. Node 24.19.0/npm 11.17.0 are pinned for React/TypeScript compilation; the frontend assets are embedded into the Go executable. See [WAILS_UI.md](WAILS_UI.md) for a project-local SDK alternative. The first build downloads locked modules/toolchain when missing. `.tools/` is ignored developer/test cache: do not commit it. Neither compiled nor downloaded wallets require it, Go, Node, npm, Python or a compiler at runtime.

| Path | Contents |
| --- | --- |
| `bin/cryptowallet` | One Wails Go executable: UI plus coordinator/signer child roles |
| `bin/portable-build-manifest.json` | Internal version, source and binary hash record |
| `dist/cryptowallet-<version>-linux-amd64.tar.gz` | Exactly `cryptowallet`, `install-user.sh`, `cryptowallet.png` and `cryptowallet.svg` at archive root |
| `dist/portable-build-verification.json` | Local packaging check results; not a release asset |
| `dist/portable-THIRD-PARTY-NOTICES.{txt,json}` | Build-time redistribution inventory, embedded as executable notices |
| `./cryptowallet` | Repository-local installed executable |
| `.cryptowallet-install.json` | Managed local upgrade marker; not required at runtime |

`make install` stages the verified executable and atomically replaces only a managed entry. Existing managed Electron launcher links can be replaced; prior `wallet-runtime/` generations and all profile data remain untouched. Unrelated files/symlinks or unsafe installation paths are rejected. Downloaded releases run directly: extract, authenticate, then run `./cryptowallet`. Optionally run `./install-user.sh --desktop` to install a per-user menu entry and desktop icon; this optional step needs Python 3 and preserves wallet data. See [desktop installation](../README.md#optional-application-menu-and-desktop-installation). `--version`, `--runtime-info` and `--licenses` provide non-secret inspection.

Legacy components are retained through `make build-components` / `make package-components`. Explicit `make build-desktop`, `make package-desktop`, `make install-desktop`, `make test-packaged-desktop` and `make test-packaged-send-desktop` use the earlier Electron layout and keep its sandbox requirements. No legacy runtime is built by default or uploaded in the Wails release. Existing sandbox/Chromium diagnostics documentation describes that retained layout only; it is irrelevant to native startup.

The downloaded binary is dynamically linked against OS GTK/WebKitGTK/libsoup/glib/libc; it is portable as one application file, not a static OS distribution. The new UI and coordinator are trusted with transient password/phrase display/input. The separate signer remains a distinct role with protected memory, but has no OS-enforced syscall/network confinement in native mode. See [native trust boundary and evidence](PORTABLE_GO.md).

## Authenticate a downloaded release before running it

The only uploaded GitHub Release asset is `cryptowallet-<version>-linux-amd64.tar.gz`. Its tag, release name and app version are the same automatically incremented bare number. It contains one runtime executable `cryptowallet` plus optional `install-user.sh`, `cryptowallet.png` and `cryptowallet.svg`, directly at archive root. Existing Electron releases retain their former layout; choose a release explicitly identified as the one-file Wails Go build.

The read-only build job compiles/packages once, verifies ELF architecture/version, embedded notices, dynamic dependencies and the exact four-file executable/installer/icon archive, then hands off a fixed archive plus verification metadata. Publishing downloads that exact artifact ID and rejects digest mismatches, changed bytes or wrong source/version. It installs no wallet compiler and executes no downloaded wallet. Tests run independently in `ci.yml`; release notes mark them NOT_RUN in the build workflow and link the exact source commit's checks. Historical evidence does not certify this candidate. See [RELEASE_PIPELINE.md](RELEASE_PIPELINE.md).

`RELEASE-SHA256SUMS` covers exactly the portable archive, `release.json` and `RELEASE-NOTES.md`. The publisher signs it using GitHub OIDC/Sigstore and verifies the signature, timestamp, repository ID, workflow, source branch and commit before publication. Required license texts are embedded in the executable and printed by `--licenses`; no separate launcher, inventory or evidence asset is published. The signed manifest, bundle and two metadata files are retained as **`wallet-release-verification-<version>`** in the same Actions run, for **90 days**, subject to earlier repository cleanup. Save this artifact for future/offline verification. Expired or deleted records must not be treated as a successful signature check.

Private repositories use the official `@actions/attest` toolkit's `skipWrite: true`, `sigstore: github` mode. No unsupported attestation-storage request, public Rekor upload, long-lived signing key or unsigned fallback is used. Only publishing has job-scoped `id-token: write` and `contents: write`. Local `make install` needs no publishing identity. See [PRIVATE_RELEASES.md](PRIVATE_RELEASES.md) and the official [detached signing option](https://github.com/actions/toolkit/tree/main/packages/attest#storage).

Use GitHub CLI **2.103.2 or later** from a separately trusted installation. The repository's optional developer helper `python3 scripts/bootstrap-gh.py` downloads the pinned official Linux amd64 CLI into ignored `.tools/`; neither downloaded wallets nor running wallets need it. Choose the expected publisher repository, version, protected source branch, immutable numeric repository ID and full source commit from an independently trusted project/release reference. Do not infer the trusted publisher from the downloaded `release.json`, bundle or a link supplied by an unknown mirror.

After the native workflow publishes, download its `cryptowallet-<version>-linux-amd64.tar.gz` portable archive and four detached verification records from the Actions run linked in that release's notes, into the same directory. In the Actions web UI, select `wallet-release-verification-<version>` under **Artifacts** and unpack that records ZIP. Alternatively, with an authenticated GitHub CLI:

```sh
release_version='X.Y.Z'
release_dir="$HOME/Downloads/cryptowallet-$release_version"
release_run_id='INDEPENDENTLY_CHECKED_ACTIONS_RUN_ID'
mkdir -p "$release_dir"
gh release download "$release_version" --repo neodix42/crypto-wallet \
  --pattern "cryptowallet-$release_version-linux-amd64.tar.gz" --dir "$release_dir"
gh run download "$release_run_id" --repo neodix42/crypto-wallet \
  --name "wallet-release-verification-$release_version" --dir "$release_dir"
```

The records are verification inputs, not runtime dependencies. From a separately trusted source checkout, verify them before extracting or executing the wallet:

```sh
# Replace these example values with independently checked project identity.
release_repo='OWNER/REPOSITORY'
release_repo_id='INDEPENDENTLY_CHECKED_NUMERIC_REPOSITORY_ID'
release_version='X.Y.Z'
release_ref='refs/heads/main' # refs/heads/master when that is the default branch
release_commit='FULL_40_CHARACTER_LOWERCASE_COMMIT_SHA'
python3 scripts/release_assets.py verify --directory /path/to/downloads \
  --repo "$release_repo" --repo-id "$release_repo_id" --version "$release_version" \
  --ref "$release_ref" --commit "$release_commit"
```

Check the permanent repository ID on a trusted connected machine with `gh api repos/OWNER/REPOSITORY --jq .id`; pin that ID separately from the downloaded files to reject ownership/name reuse. The command authenticates the manifest with `gh attestation verify`, exact certificate identity `https://github.com/OWNER/REPOSITORY/.github/workflows/release.yml@refs/heads/main`, GitHub's OIDC issuer, exact source ref/commit and certificate repository ID, SLSA v1 predicate and GitHub-hosted runner policy. It then validates the complete fixed inventory, all hashes and exact release identity/version. A missing CLI, old CLI, missing/invalid bundle, wrong publisher, branch, commit or version fails. It never executes or extracts a downloaded artifact. `prepare` and `check` subcommands only generate/check integrity; their success explicitly does **not** authenticate a publisher.

For an offline machine, obtain a Sigstore trusted-root file using `gh attestation trusted-root` on a separately trusted connected machine, transfer it along with the bundle, and add `--trusted-root /trusted/path/trusted_root.jsonl`. The root file must come from that independent trust step; never accept a replacement trust root from an untrusted release mirror. GitHub documents [offline verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline). Once verification passes, extract `cryptowallet-<version>-linux-amd64.tar.gz` and run `./cryptowallet` as an ordinary user. The adjacent installer and PNG/SVG icons are optional for desktop registration. The native UI, coordinator and signer roles are contained in the Go executable; no bundled runtime or Chrome SUID-helper setup is needed; the pinned Wails implementation also explicitly disables WebKitGTK's renderer sandbox, while keeping local-asset and ephemeral-storage restrictions.

This proves workflow provenance and downloaded-byte integrity, not source correctness, independent review, production readiness, OS code signing or rollback protection. The trusted repository/workflow, GitHub-hosted build environment, GitHub OIDC and Sigstore trust roots remain part of the supply-chain trust model. An older authentic release can still verify if the consumer explicitly requests its version/commit; there is no automatic updater.

`make test-release-auth` runs the strict inventory/policy tests and real offline cryptographic verification using frozen public upstream fixtures. Provision its pinned CLI first if it is absent. Tests reject missing/changed assets, identity mismatch, signature tampering and an independently generated ephemeral wrong verification key. Fixture keys never sign a CryptoWallet release. Historical local tests passed; GitHub OIDC issuance, complete remote workflow execution and publication for the current native Go migration remain **NOT_RUN** until that change runs on GitHub. The workflow pins all actions to exact commits and verifies its freshly issued bundle before `gh release create`.

## Tests

`make test-go` runs Go unit/corpus tests, including the shipped prompt protocol without a display dependency. `make test` also builds/types/tests the desktop. `make test-dialogs` runs the Electron main/preload/prompt/portfolio component tests. `make test-security`, `make test-race`, `make coverage`, `make test-fuzz`, and `make check-deps` enforce their respective boundaries. `make test-native` retains legacy GTK/X11 tests and needs GTK, Xvfb and XTest; it does not establish native application acceptance. Packaged recovery uses Xvfb/XTest, Python/Pillow and `tesseract-ocr`; its disposable driver reads only a synthetic generated phrase in memory and refuses saved credential/phrase screenshots. A missing prerequisite or skipped test is not a pass.

The dedicated headless recovery drill builds the production Go components and exercises their actual prompt protocol with a synthetic client:

```sh
make test-recovery
```

It creates disposable independent application profiles, records a generated phrase only in test memory through the production prompt transport, restores it under the chosen app password and compares the supported canonical wallets. This headless client tests the real Go protocol, encryption and recovery; it does not test Electron rendering or user input. No original vault, machine keyring or encrypted portable backup file is provided to the destination. The app password encrypts the destination; a different app password must preserve the same derived wallets. Any explicitly used BIP39 passphrase remains a separate recovery input. See [PHRASE_RECOVERY.md](PHRASE_RECOVERY.md) for exact executed cases and whether they passed; a same-host isolated-profile drill is not a second-physical-machine or clean-OS acceptance result.

`make test-packaged-desktop` rebuilds/installs and operates the retained legacy Electron desktop on a private display, including phrase recording with Go word verification, phrase restoration, receive QR/copy and public navigation. It does not create or verify encrypted backup files. Dependencies for those GUI tests are Xvfb/XTest, Pillow and Tesseract. Passing local drills does not establish real supported-OS/Wayland acceptance, OS lock/suspend or independent review.

`make test-packaged-send-desktop` additionally operates the actual Electron Send form, Go-authored approval and masked password reauthentication against a real private geth 1.17.9 chain. It verifies an independently queried successful receipt, exact 18-decimal recipient delta, source debit including the actual execution fee and delayed nonce state proving one payment. The only funds are public fixture allocations in fresh genesis. Geth and the app run as an ordinary UID inside a fresh network namespace containing only loopback, so no public testnet/mainnet endpoint is reachable. It preserves the production sandbox and uses no debug endpoint, fixture hook or mocked transaction/RPC implementation. Its test-only Engine API drives real EVM blocks; this does not certify beacon/public-chain consensus or the installed OS matrix.

The Send drill needs `python3-tk`, `iproute2`, `util-linux` (unprivileged user/network namespaces), Xvfb/XTest, Pillow and Tesseract. The tiny public Tk text field checks the test driver's punctuation entry before app credentials; it is separate test tooling, never wallet UI. It extracts the static geth binary only from the already cached, pinned official image provisioned by `scripts/provision-tests.py`; it never pulls an image or falls back to a public network. Its exact binary hash is frozen in `tests/e2e/local_geth.py`. Missing namespace/image/display prerequisites remain BLOCKED. Reports are `reports/packaged-send.json` and its caller's log; no credential/phrase screenshots or OCR files are written. The actual local Send drill passed against the final recorded ASAR/Go identities; supported-OS sends and public-network acceptance remain separate gates.

Protocol tests need explicit developer provisioning, not runtime user installation:

```sh
./scripts/with-toolchain.sh python3 scripts/provision-tests.py
make test-integration
```

The setup pins actual NEAR/Aptos node binaries, dcrd's Go module identity, TON TVM dependencies and a geth image digest. Test workloads run against private chains and use public synthetic material. `make test-e2e` exercises the retained legacy Electron desktop; its container probes do not certify clean real X11/Wayland or OS-session events. `make test-soak` runs the24-hour campaign. No script automatically spends mainnet funds.

`make test-portable` verifies the actual native ELF/version/notices/dependencies and archive. `make test-packaged` exercises the new Wails application separately; historical Electron results are never reassigned to it.

The exact version comes from VERSION or the validated WALLET_VERSION override. All Go executables support `--version` without opening a profile/window. `scripts/verify-version.py --go-only` checks Go manifests/runtime versions and any generated component archive; its default mode checks the complete optional desktop archive as well. Controlled tiny ELF/tar fixtures test numeric version bounds, hash failures and atomic installation without creating a Debian package or a remote release.

Production-readiness acceptance remains blocked by the actual evidence in [STATUS.md](STATUS.md), including external testnet, installed OS/display, long-running and independent review obligations. A cleaner first-run screen or a development release does not satisfy those gates.

`make test-release-evidence` tests rejection of stale or incomplete reports. The retained legacy `make verify-release` evidence model binds Electron packaged recovery to the current version, all three Go binary hashes and ASAR; artifact inspection additionally binds the Electron runtime and compares all 12 production files against current compiled/UI bytes. Frontend measurement hashes the full source/UI/test/package-pin set before and after execution, retains compiled-JS and raw-summary hashes, and rejects added/deleted files or concurrent edits. Go coverage requires its complete current source/module snapshot and both frozen raw profiles, reconstructs their exact coverage union and recalculates per-package floors; Wails UI acceptance is additional and cannot be inferred from legacy frontend coverage. Authentication/verifier regression reports are also bound to the scripts/workflow they tested. An old PASS is never relabeled for a rebuilt candidate: rerun the affected suite. All original Decred, public-testnet, installed-OS, full-soak and independent-review requirements remain mandatory.
