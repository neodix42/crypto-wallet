# Delivery milestones and release evidence

## Working rules

Read the entire task before building. Respect applicable repository instructions and preserve unrelated work. Inspect any existing application, tests and dependency policy. Record a concrete requirement-to-test checklist; implement the requested application rather than returning only this plan or scaffolding.

Work in small coherent changes. Independent public adapters and test-oracle generators can be developed in parallel after their interfaces and fixtures are agreed. Secret handling, profile changes, vault migrations and approval semantics need a consistent owner and independent review. Do not let integration pressure weaken those boundaries.

Record exact versions and source commits when a source in the compatibility document changes. Fix ordinary implementation problems autonomously. If a requirement conflicts with a hard platform constraint, preserve the safe behavior, identify the precise blocker, and complete unaffected work. Do not silently replace the required design with an easier insecure one.

## Milestone 0 — Repository, dependency and feasibility baseline

Inventory the repository and choose supported, mutually compatible Go/Electron/Node versions. Pin toolchains, SDKs, cryptography, native GUI bindings and packaging dependencies. Check official provenance, licenses and outstanding relevant vulnerabilities. Produce a dependency table with purpose and review status.

Reverify the official network/asset registry, particularly current Horizen contracts and HyperCore action schemas. Freeze derivation-profile definitions and public reference vectors before writing adapter logic. Decide and document Decred synchronization integration and the exact native input/signing isolation mechanism.

Build a minimal non-secret prototype of Electron plus Go core plus the native Go prompt on stock Ubuntu 24.04 and 26.04, X11 and Wayland. Verify sandboxing and AF_INET/AF_INET6 restrictions while local GUI facilities still work. Resolve packaging dependencies now; do not postpone the critical trust-boundary feasibility until after the UI is complete.

**Exit evidence:** architecture decision records, dependency pins, verified registry sources, executable sandbox/native-prompt proof, and an initial checklist. This milestone alone is not task completion.

## Milestone 1 — Vault, mnemonic profiles and recovery

Implement secure entropy generation, BIP39 validation and import, immutable profile registration, native input, the authenticated vault format and atomic persistence. Implement lock lifecycle, password change, encrypted portable backup and verification, and restore into a fresh profile. No production renderer may receive a secret even temporarily.

Implement derivation for every required family now, including DCR's exact child semantics and TON's StateInit-based address. Add public recovery descriptors and account-index persistence. An extra account must be reproducible without a new seed. Explicitly test nonempty imported BIP39 passphrases separately from storage passwords.

**Exit evidence:** independent vectors, no-reveal/canary checks, tamper/fault tests, 95% critical-package statement floors, explicit uncovered-code review, and clean-machine encrypted recovery producing matching addresses. Do not enable mainnet deposit workflows before the backup verification mechanism exists.

## Milestone 2 — Desktop read/receive workflow

Implement the sandboxed Electron shell and narrow typed IPC. Add wallet creation/import/unlock/lock state, network/account selection, public balance/history models and clear mainnet/testnet labels. Implement address copying and locally generated QR images, plus the Go-owned authoritative receive verification window.

Implement bounded account discovery and honest unknown/synchronizing/error states. Ensure no renderer navigation, remote resources, script injection, unexpected window creation or generic native methods. Create security tests for the packaged configuration as it evolves.

**Exit evidence:** end-to-end creation → verified encrypted backup → account selection → independently decoded receive QR → lock/restart → restore. The QR/verification window must match the exact account and network even under renderer substitution tests.

## Milestone 3 — Transaction foundation and EVM family

Implement integer amounts, immutable plans, native confirmation, approval/session binding, per-account reservations and the durable payment state machine before enabling arbitrary chain sends. Build hostile/fault-injectable RPC fixtures.

Implement Ethereum ETH, HyperEVM HYPE, Base ETH/ZEN and Horizen ETH/ZEN through shared EVM primitives and explicit network policies. Include current fee estimation, rollup charges where applicable, gas reserves, failed execution, replacement/reorg handling and token-contract identity checks.

**Exit evidence:** independent byte/signature vectors, local EVM integration, required testnet receipts/state queries where available, no-duplicate-payment fault tests, sandboxed desktop send/receive flows. HyperEVM success must not be reported as HyperCore completion.

## Milestone 4 — NEAR and Aptos

Implement native NEAR with implicit accounts, verified named associations, access-key authority, recent block hashes and access-key nonce tracking. Implement native APT with the fixed Legacy Ed25519 profile, exact BCS/signing, sequence reservations, simulation/gas bounds and rotation/unsupported-authority detection.

Make restoration and imported-account identity part of each adapter, not a later UI feature. Never hide a failed authority check behind a zero balance.

**Exit evidence:** the relevant independent oracle fixtures, local protocol integration, successful and failed execution-state tests, clean restoration, testnet acceptance and all chain-specific adversarial cases.

## Milestone 5 — TON and Decred

Implement TON with the pinned V5R1 code/parameters, user-friendly address rules, first funding/deployment, seqno/expiry and trace-based transfer confirmation. Do not use native TON mnemonic processing on the BIP39 root or change existing addresses when updating a default contract.

