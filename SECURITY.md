# Security requirements and threat model

These are engineering requirements for the future implementation. No cryptographic library, GUI toolkit, testing percentage or isolation feature is by itself evidence of an audited wallet.

## S-01: Assets, adversaries and claims

Protect the recovery entropy/phrase, binary seed, optional BIP39 passphrase, private/extended private keys, vault encryption keys, app password, and authenticated intent to send funds. Treat stored labels, addresses and history as privacy-sensitive metadata. An address is public on chain; the user's association with it need not be public.

The design must withstand copied locked-vault files, malformed backups, hostile RPC responses, token/QR strings, hostile ordinary renderer content, IPC misuse, accidental logging, interrupted writes, stale state and concurrent sends within the stated boundaries. Remote providers must never be trusted to supply transaction bytes that are signed without independent local interpretation.

Do not promise protection against a compromised kernel/root account, an attacker controlling the signer binary, hostile input/display components, same-user malware capable of capture/injection, or physical live-memory extraction. Native confirmation improves separation from the ordinary renderer; it is not a hardware trusted display. X11, accessibility, GTK/C libraries, Go runtime copies, compositor behavior and a compromised Electron main can still undermine local security. One master phrase also creates a shared compromise scope across all supported accounts.

Encryption is protection at rest. Keys necessarily exist briefly in plaintext in a signing process. Minimize and contain that exposure; never claim that encryption makes extraction impossible while unlocked. List these boundaries plainly in the delivered threat model.

## S-02: Secret containment and native input

Only the dedicated Go vault/signing process may hold recovery material, derivation private keys, plaintext vault keys and password/phrase inputs. Go wallet-core can access public wallet state, plans and signed transactions, but not recovery secrets. Neither Electron renderer, preload, main process nor a production JavaScript SDK receives them.

Use a small native Go-owned input and confirmation surface, for example through a maintained GTK binding. Evaluate packaging/accessibility requirements early and pin the implementation. It collects the password and optional phrase/passphrase directly. A general Electron input box followed by IPC forwarding would violate the boundary. Do not fall back to it silently if the native integration is difficult.

Native inputs are masked, input-only, do not autocomplete or offer a reveal button, and clear their buffers after use. No secrets in process arguments, environment variables, shell commands, command history, filenames, application URLs, generated images or terminal prompts. User-initiated paste of an already known import phrase may be supported only as an explicit action inside the native input surface; never read clipboard contents in the background. No copy operation for secrets.

Do not claim OS-wide clipboard erasure: another application or clipboard manager may already retain what the user pasted. The wallet itself must never place recovery secrets on the clipboard.

Derive private keys only when needed. Avoid storing duplicate per-chain keys if the encrypted root and immutable profiles can reproduce them. Use mutable buffers where practical, explicit best-effort clearing, minimal lifetime, and library destroy/zero methods. Review allocation/copy behavior at Go and C boundaries. Do not represent these techniques as guaranteed erasure of all runtime or OS copies.

Disable production debug endpoints, heap/CPU profiling, diagnostic dumps and crash uploads. Disable core dumping for secret-bearing processes using supported OS facilities, with a startup self-check. Use best-effort memory locking/non-dumpable settings where practical; report constraints honestly. Never require the application to run as root or relax system-wide ptrace/sandbox protections.

## S-03: Vault cryptography

Use standard primitives from reviewed libraries. The specified baseline is **Argon2id** for deriving a 32-byte storage key and **XChaCha20-Poly1305** authenticated encryption through Go's maintained cryptographic packages. The 64 MiB / three-pass / four-lane Argon2id profile corresponds to RFC 9106's memory-constrained recommendation; the proposed desktop default of 256 MiB is this application's policy and must be measured on supported hardware. Sources: [RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html), [Go Argon2](https://pkg.go.dev/golang.org/x/crypto/argon2), [Go XChaCha20-Poly1305](https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305).

Use at least a fresh 16-byte cryptographic salt and a fresh 24-byte random XChaCha nonce for every vault encryption. Encryption keys are 32 bytes. Never derive the nonce from time, wallet address or a counter that can reset on restore. Handle RNG failure as a fatal creation/encryption failure, never a fallback.

For the initial format, a single authenticated encrypted vault is preferable to a custom hierarchy of wrapped secrets. A separate encrypted history store may use a reviewed domain-separated key design, but requires its own documented nonce/record-authentication and crash-consistency contract. Do not create a second plaintext keystore in an SDK or dcrwallet database. All secret-bearing databases must obey the same password-based protection boundary.

