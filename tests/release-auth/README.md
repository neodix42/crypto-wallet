# Independent public release-authentication fixtures

These are frozen **public upstream fixtures**, not wallet keys, wallet releases or LocalWallet publisher signatures. They are never executed or extracted. The Sigstore archive is verified as opaque bytes with its original SHA512 subject, certificate chain and signed transparency evidence. `provenance.json` records source commit and individual SHA256 hashes.

Source: official [cli/cli v2.103.2](https://github.com/cli/cli/tree/4f2cfe92e3499f8581bf39db77e0a1431fa7b55c/pkg/cmd/attestation/test/data). The signed package fixture is `sigstore/sigstore-js`2.1.0, signed by its `.github/workflows/release.yml` at source commit `26d16513386ffaa790b1c32f927544f1322e4194` on `refs/heads/main`. Its original certificate and log proof are independent expected data; tests do not manufacture acceptance.

`scripts/release-auth-test.py` uses the real pinned GitHub CLI and this frozen **test-only** trust root for offline positive verification, then rejects a changed artifact, changed DSSE signature, wrong publisher, branch and commit. A separately generated temporary P-256 public key replaces the transparency verification key only in a negative test. Its private key is destroyed with the temporary test directory; it never signs any release. Missing required verifier reports BLOCKED and exits nonzero.

The fixture trusted root exists solely to make the old upstream public signature test hermetic. Production verification uses GitHub CLI's maintained Sigstore roots, or a separately provisioned trusted root selected by the consumer; a downloaded wallet must not supply its own trust anchor.

Upstream `cli/cli` is MIT licensed; the package fixture includes its upstream Sigstore license. These files are test-only and are not shipped in the desktop or component runtime.

`private-timestamp/` adds the public `actions/attest-demo` wheel and RFC3161-timestamped bundle from the same pinned upstream CLI repository. The actual CLI verifies its private-Sigstore certificate/timestamp with frozen test-only GitHub roots, then rejects timestamp/artifact tampering, a foreign publisher and a root set without the required authorities. No stored attestation record or new signing identity is used. Its per-directory provenance records the original source and root acquisition; the parent manifest hashes every fixture.
