# Dependency and build inventory

Source pins are `go.mod`/`go.sum`, `desktop/package-lock.json`, and separate oracle lockfiles under `tests/oracles`. The application ships no JavaScript blockchain SDK: those SDKs are independent test oracles only. Electron main/preload/UI have no production npm library dependency; Electron/Chromium is the packaged runtime. TypeScript/c8/jsdom/packager/fuses and Node/npm are build/test tools. Go signing dependencies are registered in the adapter evidence files with exact profiles and upstream provenance.

The build selects Go 1.27.3, Node 24.19.0 and npm 11.17.0; Electron is45.0.0 and TypeScript7.0.2. The Node bootstrap freezes the SHA256 of the official Linux x64 archive in source. Go module checksums, npm integrity fields, local node binary SHA256 checks and geth container digest are enforced. No floating upstream master branch is pulled for a signing primitive. A maintained version or a checksum is provenance, not independent cryptographic review.

`make package-deb` generates `dist/sbom.cdx.json` (CycloneDX1.6), `dependency-licenses.json`, `go-modules.json`, the exact npm lock, source SHA256 manifest and build manifest. Go h1 values are directory-content module checksums, recorded as such, not misrepresented as archive file hashes. Npm entries carry their lock integrity. Build dependencies are identified separately. GTK3/libseccomp/glib and display/input libraries are dynamically linked system dependencies; C/GTK behavior remains in the security review boundary. Electron's Chromium third-party notices remain in the full runtime package.

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

There is no automatic updater, publisher signature, remote diagnostics upload or bundled crash reporter activation. Artifacts are unsigned development candidates with public SHA256 hashes; hashes do not authenticate a publisher. The release workflow uses scoped GitHub permissions and publishes a development prerelease after local gates; it cannot certify the missing production gates.
