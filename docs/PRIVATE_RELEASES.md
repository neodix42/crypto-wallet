# Private repository release signing

Private repositories support GitHub Releases. The [reported publishing failure](https://github.com/neodix42/crypto-wallet/actions/runs/40531855450/job/122288096257) occurred after the build, artifact transfer and inventory check passed: GitHub rejected **attestation persistence** for a user-owned private repository. The release-creation step had not run. Repository visibility does not need to change.

GitHub's [official attestation toolkit](https://github.com/actions/toolkit/tree/main/packages/attest#storage) documents `skipWrite: true` for private non-Enterprise repositories. The workflow now uses that supported mode. It generates the same kind of signed SLSA provenance for `RELEASE-SHA256SUMS` through GitHub's private Sigstore CA/timestamp service, saves `release-provenance.sigstore.json` in the run’s `wallet-release-verification-<version>` Actions artifact (90-day retention, subject to earlier cleanup) and verifies it before `gh release create`. It never calls the unsupported attestation-storage API, publishes to public Rekor or falls back to unsigned output.

The existing verifier is retained: exact manifest digest, complete payload inventory, cryptographic signature and timestamp, GitHub OIDC issuer, expected repository/workflow/ref/full commit, immutable repository ID and hosted-runner policy. Downloaders continue using `scripts/release_assets.py verify` with the separately downloaded Actions bundle; no persisted attestation record is required. GitHub CLI supplies maintained trust roots, or the consumer supplies independently obtained roots. Frozen test roots never become production trust anchors.

The publisher uses pinned Node 24.19.0 and a separate integrity-locked official toolkit dependency graph, installed without lifecycle scripts. Its core DSSE dependency is pinned to a patched compatible release; audit reports no findings. Only `contents: write` and `id-token: write` remain; `attestations: write` is removed. The toolkit receives no repository write token. Build/test separation, version incrementing, complete archives and wallet security boundaries are unchanged.

| Status | Evidence |
| --- | --- |
| PASS | 13 Node signing regressions: exact digest/options, mandatory private service and storage skip, workflow/identity guards, unsafe file/output rejection, changed manifest/identity, signing failures and value-free CLI errors. The real toolkit's skipped storage branch passes; its storage-enabled control fails. The real DSSE bundle builder preserves UTF-8 type binding. |
| PASS | 21 Python release-authentication tests, including actual GitHub CLI verification of frozen private-Sigstore RFC3161 timestamp evidence without a stored record, plus wrong publisher/root and timestamp/artifact tampering rejection. Original public signature/transparency and policy tests also pass. |
| PASS | 43 build-tool, 28 release-evidence and 20 CI-helper regressions; the publisher still requires signing and independent verification before publication. |
| PASS | Pinned actionlint accepts the workflows; the signing dependency audit has zero findings. No functional test suite runs in the publishing workflow. |
| NOT_RUN | Live OIDC issuance, detached signing and publication for this changed LocalWallet workflow. Local tests do not impersonate its publisher or fabricate that result. Push the change to execute it on GitHub; rerunning the previous commit retains the previous action. |
| NOT_RUN | Independent review and original production acceptance gates. A development release is not production certification. |

Source-bound local results are recorded in `reports/private-provenance.json`, `reports/release-auth.json` and the corresponding local test/audit logs. No repository setting, visibility, user wallet, real secret, funds or remote release was changed during this fix.
