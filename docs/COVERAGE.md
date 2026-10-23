# Go coverage and remaining-path review

This is an internal implementation review, not an external audit. Go measures statements, not branches. A passing percentage does not establish network compatibility, a clean-desktop recovery drill or production readiness. Public funded testnets, the full installed OS/display matrix, the 24-hour campaign and independent review retain their separate recorded statuses.

## Reproduction and evidence integrity

Run `python3 scripts/go-coverage.py` or `make coverage`. The wrapper executes the real application/unit/stress tests with atomic cross-package instrumentation, enables the pinned local dcrd simnet and TON contract emulator when available, and runs the real private-X11 GTK input suite. It unions identical production blocks from both profiles, retaining one statement denominator and the maximum observed execution count. This counts real calls across package boundaries rather than treating their own isolated test binaries as the only coverage source.

The wrapper records each source/test/module SHA-256 at the start and refuses changed-source measurements. It checks the pinned dcrd source-module identity before launching its isolated simnet. Missing local prerequisites remain **BLOCKED**; explicit Go skips remain **NOT_RUN** or **BLOCKED**, with reasons. All tests use public synthetic fixtures. No mainnet payment or real user secret is used.

Only `internal/secureui/automation.go` (an `automation`-tagged test fixture injector absent from production) and independent non-shipped oracle programs are excluded. Their exact paths/reasons are recorded. Production native input, clipboard pumping, lifecycle, all signing adapters and all application binaries remain included. Third-party libraries and native C/GTK code are not instrumented by Go's statement counters. They retain independent fixtures, runtime tests and audit obligations.

`reports/coverage.json` is the per-package floor result; `reports/go-coverage.json` records suites, source/profile hashes, local gate outcomes and skipped tests. `reports/go-uncovered.json` contains every unexecuted production block with its exact source excerpt and statement count. `reports/go-functions.txt` and `reports/go.html` provide function and source views. The block index below ties that machine report to the review disposition.

`python3 scripts/coverage-test.py` verifies that profile union does not multiply denominators or invent hits, exclusions remain explicit and limited to non-shipped sources, changed block shapes/modes and negative counters are refused, empty coverage is not passed, and critical security packages enforce 95%. The launcher uses the 90% application-glue floor; core, signer CLI, secure UI and supervisor use the stricter 95% security floor.

## Security and funds paths exercised

The recorded suites cover immutable plans/native approval, changed recipients/fees/networks, signature verification, durable signed artifacts before broadcast, old-identity reconciliation, reservation retention, failed persistence locking, unsigned cancellation, session/generation races and encrypted recovery. A specific coordinator regression changes the epoch after the durable `SUBMITTING` update and proves the adapter never broadcasts while the signed identity remains recoverable.

First-run tests require primary and previous generations to be absent before setup or restore. Existing, corrupt, unsafe and orphaned entries remain existing/unavailable rather than a new-wallet invitation. Nine deterministic cases introduce primary, previous or both ciphertext files during Create/import/Restore native input, then verify rejection, secret cleanup and unchanged original bytes. A separate real filesystem test introduces a primary after staging fsync; the kernel's `RENAME_NOREPLACE` preserves that late file. Initialization never copies a previous generation. These are bounded collision tests, not proof of atomic exclusion across two names against an arbitrary same-user filesystem writer.

Real native tests cover masked inputs, UTF-8 byte limits, cancellation, fixed value-free failure notices, PNG display, file selection, public clipboard ownership, scrolling exact effects and unavailable display. The combined profile measures `internal/secureui` at 99.09%; `Notice` is fully executed, while the partial-result wiping guard below remains unexecuted. This union includes the real private-X11 native profile (98.3% on its narrower run) without duplicating statement denominators. Background timers, programmatic field assignments, redraw/focus and clipboard events produce no activity. Private/public activity frames carry only `{}` with current request ID/session. Tests reject stale/malformed/flooding events and prove that initial locked prompts expire while their network access remains denied.

