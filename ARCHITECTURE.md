# Implementation architecture

## A-01: Components and ownership

Use a Go module and a separately packaged Electron/TypeScript application. The owner's 2026-10-24 interface instruction replaces all Go/GTK application windows with Electron dialogs. Go still owns entropy generation, phrase/challenge validation, password authentication, immutable transaction interpretation, authorization and signing. Presentation and credential entry now place Electron main and the dedicated prompt renderer/preload inside the secret trust boundary; backend generation alone does not preserve the former native-display isolation.

| Component | Responsibilities | Must not receive |
| --- | --- | --- |
| Ordinary Electron renderer/public preload | Portfolio, network/account selection, ordinary previews, history, QR import entry | Passwords, phrases/passphrases, seeds, private keys, prompt API, arbitrary native APIs |
| Electron main | Public IPC, fixed windows/permissions, transient signer-authored prompt routing, public address copy/file dialogs | Root entropy, binary seeds, private/extended keys, vault decryption keys, generic signing authority |
| Dedicated Electron prompt renderer/preload | Masked credential/import/challenge input, pending phrase display, exact Go-authored receive/approval details | Root entropy, binary seeds, private/extended keys, vault keys, portfolio/generic IPC or filesystem APIs |
| Go wallet-core/supervisor | Network adapters, scheduling, public state and bounded transient prompt/reply forwarding | Root entropy, binary seeds, private derivation/signing keys or persistent plaintext credential/phrase state |
| Go secure signer/vault | Secret generation/storage, challenge/password verification, exact derivation, receive/approval data, final policy checks and signing | Unbounded renderer objects, arbitrary code or unreviewed signing payloads |

The core launches the secure child using inherited private handles. The signer owns authority state and generates one-use prompts; Electron displays them without accepting replacement content from the ordinary renderer. Transient password/phrase/passphrase bytes cross the inherited prompt channel through core/supervisor and Electron main. Core treats them as bounded transport data, validates schemas and never uses them for derivation or persistence; its memory is nevertheless inside that transient-secret trust boundary. The signer and Electron descendants have no remote IP access. Signed transactions may flow to the Go network core as public broadcast artifacts, but are not exposed as an unnecessary Electron signing API.

Use Go packages with narrow interfaces so an adapter cannot read a root seed simply because it needs an address. Separate public transaction preparation from private signing. Minimize dependencies in the secret-bearing executable; GUI and cryptographic dependencies still belong in its audit scope.

## A-02: Suggested source organization

| Path | Contents |
| --- | --- |
| `cmd/wallet-core/` | Network/coordinator process |
| `cmd/wallet-signer/` | Vault, signing policy and signer-authored prompt process |
| `internal/vault/` | Versioned envelope, atomic writes, crash-recovery generations and migrations |
| `internal/derivation/` | Versioned profiles, mnemonic validation, key derivation |
| `internal/policy/` | Immutable plans, approval binding, allowed operations |
| `internal/uiprompt/` | Bounded one-use prompt/reply protocol and Go recovery-word verification |
| `internal/secureui/` | Retained legacy GTK implementation/tests; absent from shipped command dependency graphs |
| `internal/ipc/` | Schemas, framing, message bounds, lifecycle |
| `internal/amount/` | Exact atomic amount parsing/formatting |
| `internal/chains/` | EVM, HyperCore, NEAR, Aptos, Decred and TON adapters |
| `internal/store/` | Public state, encrypted private metadata, durable transaction journal |
| `desktop/` | Electron main/preload/renderer and packaging |
| `testdata/`, `tests/interop/` | Public deterministic fixtures, independent oracle generators |
| `tests/e2e/`, `tests/faults/` | Packaged flows, hostile RPCs, crash/race injection |
| `docs/` | Implemented formats, threat model, recovery, evidence and release checklist |

Adapt to an existing repository without replacing its unrelated content. These paths are suggested architecture, not an instruction to fabricate empty files for progress.

## A-03: Data model and typed boundaries

Use durable IDs distinct from display names: `WalletID`, `AccountID`, `DerivationProfileID`, `NetworkID`, `AssetID`, `OperationID` and `VaultGeneration`. An asset is identified by network plus native marker or exact contract/token identity, never ticker alone. A HyperCore ledger ID is not an EVM chain ID.

A persisted public recovery descriptor records scheme and version, exact path/index, public key, address type/authentication scheme, network and relevant contract parameters. Sensitive associations and labels are encrypted at rest. Its canonical serialization must be independently testable. Public descriptors may reveal address associations, so they are not exported or uploaded automatically.

