# Independent tests, build and publishing

Owner policy: development releases build and publish independently of tests. Production-readiness requirements remain separate.

| Workflow/job | Work performed | Dependency |
| --- | --- | --- |
| `ci.yml` / checks | Current Go/React unit, security, mutation, race, interoperability, node integration, fuzz, dependency, fresh recovery and crash tests; actual Wails checks run in their separate job. Legacy Electron and coverage campaigns remain local-only | Push/pull request; superseded runs on the same branch/PR are cancelled |
| `release.yml` / build | Select auto-incremented version; compile the single Go executable; verify ELF/version, embedded notices, dynamic GTK/WebKitGTK dependencies and exact four-file executable/installer/icon archive | Default-branch push or manual dispatch; no test workflow dependency |
| `release.yml` / release | Download the exact build artifact ID; reject digest mismatch; check all payload hashes/version/source; authenticate and verify publisher provenance; publish one portable archive with matching bare numeric tag/name/version | Successful build job in the same workflow run; no compilation or test suite |

The build job has read-only repository permissions and uses the pinned Go module/build caches. Only the publishing job receives contents/OIDC write permissions. Its pinned Node runtime signs a detached bundle through GitHub's private Sigstore service; no attestation-storage permission, compiler or wallet rebuild is involved. See [PRIVATE_RELEASES.md](PRIVATE_RELEASES.md). The artifact transfer uses no recompression for already compressed wallet archives and does not download an artifact from a different test run or branch. Publishing preserves the existing immutable publisher/repository/workflow/branch/source policy. The latest owner-authorized native build deliberately applies no syscall sandbox; all other secret-handling requirements remain. See [PORTABLE_GO.md](PORTABLE_GO.md).

Release notes and `reports/release-build.json` record **NOT_RUN** for tests in this build workflow and link the exact source commit's independent checks. No old PASS is assigned to a rebuilt candidate. No evidence or metadata file is uploaded to the release. Reports retained under `docs/evidence` are historical. The four detached verification records are stored in the run’s `wallet-release-verification-<version>` Actions artifact for 90 days (subject to earlier cleanup); preserve them for later verification. Full production release verification continues to reject missing or stale mandatory evidence.

The default wallet is one portable Go executable with Wails/React windows rendered by system WebKitGTK 4.1, described in [PORTABLE_GO.md](PORTABLE_GO.md). Legacy Electron packages/tests remain explicitly named development tools and are not uploaded. Existing Electron acceptance evidence does not certify the native UI.

The sole uploaded asset is `cryptowallet-<version>-linux-amd64.tar.gz`, containing exactly `cryptowallet`, `install-user.sh`, `cryptowallet.png` and `cryptowallet.svg` at archive root. The installer and icons are optional for running the single Go executable; they register a per-user menu entry and desktop shortcut. The signed archive hash covers all four files, with exact bytes and modes verified before handoff. Required Go/dependency notices are embedded and printable with `--licenses`. The build fetches all tags and increments the greatest numeric version across both bare and legacy `v` tag histories, with VERSION as a minimum. New tag, release title and app version match exactly. Regular GitHub Releases are used; existing publications are not rewritten.

## Historical split-pipeline verification — 2026-10-27

The [reported release job](https://github.com/neodix42/crypto-wallet/actions/runs/40492654091/job/122150179830), source `39e70b48eee9349bfcafbe9a8c6174c4888b0638`, passed compilation and all preceding core/security/coverage/recovery gates. After approximately 45 minutes, a packaged UI test failed with `Public feedback timing observation started too late`. This was an automation timing failure while checking a seven-second status message, not a Go/TypeScript compilation failure.

The public OCR helper now crops the requested status rectangle **before** decoding it and translates observed coordinates back to the desktop origin. Previously every status poll decoded the entire window and filtered the results afterward, consuming the short observation interval on hosted runners. The exact timing, text, geometry, credential-window exclusion and fail-closed decoder checks remain in the independent test suite.

| Status | Verification |
| --- | --- |
| PASS | 43 build-tool, 20 CI AppArmor helper, 18 publisher-authentication/tamper, 28 release-evidence and three startup-diagnostic regressions. Workflow assertions require complete tests in CI and forbid their execution in the build/publication workflow. The actual release-note generator executes against an isolated fixture and must identify tests as NOT_RUN for the exact source. |
| PASS | Seven OCR regressions: real hosted tooltip pixels, actual cropped decoding, original desktop coordinates, empty regions, credential exclusion, timeouts and process failures. |
| PASS | Pinned actionlint accepts the three remaining workflows. Download action v8.0.2 is pinned to its official commit and rejects artifact digest mismatches. |
| PASS | 39 actual packaged recovery/navigation checks on an owned private display, including the original seven-second feedback checks. `reports/packaged-recovery.json` retains the installed candidate's hashes. This drill ran before the fresh compilation below; its result is not reassigned to the newly compiled Go binary hashes. |
| PASS | `make package-desktop package-go` compiled/packaged once in **23.28 seconds** with the existing local caches. Both actual archives pass version verification, production fuse/payload inspection and complete inventory checks. Fresh hosted downloads/cache misses have not been timed. |
| PASS | Executed the workflow's actual preparation shell block, transferred all 25 required real assets through a ZIP into an isolated publishing directory with non-executable file modes, and checked their hashes. A modified executable, foreign source commit and wrong version were each rejected. No wallet executable runs in the publishing phase. `reports/release-split-handoff.json`; inventory verification is not live publisher authentication. |
| NOT_RUN | New workflows on GitHub-hosted runners, live LocalWallet publisher attestation and publication. No push, remote dispatch or release was performed locally. |
| NOT_RUN | Full supported OS/display matrix, public funded campaigns, complete soak and independent review; no production-readiness claim. |

Build/test measurements are local evidence only. A successful release build no longer implies successful tests; inspect the independent checks for its exact commit.
