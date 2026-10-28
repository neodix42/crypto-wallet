# Delivery milestones and release evidence

> Current owner update (2026-10-28): [START_HERE.txt](START_HERE.txt) supersedes earlier Horizen-network coverage and Connect-only password requirements. Active coverage is eight mainnet networks/ten pinned assets; standard Ethereum/Base ERC-20 tracking and bounded read-only Base discovery are supported. Light/18px defaults and Settings Appearance apply to public/private Wails views. All unrelated secret-handling, approval, recovery and test gates remain mandatory.

## Working rules

Read the entire task before building. Respect applicable repository instructions and preserve unrelated work. Inspect any existing application, tests and dependency policy. Record a concrete requirement-to-test checklist; implement the requested application rather than returning only this plan or scaffolding.

Work in small coherent changes. Independent public adapters and test-oracle generators can be developed in parallel after their interfaces and fixtures are agreed. Secret handling, profile changes, vault migrations and approval semantics need a consistent owner and independent review. Do not let integration pressure weaken those boundaries.

Record exact versions and source commits when a source in the compatibility document changes. Fix ordinary implementation problems autonomously. If a requirement conflicts with a hard platform constraint, preserve the safe behavior, identify the precise blocker, and complete unaffected work. Do not silently replace the required design with an easier insecure one.

Owner interface update (2026-10-24): Electron provides every application window. The dedicated prompt and its secret-bearing transport replace the former Go/GTK display boundary; retain every vault, derivation, chain, lock and signing requirement. Go remains the source of entropy, authoritative prompt details and authorization.

Owner Exchange update (2026-10-24): implement the optional Bybit Deposit/Swap/Withdraw extension and pinned Ethereum USDT described in TASK.md §6.1. This supersedes only the original exclusion of that custodial spot-trading flow; all existing networks, recovery requirements and production gates remain. Nine mainnet network wallets expose twelve assets. The new extension has separate secret/PII, exact-route, authorization, fee and durable remote-operation evidence; no real credentials or automatic mainnet spending may be introduced to obtain it.

## Milestone 0 — Repository, dependency and feasibility baseline

Inventory the repository and choose supported, mutually compatible Go/Electron/Node versions. Pin toolchains, SDKs, cryptography, Electron and packaging dependencies. Check official provenance, licenses and outstanding relevant vulnerabilities. Produce a dependency table with purpose and review status.

Reverify the official network/asset registry, particularly current Horizen contracts and HyperCore action schemas. Freeze derivation-profile definitions and public reference vectors before writing adapter logic. Decide and document Decred synchronization integration and the exact Electron prompt/signing isolation mechanism and expanded secret trust boundary.

Build a minimal non-secret prototype of Electron portfolio plus its isolated Electron prompt and Go core/signer on stock Ubuntu 24.04 and 26.04, X11 and Wayland. Verify sandboxing and AF_INET/AF_INET6 restrictions while local GUI facilities still work. Resolve packaging dependencies now; do not postpone the critical trust-boundary feasibility until after the UI is complete.

**Exit evidence:** architecture decision records, dependency pins, verified registry sources, executable sandbox/isolated-prompt proof, and an initial checklist. This milestone alone is not task completion.

## Milestone 1 — Vault, mnemonic profiles and recovery

Implement secure entropy generation, BIP39 validation and import, immutable profile registration, isolated Electron credential input, the authenticated vault format and atomic persistence. Implement lock lifecycle, password change and phrase restoration into a fresh profile. The ordinary portfolio renderer/public bridge never receives secrets. The owner-authorized dedicated Electron prompt, its preload/main and transient coordinator transport may carry passwords/phrases/passphrases; document their trust and runtime-copy risks. During setup, Go generates the phrase after password entry, persists the root encrypted before display, then verifies separately entered recorded words. No root entropy, binary seed or private key leaves Go. Restore a fresh installation using the phrase with an independent local storage password.

Implement derivation for every required family now, including DCR's exact child semantics and TON's StateInit-based address. Add public recovery descriptors and account-index persistence. An extra account must be reproducible without a new seed. Explicitly test nonempty imported BIP39 passphrases separately from storage passwords.

**Exit evidence:** independent vectors, isolated prompt containment/canary checks, tamper/fault tests, 95% critical-package statement floors, explicit uncovered-code review, and fresh-install phrase recovery producing matching canonical addresses. Do not enable receiving/preparation/signing before recorded-word confirmation succeeds in Go.

## Milestone 2 — Desktop read/receive workflow

Implement the sandboxed Electron shell and narrow typed IPC. Add wallet creation/import/unlock/lock state, network/account selection, public balance/history models and clear mainnet/testnet labels. Implement address copying and locally generated QR images, plus the isolated Electron receive dialog containing Go-authored network/address/QR data.

Implement bounded account discovery and honest unknown/synchronizing/error states. Ensure no renderer navigation, remote resources, script injection, unexpected window creation or generic privileged methods. Create security tests for the packaged configuration as it evolves.

**Exit evidence:** end-to-end creation → Electron phrase recording/Go word confirmation → account selection → independently decoded receive QR → lock/restart → restore. The QR/verification window must match the exact account and network even under renderer substitution tests.