All API quantities are decimal strings of atomic integers with explicit units. Do not serialize uint64 values into JavaScript Numbers. Keep big integers/checked fixed-width amounts in Go and strings or explicitly controlled BigInt in TypeScript. Validate maximum lengths before parsing. Store display decimal precision separately from quantities; token metadata is untrusted until matched to the registry.

Define explicit protobuf or equally strict versioned schemas for IPC. Include request ID, session generation, protocol version and operation type. Unknown fields/actions, duplicate IDs, oversized frames, invalid enums and inappropriate state transitions fail closed. Send cancellation and lifecycle events explicitly. Every error response is sanitized.

Allowed public application operations can include `ListNetworks`, `ListAccounts`, `GetBalances`, `GetHistory`, `RequestCreateWallet`, `RequestImportWallet`, `RequestUnlock`, `Lock`, `AddAccount`, `ShowVerifiedReceive`, `PrepareTransfer`, `RequestTransferConfirmation`, `GetOperationStatus`, `RequestRecoveryConfirmation`, `RequestPhraseRestore`, and `RequestPasswordChange`. Credential parameters are absent from the ordinary portfolio bridge; these methods request a separate Electron prompt. No public `GetMnemonic`, `ExportPrivateKey`, `SignBytes`, `Eval`, `ReadFile` or generic wallet JSON-RPC passthrough exists.

The prompt uses `wallet://prompt/`, its own minimal preload and a fresh nonpersistent Electron session partition per window, separate from `wallet://app/`. Main binds delivery, reply and public-address copy to the exact live window/main frame, pending request/session/generation and signer-generated 128-bit prompt ID. A prompt is delivered once and a reply consumed once; unknown/extra fields, wrong-kind values, duplicates, stale IDs and unsolicited prompt frames fail closed. Only input replies contain masked values; recovery acknowledgment alone never authorizes the wallet. Go issues a separate three-word challenge and verifies it before persisting completion. Prompt data and DOM are cleared on completion/cancellation; lock, hide, navigation, crash and teardown revoke the prompt. Neither best-effort clearing nor ephemeral sessions guarantee erasure of all Go/V8/Chromium/OS copies.

## A-04: Derivation and account discovery

Profiles are data with immutable semantics, not a set of mutable defaults. The exact rules are in BLOCKCHAIN_COMPATIBILITY.md. Once an account exists, changing the global default must never change its address. New profile versions require an explicit new account or documented import option.

Generate additional wallet groups by deterministic account index. Persist actual indices when invalid BIP32 children are skipped, obeying the profile's rules. Keep DCR receive/change address indices independently. Maintain bounded address/account discovery with a visible scan range, gap limit and last verified checkpoint. Give users an explicit way to extend a scan; do not perform unlimited network discovery implicitly.

A phrase can reproduce keys without reconstructing all account identities. NEAR names, Aptos rotation, TON contracts and historical DCR profile variants require their documented metadata/discovery paths. Do not “recover” a new empty account and declare existing funds absent.

Watch-only data must never gain signing authority by being paired with a similarly named account. Verify key/address/control relationships after restore and before a send. Unknown contract code, missing access keys or unsupported authentication schemes lead to a clear unsupported state.

## A-05: Transaction adapters

Separate responsibilities into narrow public preparation and private signing interfaces. Public adapters validate recipients, obtain state, estimate fees, construct a typed plan, submit a signed artifact and reconcile its lifecycle. Secure adapters derive only registered account keys, reconstruct/decode the prepared transaction, check policy and produce/verify a signature after a scoped Electron approval and Go password verification.

Each transaction plan contains chain/ledger identity, source account/profile, exact asset identity, recipient(s), atomic amount(s), memo/comment, maximum fee and fee currency, nonce/sequence/expiry, applicable inputs/change, relevant contract parameters, state provenance and the canonical bytes/digest to be approved. Never rely on ticker labels to describe a transaction's effects.

The EVM adapter may share transaction primitives across Ethereum, Base, HyperEVM and Horizen; network policy and fee estimates remain explicit. HyperCore uses a separate adapter. Decred uses official wallet/transaction primitives and genuine synchronization. TON signs wallet-contract messages, not a synthetic account-model transfer. NEAR and Aptos preserve their own authority, serialization and nonce semantics.

Local signature verification is mandatory, but verifying a signature against bytes generated by the same code does not prove those bytes represent the correct protocol intent. Independent fixtures and actual node acceptance are separate gates.

## A-06: Durable payment state machine

