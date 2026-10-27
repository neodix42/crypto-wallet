# Dependency and build inventory

Source pins are `go.mod`/`go.sum`, `desktop/package-lock.json`, the CI-only `.github/provenance/package-lock.json`, and separate oracle lockfiles under `tests/oracles`. The application ships no JavaScript blockchain SDK: those SDKs are independent test oracles only. Electron main/preload/UI have no production npm library dependency; Electron/Chromium is the packaged runtime. TypeScript/c8/jsdom/packager/fuses and Node/npm are build/test tools. Go signing dependencies are registered in the adapter evidence files with exact profiles and upstream provenance.

The build selects Go 1.27.3, Node 24.19.0 and npm 11.17.0; Electron is45.0.0 and TypeScript7.0.2. The Node bootstrap freezes the SHA256 of the official Linux x64 archive in source. Go module checksums, npm integrity fields, local node binary SHA256 checks and geth container digest are enforced. No floating upstream master branch is pulled for a signing primitive. A maintained version or a checksum is provenance, not independent cryptographic review.

`make package-desktop` generates `dist/sbom.cdx.json` (CycloneDX1.6), `dependency-licenses.json`, `go-modules.json`, the exact npm lock, source SHA256 manifest and build manifest. Go h1 values are directory-content module checksums, recorded as such, not misrepresented as archive file hashes. Npm entries carry their lock integrity. Build dependencies are identified separately. The shipped Go commands dynamically use libc/libseccomp2; the complete Electron runtime uses its host graphical/system libraries. GTK3/glib and display/input libraries are retained legacy-test dependencies, not Go dialog dependencies in shipped commands; legacy C/GTK coverage is reported separately. Electron's Chromium third-party notices remain in the full runtime package.

Recorded automated review on2026-10-23:

| Check | Result and limits |
|---|---|
| `go mod verify` | PASS; downloaded pinned module content verifies |
| `go vet ./...` | PASS; static checks are not an audit |
| `govulncheck@v1.9.0 -show verbose ./...` | PASS for reachable code:0 symbol vulnerabilities and0 imported-package vulnerabilities |
| Required-module advisory | GO-2026-5932 affects unmaintained `golang.org/x/crypto/openpgp`; x/crypto v0.58.0 is required for maintained Argon2/XChaCha packages, but openpgp is neither imported nor called; no fixed module version removes this advisory. Retained in reports/govulncheck.txt, not suppressed |
| npm audit | PASS:0 reported vulnerabilities in the desktop lockfile at execution time |
| Workflow lint | PASS with actionlint v1.7.12; no workflow was remotely run by this check |
| Independent dependency/cryptographic review | NOT_RUN |
| License obligations and redistributability review | NOT_RUN; generated inventory lists discovered license files, not legal clearance |

SDK/node versions and local acceptance details are in [EVM_CORE.md](EVM_CORE.md), [NEAR_APTOS.md](NEAR_APTOS.md) and [DCR_TON.md](DCR_TON.md). Test-only transitive dependencies are not silently shipped. A future upgrade must regenerate locks, rerun frozen independent vectors, adversarial/race/mutation/fuzz/node tests and build/package verification before changing compatibility claims. Unknown contracts and auth schemes fail closed.

There is no automatic updater, remote diagnostics upload or bundled crash reporter activation. Local artifacts remain unsigned development candidates. The complete desktop/component release workflow now authenticates `RELEASE-SHA256SUMS` with GitHub OIDC/Sigstore build provenance and verifies the exact expected publishing repository, release workflow, branch, immutable numeric repository ID, source commit and hosted runner before publication. This provides publisher workflow authentication without a stored signing key; it is not an OS executable signature or a production-readiness claim. Bare hashes remain unauthenticated without the verified bundle. See the concrete [download verification procedure](BUILD.md#authenticate-a-downloaded-release-before-running-it).

Release authentication uses official GitHub CLI 2.103.2 or later. The optional developer bootstrap pins the official Linux amd64 tarball SHA256 `62ccf747115ddc61237efffdde66a3c8f70ba249433dc64f70801089b59c59c8`; the CLI stays in ignored `.tools/`, outside wallet runtime artifacts. The exact action pins were resolved from official GitHub release tags on 2026-10-24 and their action definitions reviewed for inputs, permissions and transitive action pinning:

| Action | Release | Commit |
|---|---|---|
| `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `actions/setup-node` | v7.1.0 | `949feb2413d6458794dcd2491c4babbbce0c15c1` |
| `actions/upload-artifact` | v7.0.2 | `cf430e030ddbb5b0abf93d22962f4752f3646cd9` |
| `actions/download-artifact` | v8.0.2 | `9000827ccba6bdab643e8b6fd33ac0654aef8333` |

Provenance signing now uses the CI-only `.github/provenance/package-lock.json`, with `@actions/attest` pinned to 3.2.0 (the toolkit version used by the previous pinned upstream action). It uses the documented `skipWrite: true` path and GitHub's private certificate/timestamp services, attaching the bundle to the release instead of persisting it in the unsupported attestations API. No publisher key or repository write token is passed to this signer. The normal GitHub token remains scoped to publication and verifier access.

`@sigstore/core` is explicitly pinned to 4.0.1 to avoid [GHSA-jfc7-64v2-mr8c](https://github.com/advisories/GHSA-jfc7-64v2-mr8c) in the toolkit's older transitive core. The reviewed upstream changelog identifies Node support removals in majors 3/4; the signing library's imported APIs remain available under pinned Node 24.19.0. The real DSSE bundle builder is tested with ASCII and non-ASCII types and an independent ephemeral signature rejection check. `npm audit` reports zero findings for the new lockfile. Installation uses `npm ci --ignore-scripts`; no signing dependencies enter wallet runtime artifacts. This is local dependency evidence, not an independent cryptographic review.

Checkout does not persist credentials. The build job uses Go/npm caches; the separate publisher uses a pinned Node runtime only for signing, with no wallet compilation or functional test suite. Signing and verification fail closed.

The 21 local release-authentication tests pass, including actual certificate/signature/transparency verification of frozen public fixtures from official `cli/cli` v2.103.2 commit `4f2cfe92e3499f8581bf39db77e0a1431fa7b55c`. Changed signature/artifact, wrong repository/ref/commit and an ephemeral wrong transparency verification key fail. The retained fixture trust root is **test-only**, not an alternative production root. Actual LocalWallet OIDC signing/remote workflow/publication remain **NOT_RUN**. The workflow publishes a development prerelease independently of CI tests and cannot certify missing production gates. Official sources: [GitHub CLI verification](https://cli.github.com/manual/gh_attestation_verify), [GitHub attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations), [official signing toolkit](https://github.com/actions/toolkit/tree/main/packages/attest).