## Milestone 3 — Transaction foundation and EVM family

Implement integer amounts, immutable plans, isolated Electron confirmation, approval/session binding, per-account reservations and the durable payment state machine before enabling arbitrary chain sends. Build hostile/fault-injectable RPC fixtures.

Implement Ethereum ETH, HyperEVM HYPE, Base ETH/ZEN and Horizen ETH/ZEN through shared EVM primitives and explicit network policies. Include current fee estimation, rollup charges where applicable, gas reserves, failed execution, replacement/reorg handling and token-contract identity checks.

**Exit evidence:** independent byte/signature vectors, local EVM integration, required testnet receipts/state queries where available, no-duplicate-payment fault tests, sandboxed desktop send/receive flows. HyperEVM success must not be reported as HyperCore completion.

## Milestone 4 — NEAR and Aptos

Implement native NEAR with implicit accounts, verified named associations, access-key authority, recent block hashes and access-key nonce tracking. Implement native APT with the fixed Legacy Ed25519 profile, exact BCS/signing, sequence reservations, simulation/gas bounds and rotation/unsupported-authority detection.

Make restoration and imported-account identity part of each adapter, not a later UI feature. Never hide a failed authority check behind a zero balance.

**Exit evidence:** the relevant independent oracle fixtures, local protocol integration, successful and failed execution-state tests, clean restoration, testnet acceptance and all chain-specific adversarial cases.

## Milestone 5 — TON and Decred

Implement TON with the pinned V5R1 code/parameters, user-friendly address rules, first funding/deployment, seqno/expiry and trace-based transfer confirmation. Do not use native TON mnemonic processing on the BIP39 root or change existing addresses when updating a default contract.

Implement DCR through reviewed official primitives and the selected genuine synchronization engine. Support ordinary receive/change tracking, spendability, transaction trees, coin selection, signature verification, fees and durable UTXO reservations. Ensure no hidden second secret keystore remains outside the vault boundary.

**Exit evidence:** TON contract emulator and testnet traces; DCR simnet and current testnet transactions; leading-zero derivation regression; UTXO/change/gap recovery; first-send TON deployment; both adapters integrated into phrase-recovery, discovery and transaction-journal tests.

## Milestone 6 — HyperCore HYPE

Implement separate Core spot HYPE balance/history and ordinary peer transfer. Freeze the allowed action and exact signing schema against the official Python SDK. Validate the token ID, precision, environment/signature-domain separation and replay/nonce behavior. The HyperCore adapter never exposes trading or agent-key management; the separately authorized Bybit spot extension is not HyperCore trading.

Establish fresh and existing recipient behavior, prerequisites and any fees through authoritative documentation and actual testnet cases. If a prerequisite is unresolved, the receive/send flow must state the limitation instead of claiming universal address acceptance.

**Exit evidence:** independent typed-signature fixtures, accepted transfers with independent history/balance verification, correct failure/timeout/replay handling and a complete desktop flow distinct from HyperEVM. An adapter stub or empty response parser does not satisfy this milestone.

## Milestone 7 — Full security, recovery and stress campaign

Run the entire requirement matrix against integrated code. Complete the coverage floors per critical package, mutation-critical tests, fuzz corpora, race tests and crash injection. Review every uncovered security/fund-preservation branch. Fix failures without removing checks or weakening expectations.

Run the recorded bounded stress profile and a 24-hour soak. Measure actual memory, concurrency and latency behavior. Repeat fresh-install phrase restoration with multiple accounts on all families; verify that associations/discovery state require their documented reconstruction and that a phrase cannot restore a missing payment journal. Separately test authenticated local metadata, prior-generation crash recovery and unresolved-payment reconciliation. Test upgrades without address changes.

**Exit evidence:** reproducible reports and retained regression fixtures. Each result is tied to an exact commit/configuration. Record real observed numbers; never substitute proposed targets for measurements.

## Milestone 8 — Package and release candidate

Build the actual amd64 Go executables and complete portable desktop runtime, without a Debian package, and test the required Ubuntu/display-server matrix. Verify production fuses, sandbox behavior, isolated Electron prompts, debug restrictions and exclusion of test-only secret hooks. Provide installation, upgrade, uninstall-with-data-preservation and offline recovery instructions.

Produce the SBOM, dependency/license list, package hashes and release-signature instructions. If publisher-signing credentials are unavailable, clearly mark the package unsigned/development; local build completion is still useful. Do not publish or auto-update the user's machine without a separate request.

Submit the vault, IPC/GUI boundary, each signing adapter, build/update chain and recovery flow for independent security review. Address relevant critical/high findings. A review performed only by the implementation's author or an automated coverage report is not an external audit.

**Exit evidence:** the release checklist, package/install evidence, compatibility report, review disposition and explicit remaining limitations. Development builds may be complete while a production-release gate remains blocked; label that state accurately.

## Owner extension — Bybit Exchange and Ethereum USDT