The [process-lifetime review](PROCESS_LIFETIME.md) covers the Linux creator-thread parent-death hazard. The launcher keeps one OS thread through child startup and `Exec`; the supervisor retains a dedicated locked creator thread until each signer is killed and reaped. Actual kernel controls reproduce child `SIGKILL` when an unowned caller thread retires. The fixed Child survives caller-thread retirement, answers a subsequent RPC, releases every owner over eight restarts, and receives `SIGKILL` after abrupt parent-process death. The last test independently reaps the child through a private subreaper and makes the peer wait on EOF, so ordinary pipe closure cannot pass it. These regressions confirm the isolated mechanism; they do not attribute an earlier packaged failure without its missing thread trace.

The successful isolation helper tests actual TCP/UDP sockets on all Go threads, raw packet/socket families, io_uring, TCP/UDP dials to IPv4/IPv6 loopback and a documentation-range LAN destination, custom-resolver DNS and HTTP without proxies. It requires socket permission denial, not merely an unreachable destination. DNS's underlying resolver dial errors are checked directly because `net.DNSError` does not preserve every wrapped errno.

A test-only Linux/amd64 ptrace parent exercises the compiled startup defenses without changing production code: it changes the result of an actually denied IP socket into harmless fd0, and separately changes a successful `PR_SET_NO_NEW_PRIVS` result into `EPERM`. Both produce the expected fail-closed startup error. No IP socket is opened by this substitution. The helper writes real Go raw coverage counters into the test coverage directory; the tracer never fabricates counters. A platform that blocks ptrace reports the test **BLOCKED**, so it cannot silently meet the floor through a skipped scenario.

## Remaining paths and dispositions

The retained defensive guards remain enabled. An unexecuted error return is still unexecuted; adjacent tests do not convert it into a passing test. The table distinguishes defensive paths from reachable glue that has narrower evidence.

