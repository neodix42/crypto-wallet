# Test-only official staking-pool WASM

`staking_pool.wasm` is copied unchanged from
`near/core-contracts@1b0436c96379c63aee7d2429c6d5d24019031ab8`,
`staking-pool/res/staking_pool.wasm` (package 0.4.1).

- Size: 255556 bytes.
- SHA-256: `454cf4a54ff49a1b144e2591eb4ec833bf3ab5fa4792128beb6d9d0cc8d76e58`.
- Source: https://github.com/near/core-contracts/tree/1b0436c96379c63aee7d2429c6d5d24019031ab8/staking-pool.
- License: MIT OR Apache-2.0; both upstream texts are preserved here.

The local acceptance harness verifies the checksum before deployment. It pauses
validator proposals through the upstream owner's method, without editing state;
accounting, epoch locks, signed calls, receipts and withdrawals execute on the
real pinned sandbox. Validator election and consensus rewards are not certified.
This fixture is not linked or packaged into the wallet executable.

# Test-only official NEP-141 fungible-token WASM

`fungible_token.wasm` is copied unchanged from
`near-examples/FT@6a96a120ad8537d292fd946b6bda6b139904c2c3`,
`res/fungible_token.wasm`. The pinned commit updated that compiled artifact on
2024-03-24. Its corresponding source is `ft/src/lib.rs` and `ft/Cargo.toml`
(example version1.0.0, near-sdk/near-contract-standards4.0.0).

- Size:234861 bytes.
- SHA-256:`024570509fa82cf25ee2448ab63a8343b3c7ced16b68109d27448beaf4326bbb`.
- [Pinned WASM](https://github.com/near-examples/FT/blob/6a96a120ad8537d292fd946b6bda6b139904c2c3/res/fungible_token.wasm).
- [Pinned source](https://github.com/near-examples/FT/blob/6a96a120ad8537d292fd946b6bda6b139904c2c3/ft/src/lib.rs).
- MIT OR Apache-2.0; upstream `LICENSE` and `LICENSE-APACHE` are preserved as
  `FT-LICENSE-MIT` and `FT-LICENSE-APACHE` beside the fixture.

The fixture is checked before deployment to a disposable owned loopback node;
its original `new`, NEP-141, NEP-145 and NEP-148 methods execute unchanged.
Two deployments provide independent six/eighteen decimal state. We do not claim
this historical upstream precompiled artifact was rebuilt bit-for-bit, or that
its issuer/code is suitable for real funds. No contract state is patched and no
fixture bytecode is packaged into the wallet executable. See
[the token oracle](../TOKENS.md) for execution and interoperability scope.