Document a canonical, versioned envelope containing magic, format version, KDF identifier/version, memory/time/parallelism parameters, salt, AEAD identifier, nonce and ciphertext/tag. Authenticate the complete canonical header as associated data; authenticate the root and its derivation/profile metadata together. Reject duplicate fields, unknown mandatory fields, ambiguous encodings and trailing data. Avoid inventing a new cryptographic primitive or proprietary password hash.

Suggested v1 bounds, to be finalized and tested in the format specification: maximum header 4 KiB; maximum core vault 16 MiB; Argon memory 64–512 MiB, passes 3–6, lanes exactly 4; one active KDF per vault. Default 256 MiB/3/4, with measured selection down to 64 MiB/3/4 when necessary. Never silently reduce the floor to make a slow device appear fast. Maximums are defensive parser limits, not a claim that every machine can allocate them safely. Reject unsupported high-cost imports with a clear local error before allocation; do not silently change their parameters.

Validate sizes, arithmetic, versions and resource limits before running a KDF on an unauthenticated header. Authenticate before parsing or using plaintext. Wrong password or corrupted ciphertext must reveal no partial data. Use one general unlock failure for failed authentication; do not echo any input or dump cryptographic structures.

Use established library verification APIs, not handwritten constant-time comparisons. Local unlock backoff can reduce repeated UI abuse, but does not stop offline guessing of a copied vault. Password length/strength feedback must be local and support long Unicode input and password-manager workflows. Specify password encoding/versioning exactly; do not silently trim, truncate, case-fold or normalize a storage password. BIP39 normalization is a separate rule.

## S-04: Files, backup and migrations

Keep the application data directory owner-only (`0700`) and sensitive files owner-readable/writable (`0600`) on a supporting filesystem. Use a restrictive umask. Validate ownership and reject unsafe symlink/hard-link targets for the live vault and transactional temporary files. Never overwrite a path supplied by an untrusted renderer without independent path validation and the appropriate native user action.

Use lockfiles or supported OS locking to prevent two active writers. Atomic replacement must include writing an encrypted temporary file in the same filesystem, flushing it, atomic rename and directory durability as supported by the OS. Temporary files are ciphertext only. Inject failures at every stage. Preserve a known-good encrypted generation until the next generation is durably committed and verified.

Disk full, short writes, process termination, permissions changes and power-loss simulation must leave either a valid old generation or a valid new generation. A corrupt newer file must not cause silent rollback, account renumbering or replacement with a newly generated root. Offer explicit recovery from a verified encrypted prior generation.

Version migrations authenticate the old format before conversion, preserve recovery identities, create a verified new generation, and remain reversible via the prior encrypted backup. Never “fix” derivation by silently changing keys. A supported format upgrade is different from selecting a different wallet profile.

Password changes decrypt/authenticate the existing vault and re-encrypt with new salt, nonce and password-derived key, without changing the root. Verify the new file and identical public descriptors before replacing the live generation. Test interruptions and old/new-password behavior. Old copies are still recoverable with old passwords; file deletion on SSDs cannot guarantee secure erasure.

Backup/export produces only the encrypted portable format. Restoration uses a new destination/profile by default, validates the complete contents and metadata, and verifies public recovery descriptors before replacing anything existing. Backups must not depend on desktop keyrings, OS user identities or unavailable cloud services.

## S-05: Process isolation and permissions

Launch executables from fixed packaged locations; never let a renderer choose executable paths, shell fragments or library search paths. Sanitize inherited environment variables relevant to runtime injection and diagnostics. Use shell-free process creation. Verify packaged child-binary identity against the release manifest, recognizing that an attacker controlling all installed binaries/manifests is outside this defense.

Use inherited private pipes or an equivalent inherited socket pair for typed, length-delimited IPC. Do not open an unauthenticated localhost HTTP/WebSocket wallet API. Bind each session to the intended parent/child lifecycle, invalidate capabilities on lock/restart, impose strict message bounds, and close unexpected handles. Peer-UID checks alone do not protect against same-user malware.

The signer must have no remote IP networking. Enforce and test OS restrictions preventing AF_INET/AF_INET6 connections, while retaining local display/D-Bus/Wayland/X11 facilities needed by the native UI. “Network-isolated” does not mean “no local sockets.” Do not grant broad filesystem or subprocess APIs through its RPC interface.