| Area | Review of remaining statements |
|---|---|
| Vault envelope/files | AEAD/serializer errors after validated fixed parameters, OS read/short-write/close/sync/rename errors, directory-open errors and the portable backup destination metadata-query error after a verified directory open. Stage-by-stage faults, authenticated tampering, unsafe paths, old/new valid generation recovery and failed persistence are covered. Not every filesystem/kernel error is independently induced. Guards return errors and preserve/lock state; filesystem durability remains an OS contract. |
| Root derivation and amounts | BIP39/library failure after prior entropy validation, invalid scalars and checked conversion failures precluded by preceding canonical bounds. Independent vectors, Unicode/passphrase separation, invalid-child policy and overflow/precision cases are covered. No deterministic production RNG/HMAC override was added. |
| Signer policy | OS RNG termination, verified encryption/decryption mismatches and native/output/library failures following validated inputs; repeated backup/recovery checks and unsupported corrupted descriptors. The initial-commit write-error and post-commit previous-conflict returns remain unexecuted at the engine call site; lower-level atomic late-primary rejection and the nine pre-commit prompt collisions are passing tests with narrower scope. The postcheck preserves both files and locks the current session; it does not establish durable conflict quarantine after restart. Persistence/encoding/approval faults are exercised through real encrypted IO. A cryptographic API failure must return no artifact. Recovery validation has positive/negative tests; offline metadata never proves current chain authority. |
| Supervisor/launcher | Pipe/descriptor allocation exhaustion, CSPRNG failure, diagnostic OS-path failures, sanitized optional-environment skips and process startup/exec failure variants. Real manifest tampering, private-frame validation, child death, generation-bound queued calls and lock interruption are tested. No globally weakened sandbox or production fault hook is used. The launcher entry-point `os.Exit` wrapper is subprocess exercised separately from its in-process `run` counters. |
| Core | Startup failures, worker/ticker scheduling and queue-full fallback, already-inactive context cleanup, separate NEAR/Aptos association routing, unreachable-after-marshalling JSON errors, unsupported provider states and adapter expiry defaults. The association implementations and encrypted signer association checks have direct tests/local-node acceptance; a complete packaged association workflow remains narrower evidence. Session cancellation, native idle, queue epochs, final pre-broadcast guard, reserves and atomic settings have explicit regressions. |
| NEAR/Aptos | SDK serializer/signature failures after the signer reconstructs fixed validated transaction types, secondary parse guards and some history-entry provider failure returns. Current SDKs cannot produce most of these failures from the allowlisted bounded inputs. Public helper/authority failures are exercised on balance/preparation/submission routes, but not each identical history call site. Exact independent bytes/signatures, actual node execution, failure, key removal/restriction/rotation and sender/recipient history are separate passing gates. |
| EVM/HyperCore | Invalid BIP32 master/child scalars, signer/serializer self-check failures, strict parser states precluded by complete preceding parses, endpoint/request construction errors and explicit history saturation bounds. Known fee/authority/domain/asset/finality/replay mutations and independent fixtures pass. A history limit returns an error rather than an invented complete history. See `docs/EVM_CORE.md` for the adapter owner's review. |
| Decred | Official child/script/key/serialization failure after validated fixed inputs, request construction, bounded-wallet record saturation, a late tip-change and selected repeated ancestor/synchronization failures. Guards preserve unknown/spendability state and refuse incomplete scans. Real simnet send/receive/send-back, maturity/disapproval, fee/change, tree-aware inputs, official signature engine, wire allocation and independent HD tests cover the fund-preservation invariants. |
| TON | Fixed Wallet V5R1 builder/cell/signature failures, secondary validated-BOC parse errors, repeated trace-query errors and a nil outgoing-message dictionary guard which the current official TLB decoder materializes even for an empty dictionary. Invalid BOCs, authorities, seqnos, expiry, recipient traces and real TVM first deployment/send-back/history are exercised. Seqno advancement alone never becomes payment success. |
| Strict JSON/transport | Closing-delimiter states excluded by the standard decoder's earlier complete token parse. Duplicate/trailing/deep/oversized/unknown data and malformed numeric inputs are covered; the redundant fail-closed guards remain. |
| QR/native | The image-to-bitmap constructor's nil/error guard cannot be produced by the current library after valid nonzero bounded image decoding. Native input's partial-result wiping guard is retained; tested oversized input currently fails at its first entry, so the partial-earlier-entry cleanup loop remains unexecuted. Mutable Go inputs are cleared, while GTK/runtime/OS copies cannot be promised complete erasure. |

Native C and cryptographic library behavior require external review in addition to these source and runtime checks. The retained guards are intentionally conservative. Future dependency changes must rerun fixtures and review newly reachable outcomes, not discard them as permanently unreachable.

## Measured package table and block index

The following snapshot is generated from the final recorded profiles; the machine reports remain authoritative for reruns.

Recorded on 2026-10-23 with Go 1.27.3; wrapper status **PASS**, 56.011 seconds. Source base commit `23f9520b24c0e29d337c3bac8f387581819548a0`; the 130 measured source/test/module SHA-256 values are retained individually in `reports/go-coverage.json`. All 130 source hashes and both retained raw-profile hashes were reverified after the run. `reports/go-base.cover` SHA-256: `6189ed7ccb8024e003c4abd00b64795ab5be8ec70ff212426d0c13d2ae00929f`. `reports/go-native.cover` SHA-256: `55a9ad4b13c7df92094a9ce94f2e0b76eb252fd9435374f24f0a2a89f4b38277`.

The wrapper freezes the native profile as `reports/go-native.cover` before merging, so a later standalone native-security run cannot overwrite the measurement input. The current wrapper ran DCR simnet and the TON V5R1 emulator. Its optional NEAR/Aptos/geth node tests were **NOT_RUN**, explicitly listed with their prerequisite reasons in `reports/go-coverage.json`; separate preceding local-node acceptance reports remain valid only for their own recorded scope. The 24-hour case also remained **NOT_RUN**. Bootstrap/helper tests intentionally skip outside their child-helper invocations; those are test harness roles, not network acceptance.

