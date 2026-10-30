# Go coverage and remaining-path review

## Exchange-provider change — 2026-10-30

The new client packages initially measured 90.51% (Binance) and 93.00%
(KuCoin), below the existing 95% critical-package floor. That first check is
retained as `reports/wallet-history-client-floors.log` and
`reports/exchange-clients-final.cover`; it is not a coverage pass.

Additional protocol controls exercise exact server-ID transfer pages, missing,
duplicate and truncated records, each documented funding status, malformed
acknowledgements without retries, withdrawal dates, bounded deposit
continuations, separate Funding/Spot balances and read failures after a successful
account-mode check. The final race measurements are **97.31% Binance** and
**96.00% KuCoin**. Their combined atomic profile
`reports/wallet-history-clients-reviewed.cover` passes the unchanged enforcer;
`reports/wallet-history-client-floors-final.log` records both results. No
production path, package or threshold was excluded or relaxed.

Remaining client blocks include additional invalid-query/authentication and
observation rejection paths and unused legacy normalization helpers. They remain
in the denominator. These local tests use explicit protocol peers and owned TLS
servers; they do not certify real service account eligibility, fees or funded
acceptance. KuCoin's unobservable additional KCS fee behavior remains a blocked
capability rather than a coverage exclusion.

The complete combined Go/native coverage workflow has **NOT_RUN for this
candidate**. The scoped actual history/layout GUI pass is uninstrumented and does
not replace the full generated-root coverage workflow. Earlier historical GUI
coverage failures and their exact limits remain in [STATUS.md](STATUS.md).
Frontend coverage passes separately with its unchanged floors. The older
campaigns below apply only to their recorded source and runtime identities.

## Current Wails coverage campaign — 2026-10-28

The 2026-10-28 Wails/WebKit replacement changes the shipped UI and invalidates
previous GTK coverage for the current candidate. Coverage integration is now
implemented: `internal/webui` is a **critical package with a 95% floor**, and all
existing 90%/95% floors remain unchanged. The recorded source-stable combined Go
campaign **PASS** covers **all 33 measured package floors**, all four required
local execution gates and **1,240** source inputs in **433.233 seconds**. Wails
Go measures **95.43%**, the wallet controller **96.87%**, retained GTK **95.09%**,
EVM **97.05%** and typed token-policy decoding **100%**. The actual instrumented
GUI passes all **18 workflows**, with real counters from **31 distinct private
processes**. Independent source/profile/runtime verification passes. Raw
measurements later in this document are explicitly historical GTK evidence.

`make coverage` now builds a separate instrumented executable with
`production,webkit2_41,portable,walletcoverage`, instruments the command/controller/**webui**/
memory-protection packages, and drives its actual Wails windows through
`scripts/wails-wallet-test.py`. Its report is `wails-wallet-coverage.json`; the
raw profile remains `go-native-runtime.cover` for verifier compatibility.
The base suite uses production/WebKitGTK 4.1 build tags while retaining legacy
seccomp tests. Existing GTK regression profiles and all original Decred/TON/USDT
integration gates remain required, alongside real two-token ERC-20 execution.
No shipped production path is excluded to
improve a percentage. Go counters do not measure React/JavaScript, C or WebKit
library behavior.

The base campaign explicitly enumerates every wallet-module package before
instrumentation. Go 1.27.3's `-coverpkg=./...` also matched the local Wails module
replacement and supplied dependency counters with module-relative filenames.
The collector now checks ownership/module/source directories and instruments
all 38 wallet package identities explicitly. Foreign counters fail rather than
being removed from measured profiles. The reviewed Wails fork retains its own
security tests; a third-party framework coverage percentage is not substituted
for wallet coverage.

Current source binding includes Go/C/header code, React/TypeScript/CSS/HTML
sources and tests, frontend package/configuration pins, the complete reviewed
Wails fork and both fork manifests, fork verification/security drivers, and the
actual Wails GUI coverage driver. Missing required current UI inputs fail.
Historical Electron-only toy fixtures without any Wails module/package marker
retain their historical verifier scope. A Wails build cannot reuse a GTK runtime
report or omit `internal/webui` statement counters.