Implement DCR through reviewed official primitives and the selected genuine synchronization engine. Support ordinary receive/change tracking, spendability, transaction trees, coin selection, signature verification, fees and durable UTXO reservations. Ensure no hidden second secret keystore remains outside the vault boundary.

**Exit evidence:** TON contract emulator and testnet traces; DCR simnet and current testnet transactions; leading-zero derivation regression; UTXO/change/gap recovery; first-send TON deployment; both adapters integrated into backup/recovery and transaction-journal tests.

## Milestone 6 — HyperCore HYPE

Implement separate Core spot HYPE balance/history and ordinary peer transfer. Freeze the allowed action and exact signing schema against the official Python SDK. Validate the token ID, precision, environment/signature-domain separation and replay/nonce behavior. The wallet never exposes trading or agent-key management.

Establish fresh and existing recipient behavior, prerequisites and any fees through authoritative documentation and actual testnet cases. If a prerequisite is unresolved, the receive/send flow must state the limitation instead of claiming universal address acceptance.

**Exit evidence:** independent typed-signature fixtures, accepted transfers with independent history/balance verification, correct failure/timeout/replay handling and a complete desktop flow distinct from HyperEVM. An adapter stub or empty response parser does not satisfy this milestone.

## Milestone 7 — Full security, recovery and stress campaign

Run the entire requirement matrix against integrated code. Complete the coverage floors per critical package, mutation-critical tests, fuzz corpora, race tests and crash injection. Review every uncovered security/fund-preservation branch. Fix failures without removing checks or weakening expectations.

Run the recorded bounded stress profile and a 24-hour soak. Measure actual memory, concurrency and latency behavior. Repeat encrypted restoration into a fresh supported installation with multiple accounts on all families, metadata associations and unresolved payment journals. Test upgrades without address changes.

**Exit evidence:** reproducible reports and retained regression fixtures. Each result is tied to an exact commit/configuration. Record real observed numbers; never substitute proposed targets for measurements.

## Milestone 8 — Package and release candidate

Build the actual amd64 `.deb` and test the required Ubuntu/display-server matrix. Verify production fuses, sandbox behavior, native dialogs, debug restrictions and exclusion of test-only secret hooks. Provide installation, upgrade, uninstall-with-data-preservation and offline recovery instructions.

Produce the SBOM, dependency/license list, package hashes and release-signature instructions. If publisher-signing credentials are unavailable, clearly mark the package unsigned/development; local build completion is still useful. Do not publish or auto-update the user's machine without a separate request.

Submit the vault, IPC/GUI boundary, each signing adapter, build/update chain and recovery flow for independent security review. Address relevant critical/high findings. A review performed only by the implementation's author or an automated coverage report is not an external audit.

**Exit evidence:** the release checklist, package/install evidence, compatibility report, review disposition and explicit remaining limitations. Development builds may be complete while a production-release gate remains blocked; label that state accurately.

## Required developer commands

Implement documented commands equivalent to the following targets; exact build tooling may follow the existing repository. Each command must run real checks and propagate failures.

| Target | Required effect |
| --- | --- |
| `make dev` | Start development app with testnet-only profile and synthetic/local test data chosen explicitly |
| `make test` | Hermetic unit tests, public vectors and deterministic regression corpora |
| `make test-security` | Vault, IPC, approval, no-reveal, leak, parser and mutation-critical checks |
| `make test-race` | Go race checks across relevant stateful components |
| `make test-interop` | Independent fixture parity for every profile/adapter |
| `make test-integration` | Local nodes/emulators and durable transaction-state scenarios |
| `make test-e2e` | Desktop flows, including native component boundaries |
| `make test-testnet` | Separately configured, capped testnet integration; no implicit mainnet use |
| `make test-stress` | Bounded repeatable stress workload and measured report |
| `make test-soak` | Scheduled 24-hour scenario campaign |
| `make coverage` | Enforced per-package thresholds and readable coverage reports |
| `make package-deb` | Actual Ubuntu installer plus manifest/checksums |
| `make verify-release` | Aggregate evidence/fuse/package gate; fails on missing mandatory evidence |

Go checks normally include `go test`, `go test -race`, `go vet`, an appropriate static analyzer and `govulncheck`; frontend checks include strict TypeScript, linting, meaningful unit/IPC tests and packaged E2E checks. Record supported tool versions. Do not claim that a command was executed merely because a script exists.

## Final implementation report

Provide source commit, build/package paths, installation commands, exact supported OS matrix, per-chain capability/evidence table, coverage per critical package, mutation/fuzz/race results, stress environment/results, clean recovery evidence, dependency/security findings and review status.

Use explicit statuses: **PASS**, **FAIL**, **BLOCKED**, **NOT_RUN**, or **NOT_APPLICABLE with reason**. State why a blocked test matters, what unaffected work is complete, and the precise next action needed. Lack of a public faucet or external reviewer does not justify leaving the remaining local implementation unfinished, and does not justify calling the missing gate passed.
