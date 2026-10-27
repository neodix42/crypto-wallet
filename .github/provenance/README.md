# Detached release provenance

This is CI signing tooling, not wallet runtime code. `attest.mjs` calls GitHub's official `@actions/attest` 3.2.0 with `skipWrite: true`, `sigstore: github` and an empty repository token. The package and every transitive dependency are pinned by the lockfile. The private certificate and signed timestamp remain verifiable with `gh attestation verify --bundle`; the unsupported attestations storage API is never called. No public Rekor upload or unsigned fallback is used.

The signer accepts only the fixed release manifest/output in the matching GitHub-hosted release workflow, validates the release identity, refuses unsafe files/existing output, hashes the exact manifest and rejects changes during signing. It obtains short-lived OIDC signing authority in hosted CI; it has no stored publisher key. Errors print no upstream credential-bearing diagnostics. Publication independently verifies the bundle and complete inventory with the existing strict Python verifier.

The explicit `@sigstore/core` 4.0.1 override fixes the DSSE payload-type encoding advisory in older toolkit dependencies. Upstream's major changes drop older Node support, compatible with pinned Node 24.19.0. Tests exercise the actual bundle builder's UTF-8 type binding and the actual toolkit's storage-skip branch, with a storage-enabled negative control. The remaining signing/transport stubs are explicitly not publisher signatures. Python tests separately verify real frozen upstream public and private-Sigstore signatures/timestamps with the real CLI.

```sh
./scripts/with-toolchain.sh npm --prefix .github/provenance ci --ignore-scripts
./scripts/with-toolchain.sh npm --prefix .github/provenance test
make test-release-auth
```

No local test requests a real OIDC identity or signs a LocalWallet release. Live signing/publication requires executing the changed workflow on GitHub. See [the evidence checklist](../../docs/PRIVATE_RELEASES.md).