Coverage-integrity and release-evidence regressions **PASS**. These include
synthetic 94% Wails counters failing the
unchanged 95% floor, forged lower floors, changed React/fork pins, missing UI
sources and reassigned GTK runtime evidence. Synthetic counters test enforcement;
they do not measure the wallet. Standalone GUI/security/recovery acceptance does
not substitute for the combined coverage campaign. The retained
Electron production verifier also remains unable to certify a Wails production
release.

Private-process collection uses a separate, explicitly tagged diagnostic build.
Only `walletcoverage` builds can pass an absolute, owned, mode-0700 counter
directory to a private prompt and flush real Go counters before replying. The
ordinary build imports no `runtime/coverage` package through this helper and
continues to omit `GOCOVERDIR` from private-process environments. Counters contain
statement metadata/counts, never prompt values. The driver records each actual
private X11 window's process ID; all those distinct processes must supply
counters. A parent or unrelated helper cannot stand in for a private prompt.

The hosted full, uninstrumented Wails campaign at commit
[`e014505`](https://github.com/neodix42/crypto-wallet/commit/e014505) passed all
18 workflows with stable sources, running from 07:12:09 to 07:31:34 UTC
(19 minutes 25 seconds). Its successful duration exceeds the collector's former
15-minute overall GUI limit. The separate instrumented campaign therefore has
a 40-minute overall subprocess deadline, allowing roughly twice that observed
hosted duration. All per-action deadlines, full workflow selection, counter
requirements and coverage floors remain unchanged. This deadline correction
does not itself record a passing hosted instrumented campaign.

A later unchanged-source local rerun exposed an intermittent test-tracer
failure. A covered 200-repeat run reproduced an exiting helper thread causing
`PTRACE_GETREGS` to return `ESRCH`; the assertion's deferred `exec.Cmd.Wait`
then blocked while other ptrace stops still needed draining. The failed report
and reproducing stack remain separate from passing measurements. The test-only
tracer now drains its own process group, handles the thread-exit race and uses
bounded ptrace-aware cleanup with direct output files. Real exec-stop and
clone-stop regressions require complete reaping and a dead helper. The original
fault-injection and successful-rejection assertions remain mandatory; no
production isolation behavior or coverage floor changes. The fixed covered
stress campaign passes all 250 iterations in 130.811 seconds without a skip or
failure, including both real stopped-tracee cleanup stages. Ten repeated legacy
and memory race runs, focused portable race tests, the nonportable unit suite
and vet also pass. These scoped results do not substitute for the final
revision's full measurement or hosted result.

The reported hosted failure exposed two separate prerequisites: production GTK
startup must reject an absent display before opening storage, and graphical
coverage must run under the already-configured CI-only namespace profile. Both
have regressions. Failed GUI startup retains its original reason and marks
measurement NOT_RUN or INCOMPLETE instead of hiding it behind missing metadata.
Inherited public staking opt-ins are removed before this local campaign.

The shipped React UI is measured independently with pinned Vitest/coverage-v8
5.0.5, including both real mount entrypoints and every production TS/TSX module.
The measured **51 tests PASS**, with **98.45% statements / 94.69% branches**.
Private UI and protocol files retain 95% statement/90% branch floors; other
executable modules retain 90%/85%. Type-only declarations report a zero executable
denominator explicitly. The retained Electron measurement still passes its own
unchanged floors. `reports/frontend-coverage.json` requires both campaigns and
binds source, compiled assets and raw summaries; these component tests do not
substitute for real WebKit/GUI acceptance.

Separately, `scripts/wails-inspector-test.py` passes on the current executable:
actual WebKit helper processes initialize while inherited TCP/HTTP inspector
settings produce zero listeners. Its binary/source-bound report is
`reports/wails-inspector.json`; it is security regression evidence, not statement
coverage. Disabling developer extras alone was experimentally insufficient;
trusted Go startup also clears inspector/engine injection settings before WebKit
initializes.

The current aggregate lists eleven explicit base-suite skips. The unused
launcher bootstrap-worker parent is not a separate scenario. Aptos local-node
cases, NEAR sandbox/local staking and local EVM protocol acceptance run in the
separate passing eight-gate `make test-integration` campaign, rather than being
mislabelled as base-suite passes. The public NEAR read-only probe, two public
staking tests and 24-hour soak are NOT_RUN in this campaign. Earlier partial
public staking evidence retains its separate scope; no mainnet spending occurs.

## Historical GTK measurement (2026-10-27; not current Wails evidence)

The historical 2026-10-27 source-stable GTK measurement passed **all 31 then-measured package floors**. Native GTK Go code measures **95.61%**, the native wallet controller **96.66%**, OS session monitoring **95.87%**, supervisor **97.47%**, portable/legacy memory protection **100%**, and the application command **97.37%**. The original 90%/95% floors are unchanged; native UI, controller and session monitoring enforce 95%.

The **aggregate remains FAIL** because the original pinned Decred `dcrd v1.10.9` simnet prerequisite is **BLOCKED** on this machine. This does not turn passing packages or executed tests into failures, and it does not turn the missing integration test into a pass. The TON V5R1 emulator and actual local USDT EVM execution gates both pass. Decred work was excluded from the owner's current implementation scope; its gate remains present.

Go measures statements, not branches, C ABI behavior, cryptographic soundness or independent security review. Passing local coverage does not establish production readiness. The exact reports below describe the historical GTK source snapshot only. They are not assigned to the current Wails executable.

## Reproduction and evidence integrity

Run `make coverage`, or `./scripts/with-toolchain.sh python3 scripts/go-coverage.py`. The wrapper runs all original Go/unit/stress tests with atomic cross-package instrumentation, retained GTK prompt regressions, native GTK adapter tests on a private Xvfb, targeted portable memory tests, and a **separate instrumented native executable** through the real disposable-profile GUI driver. It preserves all original local-node/emulator gates and records every explicit skip.

The current Wails campaign requires six frozen raw profiles: `go-base.cover`,
`go-native.cover`, `go-nativeui.cover`, `go-native-runtime.cover`,
`go-portable-memory.cover` and `go-webui-diagnostic.cover`. The base suite keeps
the original production/WebKit tags, measuring the shipped restricted
environment helper; the additional diagnostic profile tests the separately
tagged collector and directory guards. Historical non-Wails fixtures retain
their original five-profile requirement. Identical source blocks share one
statement denominator and use maximum observed counts. The verifier requires
the exact profiles/hashes and reconstructs the union. Missing profiles, altered
counters, changed source or below-floor packages fail. Portable memory tests
exercise actual kernel failures and preserved loopback access without claiming
an installed syscall sandbox. The base suite retains real legacy seccomp tests.

Source binding includes every first-party Go/test/module file, C/header bindings and the coverage/private-display drivers. A before/after snapshot rejects concurrent edits. Only the exact tagged `internal/secureui/automation.go`, `internal/secureui/recovery_automation.go` and `internal/nativeui/automation.go` test injectors and independent nonshipped oracle generators are excluded. No shipped path is excluded. Third-party library and native GTK/C statements are not measured by Go.

`reports/go-coverage.json` records the stable source snapshot, all suites, profiles, original gates and skipped tests. `reports/coverage.json` contains per-package floors. `reports/go-uncovered.json` retains every unexecuted block's exact source excerpt; `reports/go-functions.txt` and `reports/go.html` provide function/source views. These local generated reports are not release assets. Source or binary changes require affected checks to run again.

`python3 scripts/coverage-test.py` passes twelve integrity tests.
`python3 scripts/release-evidence-test.py` passes 45 verifier regressions,
including source/driver/C changes, forged counters, missing profiles,
lowered/missing original gates, missing private-process measurements, changed
React assets, and reclassified portable-memory/native-runtime evidence.

## Historical GTK campaigns and limits

| State | Campaign |
| --- | --- |
| PASS | All base Go packages; skipped individual integration/soak tests remain separately listed below. |
| PASS | Retained private-X11 GTK prompt suite: 14 tests; its optional five-minute campaign is NOT_RUN. This legacy prompt package is not the new UI. |
| PASS | Native GTK adapter: seven race-enabled tests, including invalid/bounded public trees, unavailable display, dialog cancellation, closed/duplicate private requests and temporary-word cleanup. |
| PASS | Portable memory: all three required parent tests, including real traced kernel-failure subtests, run without skips. |
| PASS | Instrumented native GUI: all nine setup, wallet/copy, Receive QR, Send presentation, authenticated backup, independent phrase restore and locked-restart scenarios; sourceStable=true. This executable is distinct from the uninstrumented candidate. |
| PASS, separate candidate | `reports/native-wallet.json`: ordinary uninstrumented candidate passes the same nine GUI scenarios. Generated secrets remain in disposable memory; no real wallet or mainnet transfer is used. |
| PASS | Original TON V5R1 emulator and local USDT signed-transaction execution gates. |
| BLOCKED | Original Decred simnet gate: pinned dcrd executable unavailable. |
| NOT_RUN | Clean Ubuntu 24.04/26.04 X11/Wayland matrix, separate physical machines, real OS lock/suspend acceptance, public funded testnets, live Bybit operations, full 24-hour soak and independent security/license review. |

The GUI runs in an owned isolated user/mount/network namespace. Pinned RPC hostnames resolve to unreachable test-only loopback addresses; no RPC service or mocked response is supplied, and host `/etc` is untouched. This makes RPC failures deterministic and fast; it does not establish public RPC acceptance. Synthetic controller replies validate dispatch/state contracts only. The separately reported private-geth transfer is actual headless protocol integration, not graphical Send/approval acceptance. A current native production-evidence verifier and a combined native GUI/local-node Send campaign remain unfinished; the old Electron verifier cannot certify this build.

The final measurement binds **226** source/test/module/driver files and takes **318.128 seconds**. Earlier source-changed attempts remain FAIL under `reports/coverage-source-changed-first/` and `reports/coverage-source-changed-second/`; their individually passing scenarios are not current acceptance.

| Explicit base-test skip | State | Reason |
| --- | --- | --- |
| `cmd/wallet-launcher.TestTP041BootstrapWorker` | NOT_RUN | Explicit test skip; inspect go-test.jsonl for prerequisites |
| `internal/chains/aptos.TestLocalNode` | NOT_RUN | aptos_test.go:263: NOT_RUN: pinned local validator required |
| `internal/chains/aptos.TestLocalNodeRotation` | NOT_RUN | aptos_test.go:392: NOT_RUN: independent TS SDK rotation setup required |
| `internal/chains/aptos.TestLocalNodeCommittedFailure` | NOT_RUN | aptos_test.go:462: NOT_RUN: local validator required |
| `internal/chains/decred.TestDcrdSimnet` | NOT_RUN | decred_test.go:305: NOT_RUN: WALLET_DCRD must point to pinned dcrd v1.10.9 executable |
| `internal/chains/near.TestNEARPublicBalanceReadOnly` | NOT_RUN | balance_test.go:493: NOT_RUN: explicit public read-only NEAR balance probe not requested |
| `internal/chains/evm.TestTP050LocalEVMProtocolAcceptance` | NOT_RUN | integration_test.go:44: NOT_RUN: set LOCALWALLET_EVM_NODE to explicit loopback test node |
| `internal/chains/near.TestLocalNode` | NOT_RUN | near_test.go:233: NOT_RUN: pinned sandbox harness required |
| `tests/stress.TestTP072Soak` | NOT_RUN | stress_test.go:250: NOT_RUN: invoke make test-soak for explicit 24-hour campaign |

The launcher bootstrap-worker skip is its unused parent role outside the dedicated worker subprocess. Optional Aptos/NEAR/public-balance/private-geth integration tests require their named harnesses or explicit opt-in; separate reports retain their own scope. The 24-hour soak and five-minute native typing campaigns are explicit runs and are never inferred from faster stress or UI tests.

## Historical GTK package floors

Legacy launchers/helpers and the retained GTK prompt package are measured for regressions; they do not ship in the default one-file archive. `internal/buildinfo`, `internal/model` and embedded redistribution text have no executable statement denominator, so no percentage is invented for them.

| Package | Covered/total statements | Coverage | Floor | State |
| --- | --- | --- | --- | --- |
| `cmd/cryptowallet` | 37/38 | 97.37% | 90% | PASS |
| `cmd/wallet-core` | 1/1 | 100.00% | 95% | PASS |
| `cmd/wallet-launcher` | 147/156 | 94.23% | 90% | PASS |
| `cmd/wallet-signer` | 1/1 | 100.00% | 95% | PASS |
| `internal/amount` | 45/47 | 95.74% | 95% | PASS |
| `internal/chains` | 1/1 | 100.00% | 95% | PASS |
| `internal/chains/aptos` | 453/469 | 96.59% | 95% | PASS |
| `internal/chains/chainutil` | 93/94 | 98.94% | 95% | PASS |
| `internal/chains/decred` | 787/819 | 96.09% | 95% | PASS |
| `internal/chains/evm` | 793/820 | 96.71% | 95% | PASS |
| `internal/chains/hypercore` | 636/656 | 96.95% | 95% | PASS |
| `internal/chains/near` | 447/456 | 98.03% | 95% | PASS |
| `internal/chains/ton` | 694/721 | 96.26% | 95% | PASS |
| `internal/derivation` | 74/76 | 97.37% | 95% | PASS |
| `internal/exchange` | 30/30 | 100.00% | 95% | PASS |
| `internal/exchange/bybit` | 429/431 | 99.54% | 95% | PASS |
| `internal/ipc` | 49/49 | 100.00% | 95% | PASS |
| `internal/isolation` | 20/20 | 100.00% | 95% | PASS |
| `internal/journal` | 16/16 | 100.00% | 95% | PASS |
| `internal/nativeui` | 370/387 | 95.61% | 95% | PASS |
| `internal/nativewallet` | 810/838 | 96.66% | 95% | PASS |
| `internal/qr` | 26/27 | 96.30% | 95% | PASS |
| `internal/secureui` | 213/216 | 98.61% | 95% | PASS |
| `internal/sessionwatch` | 116/121 | 95.87% | 95% | PASS |
| `internal/signer` | 992/1024 | 96.88% | 95% | PASS |
| `internal/strictjson` | 45/46 | 97.83% | 95% | PASS |
| `internal/supervisor` | 231/237 | 97.47% | 95% | PASS |
| `internal/uiprompt` | 156/164 | 95.12% | 95% | PASS |
| `internal/vault` | 284/293 | 96.93% | 95% | PASS |
| `internal/walletcore` | 1355/1400 | 96.79% | 95% | PASS |
| `internal/walletsigner` | 232/238 | 97.48% | 95% | PASS |

## Historical GTK remaining-path review

Unexecuted counters remain unexecuted even if nearby tests pass. The table below indexes every remaining production block; full spans and excerpts are in `reports/go-uncovered.json`. Conservative validation, cryptographic-library errors, serialization/storage failures, cancellation/lifecycle races, native callback boundaries and RPC rejection paths retain their guards. No guard is removed or declared impossible merely to improve coverage.

The CLI's remaining failure path is inspected without exposing raw internal errors. Native UI/controller/session-monitoring counters require both negative-state tests and actual event-loop scenarios; C allocation/widget behavior still needs native runtime testing and independent review. Core/signer/chain uncovered failures require their specific fault or node harness, rather than fabricated transactions. Retained legacy component paths remain regression scope only. Changes to these areas must rerun meaningful scenarios and review new outcomes before release.

There are **277 unexecuted blocks**, containing **309 statements**, across **45 files**. Their inclusion is deliberate; the floor permits some unexecuted code, not an audit exemption.

| Source | Unexecuted block start line.column | Statements |
| --- | --- | --- |
| [cmd/cryptowallet/main.go](../cmd/cryptowallet/main.go) | `35.3` | 1 |
| [cmd/wallet-launcher/main.go](../cmd/wallet-launcher/main.go) | `108.4`, `119.3`, `181.3`, `31.15`, `39.3`, `48.3`, `92.5` | 7 |
| [cmd/wallet-launcher/stderr_process.go](../cmd/wallet-launcher/stderr_process.go) | `22.3`, `55.3` | 2 |
| [internal/amount/amount.go](../internal/amount/amount.go) | `47.4`, `55.3` | 2 |
| [internal/chains/aptos/aptos.go](../internal/chains/aptos/aptos.go) | `338.3`, `415.3`, `420.3`, `424.3`, `439.3`, `452.3`, `456.3`, `514.3`, `518.3`, `523.3`, `527.3`, `549.3`, `560.3` | 13 |
| [internal/chains/aptos/history.go](../internal/chains/aptos/history.go) | `67.3`, `70.3`, `74.3` | 3 |
| [internal/chains/chainutil/util.go](../internal/chains/chainutil/util.go) | `135.3` | 1 |
| [internal/chains/decred/decred.go](../internal/chains/decred/decred.go) | `153.114`, `179.3`, `245.3`, `255.4`, `272.3`, `278.3`, `296.3`, `309.3`, `329.3`, `334.3`, `599.4`, `637.3`, `642.4`, `647.4`, `653.4`, `659.4`, `664.3`, `668.3`, `703.3` | 19 |
| [internal/chains/decred/sync.go](../internal/chains/decred/sync.go) | `143.3`, `151.5`, `155.5`, `186.6`, `210.5`, `301.3`, `316.4`, `326.5`, `363.2`, `421.3`, `439.4`, `451.5` | 13 |
| [internal/chains/evm/adapter.go](../internal/chains/evm/adapter.go) | `308.3`, `321.3`, `329.4`, `345.3`, `478.3`, `485.3`, `493.3`, `497.3`, `501.3`, `527.3`, `66.3` | 11 |
| [internal/chains/evm/history.go](../internal/chains/evm/history.go) | `121.4`, `163.5`, `197.5`, `219.6`, `243.6`, `90.4` | 6 |
| [internal/chains/evm/keys.go](../internal/chains/evm/keys.go) | `54.3`, `67.5`, `79.4`, `87.4` | 7 |
| [internal/chains/evm/rpc.go](../internal/chains/evm/rpc.go) | `119.3`, `164.3`, `83.5` | 3 |
| [internal/chains/hypercore/adapter.go](../internal/chains/hypercore/adapter.go) | `131.4`, `143.3`, `429.3`, `471.3`, `496.3`, `503.3`, `514.3`, `518.3`, `526.3`, `548.3` | 10 |
| [internal/chains/hypercore/history.go](../internal/chains/hypercore/history.go) | `108.4`, `143.3` | 2 |
| [internal/chains/hypercore/keys.go](../internal/chains/hypercore/keys.go) | `39.3`, `49.3`, `62.5`, `74.4`, `82.4` | 8 |
| [internal/chains/near/balance.go](../internal/chains/near/balance.go) | `18.48`, `19.48`, `31.3`, `94.3` | 4 |
| [internal/chains/near/history.go](../internal/chains/near/history.go) | `101.6`, `56.3` | 2 |
| [internal/chains/near/near.go](../internal/chains/near/near.go) | `507.3`, `512.3`, `526.3` | 3 |
| [internal/chains/ton/history.go](../internal/chains/ton/history.go) | `63.4`, `64.5` | 2 |
| [internal/chains/ton/network.go](../internal/chains/ton/network.go) | `114.3`, `163.4`, `314.4`, `358.3`, `362.3`, `373.3`, `377.3`, `431.4`, `436.4`, `443.4` | 10 |
| [internal/chains/ton/pacing.go](../internal/chains/ton/pacing.go) | `39.3` | 1 |
| [internal/chains/ton/ton.go](../internal/chains/ton/ton.go) | `136.3`, `321.3`, `339.3`, `345.4`, `411.3`, `415.3`, `420.3`, `434.3`, `441.3`, `445.3`, `452.3`, `456.3`, `461.3`, `468.3` | 14 |
| [internal/derivation/derivation.go](../internal/derivation/derivation.go) | `118.3`, `98.4` | 2 |
| [internal/exchange/bybit/client.go](../internal/exchange/bybit/client.go) | `89.3` | 1 |
| [internal/exchange/bybit/fees.go](../internal/exchange/bybit/fees.go) | `100.3` | 1 |
| [internal/nativeui/native.go](../internal/nativeui/native.go) | `162.22`, `374.3`, `400.5`, `407.5`, `423.3`, `439.4`, `522.4`, `546.5`, `646.3`, `86.3` | 17 |
| [internal/nativewallet/app.go](../internal/nativewallet/app.go) | `1004.4`, `1241.4`, `1247.4`, `1251.3`, `127.5`, `153.5`, `155.6`, `156.7`, `163.5`, `165.6`, `166.7`, `181.5`, `186.4`, `195.3`, `463.4`, `681.3`, `725.3`, `731.4`, `734.4`, `903.3` | 28 |
| [internal/qr/qr.go](../internal/qr/qr.go) | `32.3` | 1 |
| [internal/secureui/native.go](../internal/secureui/native.go) | `109.3`, `161.4`, `218.3` | 3 |
| [internal/sessionwatch/watch.go](../internal/sessionwatch/watch.go) | `103.3`, `130.4`, `149.3`, `172.4` | 5 |
| [internal/signer/engine.go](../internal/signer/engine.go) | `105.3`, `154.3`, `158.3`, `168.4`, `173.4`, `229.4`, `569.3`, `630.3`, `679.4`, `69.3`, `763.5`, `827.3`, `874.3`, `943.3` | 14 |
| [internal/signer/engine_public.go](../internal/signer/engine_public.go) | `68.3` | 2 |
| [internal/signer/exchange.go](../internal/signer/exchange.go) | `149.3`, `182.3`, `280.4`, `285.5`, `300.3`, `368.3`, `384.3`, `388.3`, `393.4`, `404.5`, `410.3`, `415.4`, `462.4`, `473.4`, `490.3`, `80.3` | 16 |
| [internal/strictjson/json.go](../internal/strictjson/json.go) | `69.3` | 1 |
| [internal/supervisor/child.go](../internal/supervisor/child.go) | `131.3`, `162.3`, `166.3`, `178.3` | 6 |
| [internal/uiprompt/prompt.go](../internal/uiprompt/prompt.go) | `163.3`, `196.3`, `202.3`, `214.3`, `48.3`, `55.3`, `59.3`, `88.3` | 8 |
| [internal/vault/backup.go](../internal/vault/backup.go) | `65.3` | 1 |
| [internal/vault/envelope.go](../internal/vault/envelope.go) | `60.3`, `79.3` | 2 |
| [internal/vault/files.go](../internal/vault/files.go) | `103.3`, `122.3`, `143.3`, `149.3`, `159.3`, `165.3` | 6 |
| [internal/walletcore/exchange.go](../internal/walletcore/exchange.go) | `1074.6`, `1112.5`, `523.3`, `530.3`, `561.4`, `577.3`, `690.3`, `748.3`, `911.4`, `933.6` | 10 |
| [internal/walletcore/history.go](../internal/walletcore/history.go) | `162.4`, `184.3`, `59.4`, `67.3`, `75.4`, `77.3` | 8 |
| [internal/walletcore/main.go](../internal/walletcore/main.go) | `223.4`, `224.5`, `226.4`, `267.23`, `280.3`, `287.3`, `291.3`, `309.5`, `328.5`, `352.5`, `358.4`, `378.5`, `518.4`, `520.4`, `524.3`, `525.4`, `527.3`, `530.4`, `532.3`, `551.4`, `613.4`, `619.4`, `851.5`, `922.3` | 26 |
| [internal/walletcore/public_pages.go](../internal/walletcore/public_pages.go) | `48.3` | 1 |
| [internal/walletsigner/main.go](../internal/walletsigner/main.go) | `52.3`, `56.3`, `79.6`, `85.6` | 6 |