| Shipped package | Statement coverage | Floor | Result |
|---|---:|---:|---|
| `cmd/wallet-core` | 95.78% | 95% | PASS |
| `cmd/wallet-launcher` | 93.33% | 90% | PASS |
| `cmd/wallet-signer` | 98.77% | 95% | PASS |
| `internal/amount` | 95.74% | 95% | PASS |
| `internal/chains` | 100.00% | 95% | PASS |
| `internal/chains/aptos` | 96.59% | 95% | PASS |
| `internal/chains/chainutil` | 98.94% | 95% | PASS |
| `internal/chains/decred` | 96.43% | 95% | PASS |
| `internal/chains/evm` | 96.54% | 95% | PASS |
| `internal/chains/hypercore` | 96.62% | 95% | PASS |
| `internal/chains/near` | 99.01% | 95% | PASS |
| `internal/chains/ton` | 95.67% | 95% | PASS |
| `internal/derivation` | 96.49% | 95% | PASS |
| `internal/ipc` | 100.00% | 95% | PASS |
| `internal/isolation` | 100.00% | 95% | PASS |
| `internal/journal` | 100.00% | 95% | PASS |
| `internal/qr` | 96.30% | 95% | PASS |
| `internal/secureui` | 99.09% | 95% | PASS |
| `internal/signer` | 96.77% | 95% | PASS |
| `internal/strictjson` | 97.44% | 95% | PASS |
| `internal/supervisor` | 95.65% | 95% | PASS |
| `internal/vault` | 96.31% | 95% | PASS |

The exact remaining 182 blocks contain 194 unexecuted statements. Every block belongs to the reviewed area table above. The index lists exact block start line/column positions; full spans, excerpts and statement counts are in `reports/go-uncovered.json`. No empty/zero-statement data-only package is represented as a tested implementation.