Persist the operation identity and plan before asking for confirmation. The conceptual states are `DRAFT`, `PREPARED`, `AWAITING_CONFIRMATION`, `CANCELLED`, `SIGNED_NOT_SUBMITTED`, `SUBMITTING`, `SUBMITTED`, `UNKNOWN`, `CONFIRMED`, `FAILED`, `REPLACED` and `REORGED`. Define actual allowed transitions and invariants in code; not every chain uses every state.

The approved transaction digest and one-use capability are tied to the live signer session and expire. Signed bytes and deterministic transaction identity are durably recorded before the first network attempt. Apply per-account/chain nonce or sequence reservations and DCR input reservations transactionally.

An HTTP timeout can mean the network accepted the payment. Reconcile the original hash/action/sequence through the adapter before considering another payment. Automatic re-signing with a new nonce after a timeout is prohibited. Idempotent resubmission uses the exact signed artifact where the protocol allows it. Any fee replacement or new signature requires a new approval and remains linked to the original operation.

On restart, load unresolved operations before allowing conflicting sends. Reconstruct reservation state from durable records and current chain evidence. Handle accepted-but-failed transactions and reorgs without inventing success or silently rebroadcasting new payments. Show the best verified state and its uncertainty.

For TON and HyperCore, implement an explicit mapping between submission identifiers, signed-message/action identity and eventual transaction/history evidence; do not assume an EVM-style transaction hash API. For DCR, include transaction tree and chain reorganization/disapproval handling.

## A-07: Persistence without secret duplication

The signer owns the encrypted root vault and any keys used for encrypted metadata. Avoid SDKs that silently write plaintext mnemonic files, unlocked key stores or credentials into their default home directory. Redirect and audit all library storage paths. A dcrwallet integration must not retain an independently unlockable secret copy outside the vault policy.

The core owns scheduling and public network caches. Sensitive metadata/journal records can be persisted through a narrow authenticated storage interface owned by the vault component; storage requests cannot return encryption keys. Signed transactions are not secret keys but remain privacy-sensitive. Document which public metadata, if any, stays in plaintext and why. Do not make an untested promise that all OS caches are encrypted.

Before lock, preserve critical transaction records; after lock, stop background account queries by default and clear volatile state. Pending results can be reconciled after unlock using the prior durable record. Do not keep a hidden unlocked signer solely to continue UI updates. An on-chain transaction already in flight continues independently of application lock.

The encrypted live database, phrase recovery and schema upgrades must support clean recovery after interruption. Public chain data may be redownloaded; missing secrets may not be regenerated. Never create a new wallet automatically when the existing one fails to unlock.

## A-08: Networking and trust

Go wallet-core performs all remote chain operations over validated TLS endpoints or an explicitly configured local node. Validate the actual chain/network identity before querying account state or broadcasting. Prevent redirect-based credential leakage, bound response bodies and nesting, apply timeouts/backoff and treat malformed numeric responses as errors. Avoid arbitrary URLs embedded in QR codes or chain metadata.

RPC endpoints can lie, censor and correlate public addresses. Matching two providers is a consistency check, not proof of consensus. Prefer validated chain/proof data where the chosen SDK offers it, and use the documented Decred SPV/trusted-node model. Show incomplete synchronization, stale heads and provider disagreement explicitly. Test fallback so switching an endpoint cannot switch the ledger.

No hosted service is required to create a wallet, record or restore its phrase, derive addresses or verify signing fixtures offline. Live balances and submission require a chain connection. An optional fiat-price service is outside v1 and must never be part of a signing decision.

## A-09: Build and deployment

Pin a currently supported stable Go toolchain, Electron version, Node build runtime and package-manager version at implementation time. Record minimum Ubuntu runtime libraries. Prefer maintained, tagged SDK releases and check Go module compatibility; do not use unreviewed repository master branches in production.

Build for stock Ubuntu as a non-root user. Ship Go executable artifacts and an explicit complete portable Electron/Go runtime with fixed binaries/assets and documented native shared-library prerequisites. The owner excludes Debian packaging; local installation needs no sudo. Application execution must use an ordinary user and preserve the sandbox. Resolve Linux sandbox/AppArmor compatibility through supported application-specific configuration rather than system-wide weakening.

Provide separate development/testnet and production build profiles. Test-only RNG injection, secret fixture hooks, debug automation and confirmation bypasses must be absent from production binaries. Mainnet send logic remains part of the implementation, but production release requires the recorded gates. No automated test or build script should spend mainnet funds.

An auditable dependency and build manifest accompanies each artifact. The release report must identify the exact commit, toolchain, dependencies, package hash, configuration and tests actually executed.
