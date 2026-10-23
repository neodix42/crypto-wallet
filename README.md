# Codex task: local multichain desktop wallet

Specification date: **2026-10-23**. Target: **Ubuntu 24.04 and later supported LTS releases**, Go backend, Electron desktop UI. This package contains implementation instructions, not an implemented, audited, or tested wallet.

## Start

1. Extract this folder into the root of the Git repository in which Codex should build the application. The folder does not overwrite a root `AGENTS.md`, application code, or an existing build configuration.
2. Open that repository in Codex and paste the contents of [START_HERE.txt](START_HERE.txt).
3. Codex must read all the specifications, inspect the existing repository, create its implementation checklist, and implement the milestones with tests. Producing another plan alone is not completion.

## Required outcome

One local recovery root generates multiple accounts for Ethereum, NEAR, Hyperliquid, Aptos, Horizen/ZEN, Decred, and TON. The wallet can show balances and history, receive through locally generated address QR codes, send with authoritative confirmation and fee estimates, and restore from an encrypted backup. HyperCore and HyperEVM are separate balance domains. ZEN is supported on Base and the current Horizen L3, with ETH for gas.

There are no custodial servers, user accounts, cloud seed backups, or production secrets in JavaScript. Go owns key generation, derivation, encryption, signing, transaction verification, and network adapters. Electron supplies the ordinary application interface. A small Go native component supplies password/phrase entry and authoritative transaction/receive verification.

## Read before implementation

| File | Purpose |
| --- | --- |
| [TASK.md](TASK.md) | Product requirements, scope, recovery semantics, completion criteria |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Process boundaries, typed interfaces, transaction and persistence design |
| [SECURITY.md](SECURITY.md) | Vault encryption, secret handling, Electron hardening, threat model |
| [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md) | Exact per-chain derivation profiles, current networks, reference implementations |
| [TEST_PLAN.md](TEST_PLAN.md) | Coverage gates, negative tests, interoperability fixtures, stress and release verification |
| [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) | Ordered milestones, evidence and remaining release responsibilities |

All files form one task. Network facts and dependency versions must be rechecked against linked primary sources during implementation. The stated security invariants are requirements; they are not claims that any future code already meets them.

## Important product choice: strict no-reveal

This brief interprets “never reveal secret keys” conservatively: the app must not display or export the generated recovery phrase, binary seed, or any private key. The generated phrase remains inside the local encrypted vault. **For a newly generated wallet, recovery requires an encrypted backup and the password that encrypted that backup. Losing either can mean losing the funds.** There is no password reset or hidden recovery service.

Importing an already known BIP39 phrase is supported through a dedicated, masked, input-only Go dialog. This does not authorize exporting it afterwards. A conventional one-time paper-seed display would be a separate, explicit change to this product policy; Codex must not quietly introduce it.

The local app password encrypts storage; it does not determine blockchain keys. An optional BIP39 passphrase is a different input and changes the derived wallets. New wallets use an empty BIP39 passphrase by default. Restoring an imported passphrase wallet requires that exact passphrase or a verified encrypted backup containing it.

## Security and compatibility priorities

- Use reviewed cryptographic libraries and documented derivation profiles. Never invent a cryptographic primitive or seed conversion.
- Require independent address/signature vectors, clean-machine recovery, adversarial tests, crash recovery, and the coverage floors in the test plan.
- No “supported” label for a chain that only has an address generator or mock transfers.
- Tests and high coverage do not replace an independent security review. Development builds use testnets; production readiness needs the explicit evidence in the release gates.
- Encryption protects stored material. A software wallet cannot guarantee secrecy or correct approvals on a compromised operating system.

The archive checksum list identifies these instruction files. It is not a wallet security attestation.
