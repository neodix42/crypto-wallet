# Independent NEAR staking transaction fixtures

`generate-staking.mjs` uses the already integrity-locked official
`@near-js/transactions` and `@near-js/crypto` 2.5.1 implementations, source commit
`345de7c326bc2da93dc0625ace8d123fc3042b17`, and `near-seed-phrase` 0.2.1. It
accepts no input and uses only the public BIP39 `abandon … about` vector, with an
empty BIP39 passphrase and indices 0, 1 and 1234. Never substitute user material.

The twelve vectors independently serialize and sign each allowed method:
`deposit_and_stake`, `unstake`, `unstake_all` and `withdraw_all`. They bind the
exact contract account, canonical JSON arguments, 100 Tgas, nonce, block hash,
128-bit attached deposit and Ed25519 signature. `unstake` and both all-methods
attach zero NEAR. The all-method vector's amount is only the approval's observed
effect; `{}` instructs the contract to act on its entire current balance.

```sh
./scripts/with-toolchain.sh npm --prefix tests/oracles/near ci --ignore-scripts
./scripts/with-toolchain.sh node tests/oracles/near/generate-staking.mjs
./scripts/with-go.sh go test -race ./internal/chains/near
./scripts/with-go.sh go test ./internal/chains/near -run '^$' \
  -fuzz '^FuzzStakingCallPolicy$' -fuzztime=10s -parallel=2
```

Expected SHA-256 of `staking-fixtures.json`:

```text
9008cbb6babf99a060b206f7ff5893f0b7aaa50f33e19ad8aa5a424c18960767
```

Protocol references reviewed on 2026-10-28:

- [Official Action discriminants and FunctionCall fields](https://github.com/near/nearcore/blob/master/core/primitives/src/action/mod.rs): FunctionCall is variant 2; the signed fields are method name, argument bytes, u64 gas and u128 deposit. Existing Transfer remains variant 3.
- [Official staking-pool source](https://github.com/near/core-contracts/blob/1b0436c96379c63aee7d2429c6d5d24019031ab8/staking-pool/src/lib.rs): four supported methods, `get_account` and four-epoch withdrawal gate. Its staked balance includes compounded rewards; the view cannot isolate lifetime rewards.
- [Official NEAR CLI staking examples](https://github.com/near/near-cli-rs/blob/main/docs/GUIDE.en.md): direct testnet pool contracts under `pool.f863973.m0`.
- [Validator onboarding](https://near-nodes.io/validator/validator-guide): mainnet `pool.near` and legacy `poolv1.near` factory namespaces. Namespace membership is not a security audit or validator-performance guarantee.

The tests preserve native-transfer fixture parity, deny arbitrary methods or
parameters, exercise strict final-block state decoding and gas/storage reserve
arithmetic, and reconcile original signed transaction identities after ambiguous
RPC replies. Provider fixtures are unit tests; they do not establish real network
acceptance. Actual sandbox and public-testnet results belong to their separate
integration reports. No mainnet transaction is sent by these tools.
