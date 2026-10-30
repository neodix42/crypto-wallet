# Independent NEAR standard-token fixtures

The wallet permits only `ft_transfer` (NEP-141) and self-only
`storage_deposit` with `registration_only:true` (NEP-145). These fixtures do not
add arbitrary function calls, `ft_transfer_call`, allowances, token burning,
storage withdrawal or third-party registration.

Reviewed primary standards on 2026-10-30:

- [NEP-141](https://github.com/near/NEPs/blob/91562ccd72f6d30f4e37e4b53f7f193cc808b5d8/neps/nep-0141.md): canonical
  positive u128 decimal-string amounts, sender/recipient registration and exactly
  one attached yoctoNEAR for `ft_transfer`.
- [NEP-145](https://github.com/near/NEPs/blob/91562ccd72f6d30f4e37e4b53f7f193cc808b5d8/neps/nep-0145.md):
  `storage_balance_bounds`, `storage_balance_of`, self-registration and exact
  attached native NEAR. The wallet supports positive registration minima up to
  one NEAR; larger/zero or malformed minima are rejected rather than guessed.
- [NEP-148](https://github.com/near/NEPs/blob/91562ccd72f6d30f4e37e4b53f7f193cc808b5d8/neps/nep-0148.md): required
  `ft-1.0.0` metadata, explicit decimals and symbol. Wallet display limits are
  0–77 decimals, a 1–24-character ASCII symbol and a bounded 128-byte name.
  Optional images/reference fields are bounded and never loaded or fetched.
- [Official Rust standard implementation](https://github.com/near/near-sdk-rs/blob/master/near-contract-standards/src/fungible_token/core.rs):
  ordinary transfer has no recipient contract callback.

`generate-tokens.mjs` uses integrity-locked official `@near-js/transactions`
and `@near-js/crypto` 2.5.1 (source commit
`345de7c326bc2da93dc0625ace8d123fc3042b17`) plus `near-seed-phrase` 0.2.1.
It takes no input and uses only the public `abandon … about` BIP39 fixture.
Eighteen vectors cover indices 0/1/1234, explicit 0/6/18 decimal policies,
standard transfer and self-registration. Go independently reconstructs the exact
Borsh receiver, FunctionCall fields, Ed25519 signature and transaction identity.

The real local integration deploys the unchanged official example FT WASM
specified in [contracts/README.md](contracts/README.md), twice, with six and
eighteen decimals. A pinned near-sandbox 2.13.4 node executes two self
registrations, four out/back transfers, and one additional transfer whose actual
successful RPC acknowledgment is deliberately discarded by an owned proxy.
Every call uses the real signer Engine, exact Go-authored confirmation and
current-password authentication, plus encrypted preparation/submission/receipt
journal writes. A fresh signer process state reopens the vault, restores both
approved token policies and the original UNKNOWN signed bytes, proves that the
whole account stays reserved, then reconciles only that identity and durably
releases the reservation. Cancellation and wrong-password controls sign nothing.
Reconciliation uses the original identity without a replacement or second
broadcast. The official JS SDK separately reconstructs every Go-signed byte and
signature, reads final receipts and checks exact final token and storage balances.
Contract storage is never patched to simulate any of these operations.

```sh
./scripts/with-toolchain.sh node tests/oracles/near/generate-tokens.mjs
./scripts/with-toolchain.sh go test -race ./internal/chains/near ./internal/model
./scripts/with-toolchain.sh node tests/oracles/near/run-tokens-local.mjs
WALLET_NEAR_TOKEN_COVERAGE=1 ./scripts/with-toolchain.sh node tests/oracles/near/run-tokens-local.mjs
./scripts/with-toolchain.sh go test ./internal/chains/near -run '^$' -fuzz '^FuzzNEP141Policy$' -fuzztime=10s -parallel=2
./scripts/with-toolchain.sh go test ./internal/chains/near -run '^$' -fuzz '^FuzzTokenPlan$' -fuzztime=10s -parallel=2
```

`reports/near-tokens-local.json` distinguishes PASS/FAIL and binds the producer,
Go adapter/model/test source hashes, contract checksum, number of actual
transactions and unchanged-source check. `reports/near-token-transactions.json`
contains only public synthetic account descriptors, signed public transactions
and expected final balances for the independent SDK oracle. It contains no
private keys/passwords and is not a runtime resource. Fresh test fixtures are
not accepted as real user recovery roots.

This proves the stated local execution scope, not public-chain consensus,
provider honesty, arbitrary issuer safety or a GUI workflow. Live funded
networks and real Bybit routes are not exercised; no mainnet funds are sent.
Runtime code pinning detects direct code changes but does not prove that contract
state, administrators or delegated execution are safe. Imported token policies
are encrypted local configuration; phrase recovery recovers keys, not the list
of tracked token contracts.
