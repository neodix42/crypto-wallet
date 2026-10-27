# Portable release layout and executable rename

The publishing workflow uploads exactly **one** release asset: `localwallet-<version>-linux-amd64.tar.gz`. Publications are regular GitHub Releases, without the Pre-release or Draft flags. Tag, release name and app version always match the automatically selected version. Both bare numeric tags and legacy `v...` tags consume the same version history, with `VERSION` as a floor. Existing `v0.1.1` and `0.1.1` select **0.1.2** next, then **0.1.3**. Existing remote releases/tags are not rewritten by these local changes.

The desktop entry point is **cryptowallet**, both inside `LocalWallet-linux-x64/` and after local `make install`. It is the existing Go isolation launcher under its new public name. It starts the internal **cryptowallet-ui** executable. The portable archive retains Chromium resources, Go components and required redistribution notices. Standalone launchers, Go archives, evidence, JSON and notices are not uploaded separately to Releases. Manual `make package-go` remains available for developers.

The archive and two metadata records are covered by the exact signed `RELEASE-SHA256SUMS` inventory. Publisher signature, timestamp, immutable repository ID, workflow, source branch and commit verification still precede publication. The four detached verification records are stored in the run's **wallet-release-verification-<version>** Actions artifact for 90 days, subject to earlier cleanup. Preserve those records for later/offline verification; their absence is not a passing signature check. See [download and verification instructions](BUILD.md#authenticate-a-downloaded-release-before-running-it).

`make install` preserves existing profile locations, keys, prior managed runtime generations and the old `./wallet` entry. Use `./cryptowallet` for the new installation. Tests use disposable isolated profiles; no real user secret or funds are used.

The following measurements describe the earlier executable/layout change, before the version-continuity correction below. They are not new runs against the corrected workflow.

| Status | Historical executable/layout evidence |
| --- | --- |
| PASS | 45 build/install/version regressions, including numeric version ordering, legacy-tag migration, version floors/exhaustion, unsafe file/link rejection, atomic updates, legacy entry/data preservation, and executing the actual publication shell with an argument-capturing transport: exactly one portable archive, tag/title `0.1.1`, no Pre-release/Draft flag. The transport test makes no GitHub request. |
| PASS | 21 publisher-authentication tests and 13 Node signing regressions. Real frozen upstream public/private Sigstore fixtures verify cryptographically with the actual pinned GitHub CLI; wrong identity/root, changed archive/manifest, signatures and timestamps fail. These fixtures do not sign this wallet. |
| PASS | 28 release-evidence regressions; candidate binding now hashes `cryptowallet` and `cryptowallet-ui`. |
| PASS | 13 license-collection regressions; the actual packaged runtime retains redistribution texts for 40 components. Independent legal review remains NOT_RUN. |
| PASS | Go launcher unit and race suites, including the renamed internal executable path. The direct bootstrap worker is a helper exercised by its parent test, not standalone OS acceptance. |
| PASS | Actual `make package-desktop`, portable archive verification, all Go runtime versions, packaged Electron metadata version **0.1.1**, production fuses and all 12 shipped UI payloads. Installed `./cryptowallet --version` returns **0.1.1**. |
| PASS | 39 uninstrumented packaged X11 recovery/navigation checks through the renamed installed entry point: password/phrase setup, recorded-word confirmation, fresh-profile restoration, independent public-address comparison, receive QR/copy, wallet/account tabs, Settings and status feedback. Current candidate hashes and unchanged driver hashes are in `reports/packaged-recovery.json`. |
| PASS | Actual workflow note generator and real portable ZIP transfer into a non-executable publishing directory; exact archive/metadata inventory and tag match; changing an archive byte is rejected. Inventory checks alone do not authenticate a publisher. |
| PASS | Pinned actionlint accepts all three workflows. |
| NOT_RUN | New hosted build/OIDC signing/publication and consumer download of this changed release. No push, remote workflow dispatch, old-release cleanup or publication was performed locally. |
| NOT_RUN | Full independent CI campaign after this packaging change, installed Ubuntu/Wayland/lifecycle matrix, funded network acceptance, soak, independent security/legal review. Regular GitHub release classification does not establish production readiness. |

The source-bound local summary is `reports/portable-release.json`, with per-command exit codes, raw log hashes, current candidate identity and its input snapshot. Reproduction commands are `python3 scripts/build-tools-test.py`, `make test-release-auth test-release-evidence test-license-notices`, `./scripts/with-toolchain.sh go test -race ./cmd/wallet-launcher`, `make package-desktop`, `python3 scripts/install.py`, and `./scripts/with-toolchain.sh python3 scripts/packaged-recovery.py`. The packaged drill uses its own private display and disposable data directories. See [the full remaining gates](STATUS.md).

## Version continuity correction — 2026-10-27

The earlier migration incorrectly ignored `v...` tags, allowing `v0.1.1` and `0.1.1` to reuse the same numeric version. Read-only GitHub inspection confirmed both tags/releases. The selector now counts both formats as one history, and checkout explicitly fetches all tags. New names/tags remain bare numeric versions. With the observed tags the next version is **0.1.2**, then **0.1.3** after that version is published; the tag, title, archive and embedded build version stay synchronized.

| Status | Evidence for this correction |
| --- | --- |
| PASS | 47 build/install/version tests, including a real temporary Git repository starting with both reported tags and three consecutive CLI selections (`0.1.2`, `0.1.3`, `0.1.4`), mixed-format numeric ordering, invalid tags, version floors/exhaustion and read-only selection. |
| PASS | The actual publication shell runs with an argument-capturing transport for consecutive selected versions; tag, title and sole archive filename match. The production build-version helper receives the same version. No GitHub publication occurs in this test. |
| PASS | 21 publisher-authentication, 13 Node signing and 28 release-evidence regressions. All three workflows pass pinned actionlint; root specification checksums and whitespace checks pass. |
| NOT_RUN | Hosted workflow execution and publication of this correction. No remote tag/release was edited or created. |
| NOT_RUN | Wallet rebuild, packaged GUI, full security/network/stress/OS campaigns for this version-selection-only correction. The earlier layout measurements above remain historical; production gates are unchanged. |

Current reports are `reports/build-tools.json`, `reports/release-auth.json`, `reports/version-increment.json` and `reports/version-increment-*.log`. Reproduce with `python3 scripts/build-tools-test.py`, `make test-release-auth test-release-evidence`, and the pinned actionlint commands from `make check-deps`.