Keep the signer interface limited to creation/import/unlock/lock, public derivation for named registered profiles, encrypted backup/restore/change-password operations, verified receive display, and approval/signing of allowlisted transfer plans. No generic eval, filesystem read, arbitrary derivation path, seed getter, raw signature or remote plugin loading.

## S-06: Electron configuration

Explicitly enable `contextIsolation`, renderer sandboxing and `webSecurity`; disable `nodeIntegration`, insecure content and webviews. Load packaged assets through a narrow custom application protocol. Use a strict CSP with no remote scripts, inline script exceptions or eval. Deny navigation, unexpected windows, downloads and permissions by default. Validate sender origin, frame, window and message schema for every privileged IPC call. Expose only narrow typed methods through preload. Source: [Electron security guidance](https://www.electronjs.org/docs/latest/tutorial/security).

Production renderer networking must be disabled, including WebSocket, WebRTC and indirect remote resources. All required RPC activity goes through Go. Render token names, errors, account labels and memos as inert text. Bundle fonts/icons; do not fetch arbitrary URLs from chain metadata. Handle external block-explorer links only after validation against approved schemes/hosts and a deliberate user action.

Set appropriate Electron fuses for the selected release to disable RunAsNode, Node option injection and inspect/CLI hooks; use available application integrity/ASAR protections and explicitly document platform limitations. Do not claim that an unsigned hash proves publisher authenticity. The shipped binary configuration must be tested independently of an instrumented test build. Source: [Electron fuses](https://www.electronjs.org/docs/latest/tutorial/fuses).

Never solve Ubuntu packaging problems by adding `--no-sandbox`, running the wallet as root, globally disabling AppArmor/user-namespace restrictions, or making broad changes to the user's system. Provide a package-specific supported sandbox configuration and test it on stock Ubuntu installations.

## S-07: Approval and transaction integrity

The secure component reconstructs or independently decodes the actual transaction and produces its own confirmation summary. It validates the account/profile, asset/network, recipient, amount, every output/change destination, contract method, fee limits, sequence/nonce, expiry and relevant account authority. It must not trust a friendly summary supplied beside opaque bytes.

Approval is bound to a canonical transaction digest, a wallet-session generation, a one-use operation ID and an expiry. Changing any meaningful field requires a new native confirmation. An ordinary renderer cannot synthesize an approval event. Invalid/expired/replayed/cancelled capabilities cannot sign or release a signature.

Authenticated encryption does not provide transaction authorization. An unlocked vault alone is not permission to spend. Reauthenticate per send by default. No automatic conversion, chain switching, unlimited approval, bridge call or user-controlled opaque signing extension.

Persist the prepared/signed operation safely before its first transmission. The network component must broadcast the exact approved signed bytes/action. Locally verify signatures before accepting the result, and test receipt/execution state. A balance query or successful RPC HTTP status is not proof of successful payment.

Serialize sensitive state transitions. On lock, immediately reject new signing, invalidate unapproved plans and prevent an in-flight operation from releasing a new signature after the lock boundary. Already signed/broadcast transactions cannot be recalled; retain their durable journal and reconcile them after unlock. Document the exact commit point and test its races.

## S-08: Logs, diagnostics, dependencies and releases

Use allowlisted structured diagnostic fields, not a blacklist of seed-like strings. Errors must carry stable codes and sanitized context. Never log HTTP authorization, raw secret-bearing requests, vault plaintext, credentials, key objects or cryptographic buffers. Public fixture secrets may be present only in explicitly marked test sources and test-only outputs; real user data never enters CI, a coding assistant, issue tracker or support archive.

Pin dependency versions and hashes, keep lockfiles, review install/build hooks, generate an SBOM, run appropriate vulnerability scanners and record reachable findings. Use maintained official implementations where available. A community library being linked from a project's docs is not an audit. Do not import a giant wallet framework just for one trivial utility without reviewing its added attack surface.

Build and publish reproducible instructions, package hashes and publisher-signature verification procedures. If release-signing keys are unavailable, deliver an explicitly unsigned development artifact; never fabricate a signature. Automatic updating is disabled in v1. A future updater must authenticate publishers and prevent rollback before it is enabled.

No production-readiness claim until the test plan and independent security-review gates are satisfied. The security report must distinguish verified behavior, design intent, missing evidence and residual risks.