| Source | Unexecuted block starts (line.column) | Statements |
|---|---|---:|
| [cmd/wallet-core/main.go](../cmd/wallet-core/main.go) | `190.4`, `191.5`, `193.4`, `222.3`, `226.3`, `230.3`, `247.5`, `266.5`, `303.5`, `412.4`, `414.4`, `418.3`, `419.4`, `421.3`, `471.4`, `526.4`, `532.4`, `768.5`, `839.3` | 21 |
| [cmd/wallet-launcher/main.go](../cmd/wallet-launcher/main.go) | `30.15`, `44.3`, `78.5`, `94.4`, `105.3`, `162.3` | 6 |
| [cmd/wallet-signer/main.go](../cmd/wallet-signer/main.go) | `36.3`, `43.3` | 2 |
| [internal/amount/amount.go](../internal/amount/amount.go) | `47.4`, `55.3` | 2 |
| [internal/chains/aptos/aptos.go](../internal/chains/aptos/aptos.go) | `338.3`, `415.3`, `420.3`, `424.3`, `439.3`, `452.3`, `456.3`, `514.3`, `518.3`, `523.3`, `527.3`, `549.3`, `560.3` | 13 |
| [internal/chains/aptos/history.go](../internal/chains/aptos/history.go) | `67.3`, `70.3`, `74.3` | 3 |
| [internal/chains/chainutil/util.go](../internal/chains/chainutil/util.go) | `135.3` | 1 |
| [internal/chains/decred/decred.go](../internal/chains/decred/decred.go) | `143.114`, `169.3`, `235.3`, `245.4`, `262.3`, `268.3`, `286.3`, `299.3`, `319.3`, `324.3`, `589.4`, `627.3`, `632.4`, `637.4`, `643.4`, `649.4`, `654.3`, `658.3`, `693.3` | 19 |
| [internal/chains/decred/sync.go](../internal/chains/decred/sync.go) | `143.3`, `151.5`, `155.5`, `210.5`, `301.3`, `316.4`, `421.3`, `439.4`, `451.5` | 10 |
| [internal/chains/evm/adapter.go](../internal/chains/evm/adapter.go) | `66.3`, `290.3`, `303.3`, `311.4`, `318.4`, `327.3`, `460.3`, `467.3`, `475.3`, `479.3`, `483.3`, `509.3` | 12 |
| [internal/chains/evm/history.go](../internal/chains/evm/history.go) | `90.4`, `121.4`, `163.5`, `197.5`, `219.6`, `243.6` | 6 |
| [internal/chains/evm/keys.go](../internal/chains/evm/keys.go) | `54.3`, `67.5`, `79.4`, `87.4` | 7 |
| [internal/chains/evm/rpc.go](../internal/chains/evm/rpc.go) | `83.5`, `119.3`, `164.3` | 3 |
| [internal/chains/hypercore/adapter.go](../internal/chains/hypercore/adapter.go) | `131.4`, `143.3`, `429.3`, `471.3`, `496.3`, `503.3`, `514.3`, `518.3`, `526.3`, `548.3` | 10 |
| [internal/chains/hypercore/history.go](../internal/chains/hypercore/history.go) | `60.4` | 1 |
| [internal/chains/hypercore/keys.go](../internal/chains/hypercore/keys.go) | `39.3`, `49.3`, `62.5`, `74.4`, `82.4` | 8 |
| [internal/chains/near/history.go](../internal/chains/near/history.go) | `101.6` | 1 |
| [internal/chains/near/near.go](../internal/chains/near/near.go) | `492.3`, `497.3`, `511.3` | 3 |
| [internal/chains/ton/history.go](../internal/chains/ton/history.go) | `60.4`, `61.5` | 2 |
| [internal/chains/ton/network.go](../internal/chains/ton/network.go) | `114.3`, `163.4`, `307.4`, `351.3`, `355.3`, `366.3`, `370.3`, `425.4`, `430.4`, `437.4`, `449.5` | 11 |
| [internal/chains/ton/ton.go](../internal/chains/ton/ton.go) | `128.3`, `310.3`, `328.3`, `334.4`, `400.3`, `404.3`, `409.3`, `423.3`, `430.3`, `434.3`, `441.3`, `445.3`, `450.3`, `457.3` | 14 |
| [internal/derivation/derivation.go](../internal/derivation/derivation.go) | `60.4`, `80.3` | 2 |
| [internal/qr/qr.go](../internal/qr/qr.go) | `32.3` | 1 |
| [internal/secureui/native.go](../internal/secureui/native.go) | `139.4` | 1 |
| [internal/signer/engine.go](../internal/signer/engine.go) | `68.3`, `152.3`, `156.3`, `166.4`, `171.4`, `227.4`, `440.3`, `486.3`, `490.3`, `495.3`, `499.3`, `502.3`, `600.3`, `642.4`, `723.5`, `787.3`, `834.3`, `886.3` | 19 |
| [internal/strictjson/json.go](../internal/strictjson/json.go) | `69.3` | 1 |
| [internal/supervisor/child.go](../internal/supervisor/child.go) | `108.5`, `117.3`, `121.3`, `133.3` | 6 |
| [internal/vault/backup.go](../internal/vault/backup.go) | `65.3` | 1 |
| [internal/vault/envelope.go](../internal/vault/envelope.go) | `60.3`, `79.3` | 2 |
| [internal/vault/files.go](../internal/vault/files.go) | `103.3`, `122.3`, `143.3`, `149.3`, `159.3`, `165.3` | 6 |

A separately requested five-minute native campaign passed with63 actual XTest key events over5m11.962s (`reports/native-input-long.json`). The ordinary short native suite intentionally skips that long case and records the skip honestly; its own PASS does not stand in for the separate wall-clock result. Wayland and the clean installed desktop matrix remain outside this local coverage result.