Implement the actual V5 client and signer-owned encrypted credentials first, preserving the isolated Electron prompt and network-disabled signer/renderer. Add the fixed Ethereum USDT identity through the existing EVM transfer, approval, gas and receipt pipeline. Establish exact Bybit network/full-contract matching and required TON memo handling before enabling Deposit. Build the Exchange tab with public metadata only and three user-triggered actions; connecting and refreshing cannot spend funds.

Authorize each internal Funding/Unified transfer, unleveraged Market order and owned-address withdrawal independently. Bind canonical payload, credential/endpoint, immutable intent, session/expiry and durable ID before network submission. Reconcile exact original order links, UUIDs, acknowledged withdrawal IDs, complete fill pages and fees/taxes. Keep ambiguous requests UNKNOWN, with no automatic replacement. Document Bybit custody and its HMAC path-binding limitation rather than implying wallet-style consensus guarantees.

Collect the eight documented UAE withdrawal fields only through masked prompts and encrypted storage for the UAE endpoint. Gate unsupported regional questionnaires before transferring funds. A phrase-only restore cannot recreate exchange credentials, beneficiary information or the local exchange journal; test and explain those recovery limits.

**Local exit evidence:** meaningful client/signature/TLS/JSON/amount tests, signer persistence/approval/lock/canary tests, hostile-provider coordinator fixtures, actual local USDT node execution, UI/IPC validation, critical-package coverage floors, semantic mutants, race checks and bounded authorization/tax-parser fuzzing. Retain source/candidate identities and distinguish fake-provider scheduling tests from actual local-node and packaged-desktop tests.

**Separate external gates:** private Bybit account/permission/whitelist acceptance, funded deposit → credited balance → spot fill → owned-wallet withdrawal, all affected regions and independent review remain NOT_RUN until explicitly performed. Do not obtain those results using real user secrets, implicit mainnet activity or an automated publishing/funding step. These gates do not replace or waive the original wallet network, OS, soak and audit gates. The current checklist is [docs/EXCHANGE.md](docs/EXCHANGE.md).

## Required developer commands

Implement documented commands equivalent to the following targets; exact build tooling may follow the existing repository. Each command must run real checks and propagate failures.

| Target | Required effect |
| --- | --- |
| `make dev` | Build/install and start the app with the current mainnet default; testnet/local profiles and synthetic fixtures require explicit selection |
| `make test` | Hermetic unit tests, public vectors and deterministic regression corpora |
| `make test-exchange` | Actual Bybit/signing/IPC code with synthetic API fixtures, Exchange UI, security mutants and explicit local USDT node integration; never a funded Bybit campaign |
| `make test-security` | Vault, IPC, approval, isolated Electron phrase containment, leak, parser and mutation-critical checks |
| `make test-race` | Go race checks across relevant stateful components |
| `make test-interop` | Independent fixture parity for every profile/adapter |
| `make test-integration` | Local nodes/emulators and durable transaction-state scenarios |
| `make test-e2e` | Desktop flows, including isolated Electron prompt and Go authorization boundaries |
| `make test-testnet` | Separately configured, capped testnet integration; no implicit mainnet use |
| `make test-stress` | Bounded repeatable stress workload and measured report |
| `make test-soak` | Scheduled 24-hour scenario campaign |
| `make coverage` | Enforced per-package thresholds and readable coverage reports |
| `make package-go` | Go executables and portable archive plus manifests/checksums; no Debian package |
| `make package-desktop` | Complete portable Electron/Go runtime and archive with integrity checks; no Debian package |
| `make test-recovery` | Fresh installation copies and production prompt-protocol phrase restoration with same/different app passwords and public wallet comparison |
| `make verify-release` | Aggregate evidence/fuse/package gate; fails on missing mandatory evidence |

Go checks normally include `go test`, `go test -race`, `go vet`, an appropriate static analyzer and `govulncheck`; frontend checks include strict TypeScript, linting, meaningful unit/IPC tests and packaged E2E checks. Record supported tool versions. Do not claim that a command was executed merely because a script exists.

## Final implementation report

The owner-authorized NEAR staking extension (2026-10-28) adds `make test-near-staking` for focused security/race/frontend/mutation checks, `make test-near-staking-local` for the real pinned contract cycle, `make test-near-staking-gui` for actual isolated Wails presentation, and explicitly selected `make test-near-staking-testnet` for capped fresh-secret public-testnet acceptance. The existing local integration and fuzz campaigns include staking; release compilation remains separate from tests. The concrete TP-100–104 checklist and independent fixture provenance are in [docs/NEAR_STAKING.md](docs/NEAR_STAKING.md). This narrow extension does not enable arbitrary smart-contract signing or waive any original network, recovery or review gate.

Provide source commit, build/package paths, installation commands, exact supported OS matrix, per-chain capability/evidence table, coverage per critical package, mutation/fuzz/race results, stress environment/results, clean recovery evidence, dependency/security findings and review status.

Use explicit statuses: **PASS**, **FAIL**, **BLOCKED**, **NOT_RUN**, or **NOT_APPLICABLE with reason**. State why a blocked test matters, what unaffected work is complete, and the precise next action needed. Lack of a public faucet or external reviewer does not justify leaving the remaining local implementation unfinished, and does not justify calling the missing gate passed.
