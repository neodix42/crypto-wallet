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
