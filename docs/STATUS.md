# Implementation and release evidence

## Storage directory rename — 2026-10-28

| State | Evidence and limits |
| --- | --- |
| PASS, migration/security units | Shared `cryptowallet` path for coordinator, preferences and Settings. Atomic no-replace whole-root migration preserves both profiles, current/previous/interrupted ciphertext, permissions/inodes and public preferences. Tests authenticate moved ciphertext, reject busy writers on either network, simultaneous roots, unsafe links/files/modes and relative configuration, and exercise 20 concurrent migrations. Startup conflict stops before preferences/backend creation; public diagnostics redact private error details. |
| PASS, current units/race/build | `make test-ci`, focused race checks across migration/core/controller/Wails/CLI, checkout-input regressions and a fresh Go build/extraction/package pass. All unchanged secret-handling, derivation and chain policies remain. First draft tests exposed an overly restrictive check on ordinary Ubuntu configuration parents (0775); compatibility is retained while wallet roots/profiles remain owner-only and no permissions are rewritten. |
| PASS, actual portable migration/recovery | Seven headless steps on the built executable: disposable generated legacy layout migrates with exact ciphertext/preferences before unlock, then unlocks all 24 unchanged descriptors across eight networks at indices 0, 1 and 127. Separate installations restore the same descriptors from the phrase with the original or a different local password. Public IPC/files/logs pass secret-canary checks. `reports/portable-phrase-recovery.json`. |
| PASS, commit guards | Destination collisions introduced immediately before the rename, including empty directories, are never overwritten. Replaced source inodes reject. Three copied-package semantic mutants (no-replace, source identity and writer lock) are killed by their exact assertions after passing controls; production sources are unchanged by the campaign. `reports/storage-migration-mutants.json`. |
| PASS, actual desktop and installation | All 22 actual Wails steps pass with stable sources: generated-root setup, all eight wallets, token controls, new Settings storage path, authenticated backup, appearance persistence, fresh-profile restore and Enter unlock. Actual per-user install/uninstall preserves synthetic profile data. Native WebKit security and inspector rejection pass. `reports/wails-wallet.json`, `reports/portable-install.json`, `reports/wails-security.json`, `reports/wails-inspector.json`. Installation does not launch the application or migrate real user data. |
| PASS, actual local-chain payment | Eight source-stable checks execute the real Go signer/payment pipeline against isolated genesis-funded geth, verify the new testnet vault location, exact receipt/balances/fee/nonce and reject public RPC/secret canary leaks. No mainnet funds. `reports/portable-send.json`. |
| NOT_RUN | New hosted workflow, clean OS/Wayland upgrade matrix and independent review. Existing funded-network acceptance gates are unchanged; no real profiles/secrets or mainnet funds are used. |

See [storage directory and conflict recovery](STORAGE_DIRECTORY.md).

## Window layout, storage location and desktop integration — 2026-10-28

| State | Evidence and limits |
| --- | --- |
| PASS, current units and race | `make test-ci`: current Go/frontend/tool checks pass, including all 55 React tests. Focused controller/Wails/CLI race checks pass. Window sizing covers monitor bounds, all supported recovery word counts and fonts, unchanged private prompt authority, and exact mainnet/testnet and HOME/XDG storage-path display without opening storage. |
| PASS, layout supplement | Thirteen actual system WebKit rendering cases with compiled React and public fixtures/mock bridges: eight-wallet overview and default 18px 12/24-word backups fit tested normal viewports. At 24px with 24 words and a 1080px monitor cap, 38px of safe body scrolling remains necessary; constrained viewports keep overflow and footer access. This is layout evidence, not a real-wallet authorization/recovery campaign. `reports/window-layout.json`. |
| PASS, original icons | SVG and matching 512px desktop/128px embedded PNGs regenerate exactly; independent PNG decoding checks dimensions/transparency. The actual X11 window's icon property matches the embedded ARGB pixels exactly. No external icon or brand copied. `python3 scripts/app-icons.py --check`. |
| PASS, installer and package | Exact four-file archive hashes/modes and real extracted executable diagnostics. Thirty-seven current build/tool tests, including 13 installer cases, unsafe destination/ancestor refusal and atomic rollback. Ten actual-binary disposable installation checks cover menu/icons, GIO diagnostic execution, upgrade/retry, download-directory removal and uninstall preserving a synthetic profile. `reports/portable-install.json`. |
| FAIL, corrected startup diagnostic | First actual run exposed a main window origin above the monitor after startup height clamping. Centering after GTK allocation corrects it. A separate exact-icon test decoded Xlib's sign-extended native longs incorrectly; decoding their specified low 32-bit CARDINAL values preserves exact ARGB identity. Failed reports remain separate from later passes. |
| PASS, actual 24-word backup | Final real Go application: eight rows without vertical wheel movement, main window at `[80,32,1440,1036]`, exact embedded icon, Settings storage label/profile suffix and existing disposable vault. Password-authenticated 24-word backup at `[340,60,920,980]` shows every numbered word and Copy phrase initially, stays within the display, does not scroll and copies only after the private click. Four steps pass with stable sources. `reports/wails-recovery-layout.json`. |
| FAIL, first complete GUI attempt | Eight steps pass through generated-root setup, overview, staking, table copy and ETH/USDT metadata; the subsequent USDT Send observation exceeds its per-operation deadline. A separate targeted run and guarded public diagnostic establish that OCR missed the real selector and clicked its repeated balance symbol while ETH stayed selected. Later steps are not passed by those runs. `reports/desktop-layout-first-full-failed-wails-wallet.json`. |
| PASS, repaired selector observation | Exact contrast-decoded symbol must lie inside an observed bounded enabled button, left of its independently matched balance. Wrong glyphs, balance-only candidates, disabled controls and unbounded fills reject. All 71 current helper tests and the actual targeted token campaign pass; exact contract/units, owned address and no-payment/authority checks remain. No production asset code or timeout changes. |
| PASS, complete actual application | All 22 steps pass with stable sources and the final binary: generated phrase, overview, all eight wallets, NEAR staking, USDT selection, Receive/Send, Settings storage, authenticated 12-word no-scroll backup/copy, Appearance/Bybit geometry, independent fresh-profile phrase restore with all eight exact addresses, restart and Enter unlock. `reports/wails-wallet.json`. Separate final 24-word checks pass. |
| PASS, native security and build | Actual ephemeral WebKit/network/storage restrictions and production inspector rejection pass with stable sources. Fresh Go build/extraction, embedded notices, exact four-file archive, root installation and pinned workflow lint pass. No Electron/Debian build tools run. `reports/wails-security.json`, `reports/wails-inspector.json`, `reports/go-only-build.json`. |
| NOT_RUN at commit checkpoint | New hosted workflow, clean supported OS/Wayland/monitor matrix, public funded acceptance and independent review. No real profile or mainnet funds are used. |

## Hosted native-security display startup follow-up — 2026-10-28

| State | Evidence and limits |
| --- | --- |
| FAIL, hosted at `44a8c69` | [Run 40701103846](https://github.com/neodix42/crypto-wallet/actions/runs/40701103846) passes archive, controller/security and independent binary recovery checks, then the native WebKit policy probe's private X11 display never signals readiness. Native policy runtime, inspector and complete wallet GUI steps are not passed. No display stderr was retained, so the historical report cannot establish the precise cause. |
| PASS, separate hosted release | The same commit's build and publication workflow succeeds. That does not pass its failed test workflow. |
| PASS, complete hosted checks job | Its checks job passes current units, mutation/security, the complete two-phase race command, interoperability, all local-chain integrations, fuzzing, isolated EVM transfers, dependency review and headless recovery/crash checks. The overall run remains failed because of the separate Wails startup. |
| PASS, controlled diagnosis | The unchanged full driver reproduces the readiness timeout when public diagnostics exceed its stderr-pipe capacity. This demonstrates a real harness deadlock mechanism, without establishing the lost historical stderr. Shared display-file offsets alone did not reproduce failure. The failed reproduction is retained separately. |
| PASS, repaired actual security probe | Frozen normal and noisy runs execute all eight checks with identical native policy/clipboard producers; 270,468 startup diagnostic bytes no longer block readiness. Both complete an owned X11 setup handshake within 0.08 seconds, preserve the five-second deadline, receive zero witness HTTP requests and clean their display processes. `reports/wails-security-stderr-deadlock-after.json`. |
| PASS, negative control and regressions | Controlled early exit remains FAIL with exit 23, a bounded 4 KiB diagnostic tail and policy/clipboard explicitly NOT_RUN. Sixty-five current helper regressions cover announcement framing, owned PID/UID, delayed fragmented setup, bounds, descriptor closure and stubborn descendant cleanup. |
| PASS, complete local security target | `make test-wails-security`: pinned fork verification, actual Go policy tests, native WebKit/clipboard policy and actual app inspector rejection all pass with stable sources. No production C/Go policy or wallet driver changes. The prior full 20-step GUI evidence still binds the unchanged application and driver. |
| NOT_RUN at commit checkpoint | New hosted execution of this startup correction. Public funded operations, clean supported OS matrix and independent security review retain their separate gates. |

## Hosted table-copy and race-stress follow-up — 2026-10-28

| State | Evidence and limits |
| --- | --- |
| PASS, hosted NEAR checks at `ece6768` | [Run 40695614368](https://github.com/neodix42/crypto-wallet/actions/runs/40695614368) recognizes the exact 25-byte public contract at native resolution and passes every staking assertion. This establishes the prior OCR repair on the hosted runner. |
| FAIL, hosted GUI | The next main-table copy step receives no verified dialog. Its fixed window-edge click did not observe the actual button. The evidence cannot establish a product defect or successful copy; subsequent GUI steps are skipped. |
| PASS, diagnosis only | Actual guarded public pixels at the hosted window geometry show a partly clipped button. The first whole-button locator probe rejects it; that failed probe remains failed. A bounded horizontal table reveal must precede measurement. No private pixels are retained or public-network funds used. |
| FAIL, hosted race stress | The same run completes its functional stress workloads without a reported data race but measures signer-lifecycle p99 at 60.440580 seconds, above the unchanged 60-second budget. The stress and signer packages compete under two-package race execution. Subsequent checks-job steps are skipped. |
| PASS, local race isolation | The full uncached race stress package passes under two-CPU affinity: lifecycle p99 28.834226 seconds against 60 seconds, production-KDF p99 2.470457 seconds against 15 seconds, all 1,000 signed fake-ledger payments reconciled and no unresolved payments. Existing workload/resource limits and source files are unchanged. Seven routing regressions require every package exactly once, sequential execution, fresh stress measurement and both phases even after failure. `reports/ci-race-stress-two-cpu.json`. |
| PASS, focused actual GUI | Final Ubuntu 24 OCR run at the hosted window geometry uses one ordinary horizontal table reveal and one measured-button click, copies the exact independent fixture address and keeps the account overview open. Stable sources. The failure crop is only a public row region, including an address suffix; it contains no private window pixels. `reports/wails-main-row-copy-final-ubuntu24.json`. |
| PASS, local current checks | `make test-ci`, 52 current-interface helper regressions on both OCR versions, all 67 local helper regressions, 23 CI routing/privacy checks, pinned workflow lint and root specification checksums. Narrow public-row failure artifacts remain guard-checked; no general screenshot glob is uploaded. |
| PASS, complete actual local GUI | Final `make test-wails-wallet` with Ubuntu 24 OCR passes all 20 steps with stable sources: generated phrase, NEAR staking, measured table copy matching the independently opened Ethereum wallet, all eight addresses, token controls, authenticated backup/copy, Appearance/Bybit geometry, fresh independent phrase restore and Enter unlock. `reports/wails-wallet.json`. Local rendering is not a clean Ubuntu 24 OS matrix. |
| NOT_RUN at commit checkpoint | Next hosted execution. Separate public funded operations, clean supported OS matrix and independent review remain outstanding. |

## Hosted contract-text observation follow-up — 2026-10-28

| State | Evidence and limits |
| --- | --- |
| PASS, hosted checks job at `8e677a2` | [Run 40690429440](https://github.com/neodix42/crypto-wallet/actions/runs/40690429440) passes the complete checks job: current units/helpers, mutation/security, race, interoperability, real local integrations, fuzz, isolated EVM transfers, dependency review and headless recovery/crash checks. Its separate GUI job fails; the overall run is not green. |
| FAIL, hosted GUI | Precise diagnostics show correct NEAR wallet, first-attempt navigation and AstroStakers, but full contract OCR remains false. Sixteen repeated tab clicks do not fix that recognition error. The overall GUI campaign remains failed. |
| PASS, diagnosis only | Ubuntu 24/Tesseract 5.3.4 and DejaVu Sans Mono reproduce `poolv1` → `poolvl` on synthetic public text. Earlier sans-matched local GUI probes did not preserve the mono fallback and therefore did not establish hosted equivalence. See [the corrected diagnosis](CI_DESKTOP.md). |
| PASS, exact decoder | Both OCR versions reproduce the raw public-crop misread and read the exact 25 contract bytes after anchored segmentation/padding. Both reject the committed synthetic wrong-`l`/wrong-`I` image fixtures; 72 additional synthetic near-misses reject. No whitelist, address rewriting or reduced identity check. Genuine mono-font false negatives remain failures. |
| PASS, local final tests | `make test-ci`, all `make test-security-ci` checks, 35 current-interface regressions on both OCR versions, 50 complete helper regressions, 23 CI routing/privacy-artifact checks and pinned workflow lint. All 24 general, five Bybit and five Exchange mutants are killed with current stable sources. |
| PASS, full local GUI with Ubuntu 24 OCR | `make test-wails-wallet`: all 20 steps and stable sources using extracted official Tesseract 5.3.4. Focused five-step campaigns also pass under 5.3.4 and 5.5. This is local system rendering, not a clean Ubuntu 24 desktop matrix. |
| NOT_RUN at commit checkpoint | New hosted execution of the contract-crop correction. The local full GUI and raw fixture results do not certify hosted rendering, public funded operations or independent review. |

## Hosted mutation/navigation repair — 2026-10-28

The failed [run 40683603591](https://github.com/neodix42/crypto-wallet/actions/runs/40683603591) is retained as a failure: four mutation campaigns timed out and the Wails navigation observer did not reach its NEAR Staking assertions. See [the diagnosis and repair scope](CI_DESKTOP.md). No wallet security policy, accepted network, production timeout or release authentication check is changed by this repair.

| State | Evidence and limits |
| --- | --- |
| FAIL, hosted at `59e063f` | Current unit/frontend/helper checks and Go security units pass; 20 general mutants are killed and four are INVALID at 120 seconds. Wails startup/phrase setup passes, then the coarse NEAR-navigation stage times out. Later job steps are skipped, not passed. |
| PASS, diagnosis only | Unmodified signer package takes 68.348 seconds locally. A two-CPU broad-mutant diagnostic observes the intended assertion, then expires in unrelated receive/QR work; that diagnostic is not a passing security campaign. Original-source targeted staking and a CI-like font probe reach the real page. The original hosted GUI timeout remains unreproduced. |
| PASS, repaired security | All 24 general mutants killed in 95.868 seconds with stable sources. Four affected campaigns also pass under two-CPU affinity in 42.950 seconds, with all six required controls and exact failures. Five Bybit and five Exchange mutants, Go security packages and the current Go/React surface checks pass. `reports/ci-security-validation.json`. |
| PASS, regressions | `make test-ci`: current Go packages, all 55 React tests, 11 mutation-driver and 27 current Wails startup/navigation regressions. Twelve build-input regressions, 23 namespace/routing checks, all three pinned workflow lints and independent read-only review pass. |
| PASS, targeted GUI | Final actual CI-font staking: five steps, stable sources, complete validator, unknown real offline state and inert disabled withdrawal. An intermediate overly strict heading detector failed and was corrected using guarded public observations; its failed report is retained separately. `reports/wails-staking-ci-fonts-fixed.json`. |
| PASS, complete local GUI | Final `make test-wails-wallet`: all 20 steps, stable sources, generated-root setup, real NEAR Staking, all eight addresses, ETH/USDT controls, authenticated phrase backup/copy, Appearance and private Bybit geometry, fresh independent phrase restore and Enter unlock. `reports/wails-wallet.json`. No real user profile or public-network funds are used. |
| NOT_RUN at commit checkpoint | Revised hosted execution follows commit/push. Funded public-network/Bybit operations, the full OS matrix and independent review remain separate. |

## Hosted test scope update — 2026-10-28

The owner removed legacy Electron tests and Go/frontend coverage measurement/enforcement from GitHub jobs. Explicit CI targets retain current Go/Wails/React tests and shared security/recovery protocols while full local targets and coverage thresholds remain available. No application code, network support, signing policy or release publication check is relaxed. See [the CI scope and historical results](CI_DESKTOP.md).

| State | Evidence and limits |
| --- | --- |
| PASS, local current targets | `make test-ci`, `make test-security-ci`, `make test-race-ci` and `make check-deps-ci`. Dynamic Go discovery retains 37 packages, excluding only the legacy Electron launcher. All 55 current React tests pass; security runs kill all 24 general, five Bybit transport and five Exchange authorization mutants. Module verification, vet, reachable-vulnerability checks and current frontend/provenance npm audits pass. |
| PASS, scoped tooling | Pinned actionlint v1.7.12 accepts all three workflows; 12 build-input, 23 CI namespace/routing, three Go package-selection and three surface-routing regressions pass. Current helper modes run 22 build, 16 Wails startup and 12 notice tests. |
| PASS, local retention | Full default helper modes: 58 build, 31 startup and 15 notice tests; the default legacy surface check still builds/tests its 163 Electron cases locally. Local coverage collectors/thresholds and explicit legacy GUI commands remain unchanged and resolve in dry-runs. No new full graphical or coverage campaign is claimed. |
| PASS, read-only audit | All three workflows and their expanded command graph contain no legacy Electron test execution or Go/frontend coverage measurement. Current Wails GUI isolation/cleanup, chain integrations, shared recovery/crash security and release provenance checks remain. Local aggregate: `reports/ci-current-scope.json`; command logs are retained beside it. |
| NOT_RUN | Hosted execution of the revised workflow follows push. Prior coverage or legacy Electron results remain tied to their original source commits. |
| NOT_RUN, outside this change | New coverage, graphical acceptance, funded network/Bybit tests, full OS matrix and independent review. Removing a hosted gate records no passing evidence for it. |

## Appearance, unlock, Bybit and Base update — 2026-10-28

The owner request adds Light/Dark and public font preferences (Light/18px by default), distinct selected Settings tabs, Enter-based unlock, a larger Bybit credential prompt and a Connect-only exception to repeated password entry. It explicitly removes active Horizen support while retaining authenticated historical encrypted records and every other family. Base retains canonical ZEN and gains exact-contract ERC-20 import plus bounded read-only candidate discovery. See [APPEARANCE.md](APPEARANCE.md) and [ERC20.md](ERC20.md).

| State | Evidence and limits |
| --- | --- |
| PASS, prior immutable CI | [run 40667566633](https://github.com/neodix42/crypto-wallet/actions/runs/40667566633) on e51d21e passed both jobs, including coverage, actual Wails workflows and the repaired legacy Unlock timer observer. This does not certify the new feature changes. |
| PASS, scoped local | Frontend appearance/protocol suites with unchanged per-file floors; Go public preference safety, private prompt wire serialization, current Connect consent/session/persistence guards, Base candidate authorization separation and retired-vault compatibility under race detection. |
| PASS, scoped real execution | Two actual standard-token contracts with 6/18 decimals on each of Ethereum/Base Sepolia-shaped owned local EVMs: 8 signed transfers, exactly 12 mined transactions including 4 deployments; independent receipt/event/balance/nonce/native-gas checks. Base fee oracle is a deterministic fixture, not actual rollup economics. |
| BLOCKED, live provider | Base Blockscout candidate-inventory request returned HTTP403 in this environment. Controlled indexer/RPC tests pass; live inventory completeness/availability is not claimed. Exact-contract import is independent. |
| PASS, actual local GUI | Final normal and instrumented Wails campaigns: 20 steps each, unchanged source, all eight addresses restored in a fresh independent app copy using a different app password. Targeted Appearance also passes; actual private Bybit fields/footer at 24px and Enter unlock are verified. No Bybit credentials are committed or live requests made. |
| PASS, full local aggregate | `make test`, `make test-security`, all-package `go test -race -p 2 ./...`, interoperability, all eight real local integration gates, 18 actual bounded fuzz targets, vet, and six fresh-copy headless recovery checks comparing 24 descriptors. All 24 semantic mutants killed with unchanged source and every required passing control. |
| PASS, enforced coverage | `make coverage`: all 33 original package floors and four required local execution gates; both frontend campaigns pass. Controller 97.04%, Wails adapter 95.10%, EVM 96.81%. Full instrumented GUI passes 20 steps and every observed private process has real counters. Raw source/runtime bindings verify against the frozen files. |
| PASS, local artifact | The tested ordinary one-file Go binary is installed as repository-root `./cryptowallet`; existing profiles are not opened, replaced or migrated by installation. |
| NOT_RUN at this checkpoint | Hosted tests for this new feature commit follow push; the separate earlier e51 CI success is not promoted to a result for these changes. |
| NOT_RUN | Full clean supported X11/Wayland/monitor matrix, live/funded Bybit acceptance, public-network token roundtrips and independent security/legal review. No real user secret or automated mainnet funds are used. |

The actual Appearance drill caught a private prompt JSON marshaling defect: a promoted kind-specific marshaler omitted its sibling appearance field. Explicit serialization and all-kind one-use wire regressions fix it. Retained desktop registry assertions also needed the exact user-authorized 8 networks/10 assets on mainnet and 11 networks/12 assets on testnet; no other network or security threshold was removed. An intermediate mutation campaign was invalid because its input driver changed and one broad password test timed out under competing race instrumentation. The password mutant now requires an unmodified passing control and the exact wrong-reauthentication assertion; the final full 24-mutant run passes with unchanged source. Larger typography also required bounded real backup-body scrolling and guarded Light-theme OCR contrast handling in the GUI driver; exact control-state, phrase and clipboard comparisons remain mandatory.

## CI status timing observer — 2026-10-28

The remaining failure in hosted run
[40653531321](https://github.com/neodix42/crypto-wallet/actions/runs/40653531321)
was a legacy Unlock test that counted two OCR passes as message-expiry delay.
The Wails job and security/race/interoperability/integration/fuzz/coverage gates
passed; the later legacy payment regression was skipped after the observer
failure. See [CI_DESKTOP.md](CI_DESKTOP.md) for the exact diagnosis and captures.

The test now captures the guarded public status bar at 6.8/8.0 seconds before
decoding either image. Capture deadlines remain before 7.0/8.5 seconds, actual
visible/absent text is required, and private/unknown windows block pixel access.
The app's seven-second timer and production payloads are unchanged.

| State | Evidence and limits |
| --- | --- |
| PASS | 22 focused startup/timing regressions, seven existing OCR regressions, 45 release-evidence integrity checks, all 163 retained desktop tests, syntax/whitespace checks and independent read-only review. Exact 7,000 ms timer assertions remain enforced. |
| PASS, actual local UI | Normal and deliberately slowed real-Tesseract runs on an isolated X11 display and public fixture profile. Both verify pre-expiry visibility, absence by 8.5 seconds, geometry, no outline, unchanged vault, canaries and cleanup. Delayed decoding finishes at 15.032 seconds after valid 6.811/8.013-second captures. |
| PASS, scoped local execution | Previously skipped legacy payment check: seven actual private-geth receipt/balance/fee/nonce and canary assertions; no public-network funds. |
| NOT_RUN at this source checkpoint | Full revised hosted CI after commit/push. Earlier successful coverage and local scoped tests do not certify this follow-up's full workflow. |

## Multiple Ethereum ERC-20 assets — 2026-10-28

Ethereum mainnet/Sepolia wallets now show balances for every configured asset,
with exact contract/precision and per-token unknown states. Contract import
requires a private exact-policy confirmation and current password; the signer
persists the approved policy in the encrypted vault and restores it on unlock.
Native ETH fees, standard-transfer-only signing, canonical USDT/ZEN policies,
all existing networks and derivation identities remain intact. Imported assets
are excluded from unreviewed Bybit routes. See [ERC20.md](ERC20.md) for commands,
manual tracking/recovery behavior and issuer/proxy limitations.

| State | Evidence and limits |
| --- | --- |
| PASS | Actual two-token local EVM execution (6/18 decimals): four Go-signed transfers out/back, independent ethers calldata/receipt/log/balance/gas/nonce checks, exactly six transactions including two deployments; runtime/precision/false-result faults reject safely. |
| PASS | Private import authorization/cancellation, current password, stale session/generation, persistence failure, encrypted fresh-engine reload and exact signing; direct vault/child-handler malformed/null/duplicate/cross-profile/boundary tests. Explicit zero decimals is supported; missing/null precision is rejected. |
| PASS | Nine controller tests plus actual isolated Wails presentation: separate ETH/USDT rows, full contract, exact units and same verified owned receive address. Public RPC remains deliberately unavailable; graphical balance observations are unknown, not mocked successes. |
| PASS | Full local unit/security/race/interoperability/integration/fuzz/dependency commands. Twenty-two semantic mutants are killed, including four ERC-20 policies; all eighteen bounded fuzz targets execute mutations. Scope-specific reports retain their own source snapshots. |
| PASS | Rebuilt/installed ordinary one-file Go binary; actual ephemeral WebKit/inspector restrictions and fresh-copy headless recovery comparing 27 canonical descriptors on nine networks. Installation opens no real profile. |
| PASS | Final combined source-stable coverage: all 33 package floors and all four required local execution gates. Wails adapter 95.43%, controller 96.87%, EVM 97.05%, retained GTK 95.09%; all six raw profiles and independent source/runtime verifiers pass. Both frontend campaigns pass. |
| NOT_RUN at this source checkpoint | Hosted CI follows commit/push. Inspect the exact commit's Actions results; this checked-in checklist records local acceptance. |
| NOT_RUN | Public funded ERC-20 transfers, full clean OS/Wayland acceptance, combined GUI-approved real-node payments and independent security/legal review. No mainnet funds or user secrets were used. |

## CI production-runtime repair — 2026-10-28

Run [40632779639](https://github.com/neodix42/crypto-wallet/actions/runs/40632779639)
passed its Wails job, but the broader checks failed during coverage. Production
Wails attempted GTK initialization in a test with no display, and the graphical
coverage campaign ran before its CI-only namespace permission was installed.
Missing counters then obscured that startup error. These were actual failures;
the earlier passing development-tag tests did not exercise this configuration.
The later NEAR approval-title refactor had also invalidated the literal native
confirmation mutation anchor; its repair now requires a passing control and the
specific cancelled-confirmation assertion to kill that mutant.

The application now rejects missing/blank X11 and Wayland display settings before
starting child roles or opening profile storage. CI sets up its scoped AppArmor
test profile before coverage, preserves cleanup, and reports unit, security,
race, interoperability, integration, fuzz and coverage failures as separate steps.
No application sandbox setting or production security requirement changes.

Coverage also now measures the shipped React UI and actual private Wails
processes, keeping the existing package/file thresholds. Only diagnostic
`walletcoverage` builds inherit a validated private counter directory and flush
real statement counters before a reply; ordinary builds retain their restricted
prompt environment. Every observed private window PID must have its own counters.
Updated NEAR registry fixtures retain all network/asset identities and bind the
current implementation hash.

| State | Evidence and limits |
| --- | --- |
| PASS | Production-tag missing-display regression: both profiles reject empty/whitespace settings without creating profile files. Focused controller race/vet checks also pass. |
| PASS | CI helper ordering/command allowlist, build-tool regressions and all three pinned actionlint workflow checks. No local AppArmor policy was changed. |
| PASS, scoped | Actual V8 coverage of all shipped React/TypeScript modules, including both mount entrypoints; private/protocol floors remain 95% statements/90% branches, other executable modules 90%/85%. Component coverage does not establish WebKit or public-network acceptance. |
| PASS | Final source-stable combined Go/frontend coverage, all original package floors/local gates and the added real two-token execution gate; no skipped external test is promoted to passing. Coverage integrity: 12 regressions; release-evidence validation: 45 regressions; startup guard: 10 regressions. |
| NOT_RUN at this source checkpoint | Hosted execution follows commit/push. Clean OS/Wayland, GUI-approved real-node payments, remaining funded-network and independent-review gates retain their separate scopes. |

A later local coverage rerun timed out in the portable memory test. A covered
200-repeat reproducer established a test-harness race: an exiting traced thread
returned `ESRCH`, then fatal-assertion cleanup blocked in `exec.Cmd.Wait`. The
test-only tracer now handles this exit race and drains only its own process
group with bounded cleanup, preserving the required injected fault and normal
helper exit. Actual aborted exec-stop/clone-stop regressions verify complete
reaping and helper termination. This changes no production memory protection.
The fixed covered stress run passes 250/250 iterations without skips in
130.811 seconds; ten legacy/memory race repetitions, focused portable race,
nonportable unit and vet checks pass.
The failed run is retained as failed evidence; prior full passes do not certify
the tracer follow-up's full coverage or hosted checks.

## NEAR staking extension — 2026-10-28

Implemented **NEAR Wallet → Staking** through the actual Go FunctionCall/signing/journal pipeline: stake, partial/all unstake, withdraw all, exact `get_account` state and private password-authenticated approvals. No generic contract call or new secret export is exposed. [TP-100–104 checklist, commands, trust limits and precise test evidence](NEAR_STAKING.md).

| State | Evidence and limits |
| --- | --- |
| PASS | Twelve independent official SDK staking vectors, unchanged transfer vectors, strict state/plan tests, 10,000 malformed cases, 36 submission/reconciliation cases and 104 downstream-receipt checks. |
| PASS | Actual official contract on pinned local NEAR: full stake → partial/all unstake → epoch-gated withdraw, independent liquid/delegated balances, four encrypted durable confirmations, cancellation and lost-ack restart/original-hash reconciliation. Validator proposals are explicitly paused; consensus rewards are outside this fixture. |
| PASS, recorded GUI scope | Five actual isolated Wails checks for Staking navigation, exact pool/units, unknown/offline state, disabled Withdraw and graceful exit. No mocked RPC and no funded graphical approval. |
| PASS, scoped | Affected race/vet/frontend/type checks; seven semantic mutants killed; 10-second staking-policy fuzz campaign; fresh NEAR/signer/core/dispatcher statement coverage exceeds unchanged 95% floors. |
| PASS, partial public-testnet scope | Fresh random faucet-funded account, actual password-authenticated 0.01-NEAR stake and unstake_all confirmed; independent pool state verified. No public seed fixture or mainnet funds were used. |
| PARTIAL / NOT_RUN | Testnet withdrawal awaits real pool epochs (`can_withdraw=false`). Actual GUI-approved staking execution, full clean-OS/Wayland/coverage/soak and independent review remain separate. Existing production gates are not waived. |

## CI checkout repair — 2026-10-28

Release run [40631055106](https://github.com/neodix42/crypto-wallet/actions/runs/40631055106) failed in `setup-node` because the configured npm lockfile was absent from commit `b874fde`. The Wails adapter/frontend, pinned fork and new helpers existed only as untracked local files. The cache path was correct; disabling caching would have hidden the first missing input without repairing the build. This repair commits the complete source inventory and adds a Python-only checkout preflight before compiler/cache setup.

| State | Evidence and limits |
| --- | --- |
| PASS | Full pinned-fork inventory verification: 963 upstream files, 20 explicit wallet patch entries, 976 final files. Generated frontend output, `node_modules/`, `.tools/`, executable artifacts and profiles stay excluded from Git. |
| PASS | 58 build/install/release regressions, 15 license-inventory regressions, 16 React tests and focused Wails/controller race tests. The Go results reused valid cached test results for unchanged code. All three workflows pass pinned actionlint. |
| PASS | Twelve checkout-preflight regressions, including missing/untracked lockfile and fork sources, ignored uncommitted frontend source, lock drift and fork tampering. The actual index contains all 1,015 checked build inputs. |
| PASS, clean Git checkout | `python3 scripts/go-build-test.py` rebuilt locked npm dependencies, production frontend, notices and the one-file Go archive from a disposable committed worktree. No application source or generated asset was copied from the development tree; only development SDK/compiler/module caches were reused. Extracted diagnostics work without those caches or toolchains and create no profile. Electron/Debian tool invocations: zero. Report: `reports/ci-checkout-build.json`; this build check does not certify a new GUI/network campaign. |
| NOT_RUN | Hosted build/release for the repair commit, pending push. No additional security, funded-network, OS-matrix or production-readiness gate is claimed. |

## Initial Wails / React distribution — recorded 2026-10-28 candidate

The owner selected the sibling charting project's main UI architecture: Wails v2.17.0 and React/TypeScript with system WebKitGTK 4.1, replacing the handwritten GTK interface. Charting's optional Chromium Screener is not included. CryptoWallet retains one Go executable containing embedded frontend assets and coordinator/signer self-roles, unchanged profile/vault/derivation identities and all nine networks/twelve asset identities. Node/npm compile the frontend only; runtime prerequisites are system GTK3/WebKitGTK. There is no Electron/Chromium bundle or SUID-helper setup. The Go roles apply no syscall sandbox; the pinned Wails implementation also explicitly disables WebKitGTK's renderer sandbox while enforcing local-asset and ephemeral-storage policy. [Current trust boundary and evidence](WAILS_UI.md).

| State | Evidence and limits |
| --- | --- |
| PASS — local build-tool regressions | 58 build/install/archive/release continuity checks and 15 dependency-license provenance checks passed after adapting their runtime identity to Wails/WebKitGTK. Twenty CI-only namespace-helper regressions and all three pinned actionlint workflow checks pass. These are tool regressions, not an actual Wails GUI pass. |
| PASS, current recorded candidate | Actual Wails build/package/extracted diagnostics and six disposable actual-binary installation checks bind the b82b8c executable. No Electron/Debian tool was used, the archive has one regular executable, system WebKitGTK is required and notices/frontend assets are embedded. |
| PASS, actual Wails GUI | `reports/wails-wallet.json`: nine uninstrumented workflows plus an extra graceful-exit row covering all three window closes pass with sourceStable=true, including random phrase/word confirmation, full wallet addresses/tabs/copy, Receive QR, Send presentation, authenticated numbered phrase backup/copy, fresh restoration under a different password and locked restart/unlock. Actual GUI payments and clean OS/Wayland acceptance remain separate. |
| PASS, root installed candidate | Atomically installed regular 0755 `./cryptowallet` has exact tested SHA256 `b82b8c06efd7f7ed849e9467f0a0aabd328f08f0e92f81e4a740ce47eaabb260`, version 0.1.1 and runtime `wails-webkitgtk4.1; portable-go; sandbox=none`. Installation opened no real profile. |
| PASS, recorded headless candidate | Six fresh-copy recovery checks compare 27 canonical descriptors on nine networks/three indices; eight actual private-geth Send checks verify a real receipt, exact balances/fee, nonce 1/no duplicate and durable reconciliation after locked restart. Both bind version 0.1.1 executable SHA256 `b82b8c06efd7f7ed849e9467f0a0aabd328f08f0e92f81e4a740ce47eaabb260`. No GUI payment approval, real user secret or mainnet funds were used. |
| PASS, current source-stable Go scope | `reports/wails-go-tests.json` binds full Go package tests, focused Wails/controller/session/IPC/CLI race checks and repository vet to unchanged sources and exact logs. Optional integration gates retain their own scopes. |
| PASS, focused local test scope | Fourteen Wails adapter tests, controller and CLI race tests cover current private lifecycle/copy guards. Sixteen React component tests pass on unchanged frontend sources. These are separate from actual WebKit/GUI and funded-network acceptance; historical GTK results are not reused. |
| NOT_RUN, coverage driver implemented | Current Wails frontend/runtime coverage measurement must run on unchanged sources; historical GTK/Electron counters do not certify Wails. The production-evidence verifier remains a separate migration gate. |
| NOT_RUN / unfinished integration | Combined Wails-GUI-approved real-node Send acceptance, clean Ubuntu 24.04/26.04 X11/Wayland lifecycle/upgrade/uninstall matrix, funded public-network/Bybit acceptance, 24-hour soak and independent security/legal review. |

This checklist distinguishes implemented migration work from executed acceptance. No real profile, user secret or mainnet payment is authorized for tests. [Detailed commands and current results](WAILS_UI.md) retain remaining gates. Earlier sections below are historical, not evidence for a rebuilt Wails executable.

## Historical handwritten GTK distribution — 2026-10-28

The owner's portable-binary request replaces the default Electron distribution with one Go executable using system GTK3. The old launcher started Chromium, which required a configured sandbox; renaming that launcher did not make it a self-contained Go wallet. The new `cryptowallet` contains the UI, coordinator and signer roles and starts its own child roles through private inherited pipes. It installs no syscall sandbox or SUID helper. The existing vault formats, configuration paths and all nine networks/twelve asset identities remain unchanged. [Build, security boundary and evidence checklist](PORTABLE_GO.md).

| State | Evidence and limits |
| --- | --- |
| PASS | Native ELF/version/dependency checks and a single regular `cryptowallet` member in the archive. Actual build succeeds with Node, npm, Electron and Debian packaging executables forbidden. Safe temporary installation and unrelated-file rejection pass. |
| PASS | Full default Go package tests; focused native controller, private-prompt lifecycle, supervisor and session-monitor race tests. Private-bus lock/suspend/shutdown tests pass; they do not establish real OS acceptance. |
| PASS | Current native binary: six headless recovery checks across three isolated copies, matching all 27 canonical descriptors on nine networks and three indices. Eight isolated actual-geth execution checks independently verify the approved amount, transaction, receipt, fees, balances, nonce and durable reconciliation. No public-chain funds or user secrets were used. |
| PASS — scoped adapter | Seven race-enabled native GTK private-dialog adapter tests on an owned Xvfb display. These are instrumented adapter tests, not a replacement for the actual application drill. |
| PASS — actual native GUI | All nine source-stable scenarios on the uninstrumented installed candidate: generated phrase and recorded-word confirmation, all nine wallet addresses/copy controls, tabs, Receive QR, Send form/units, authenticated numbered phrase backup/copy, an independent fresh-profile restore with a different app password, and locked restart. |
| PASS floors; aggregate FAIL / BLOCKED | All 31 unchanged Go coverage floors pass across five source-stable profiles. The original pinned Decred simnet prerequisite remains BLOCKED, so the aggregate remains FAIL. TON and actual local USDT execution gates pass. [Exact measurements and skipped cases](COVERAGE.md). |
| NOT_RUN — unfinished integration | Native production-evidence verifier migration and a combined native-GUI-approved local-node Send campaign. The headless private-node transfer and graphical Send-form checks are separate evidence; neither establishes this combined gate. The retained Electron production verifier cannot certify this executable. |
| NOT_RUN | Clean Ubuntu 24.04/26.04 X11/Wayland and real OS lifecycle matrix, hosted execution of the changed workflows, public funded testnet/Bybit acceptance, full 24-hour soak, independent security and license review. |

Native reports are `reports/native-wallet.json`, `reports/native-ui.json`, `reports/portable-phrase-recovery.json`, `reports/portable-send.json`, `reports/sessionwatch.json`, `reports/go-coverage.json` and `dist/portable-build-verification.json`. The installed version 0.1.1 executable has SHA256 `bfc3652c18274f807eb823084bfd68b0c94c9fc0cd3e4d12d7e5612bb4dc18dd`; the GUI, recovery, private-node Send, build and temporary-install checks bind those exact bytes. The separate instrumented GUI run is coverage evidence only. A failed attempt is not a passing gate. Production readiness remains unestablished, and a new development build is not an audit claim.

## Historical Electron Copy phrase update — 2026-10-27

The authenticated **Back up recovery phrase** dialog now offers **Copy phrase**. It copies the ordered Go-provided words without numbering. Only this explicit button may export the phrase: setup and keyboard copying remain blocked. The dialog explains that other apps and clipboard history may retain the words and asks the user to clear the clipboard after use. No automatic or guaranteed erasure is claimed.

The private command accepts no text parameters and returns a value-free acknowledgment. Electron main binds it to the live delivered backup prompt, exact trusted main frame/window and pending `backupPhrase` request. Closed, locked, stale, ordinary-renderer, setup and other prompt requests fail closed. Clipboard failures use fixed feedback without exposing native error contents. Copying does not approve or close the dialog, change vault files or bypass the existing per-view Go confirmation/password check. This is the owner's narrow exception to the earlier blanket clipboard prohibition; no other secret export is added.

| Status | Evidence and scope |
| --- | --- |
| PASS | **163 frontend tests**, zero skipped; all eight shipped JS files **100% statements**. Main branches **98.58%**, portfolio **98.27%**, remaining six including secret prompt/preload **100%**. Tests reject wrong frames, kinds, delivery states, arguments, stale callers and native clipboard failures; duplicate clicks and late completions cannot repopulate closed UI. |
| PASS | **14 actual installed backup checks** on an owned private X11 display, including exact copied word order, no numbering, keyboard-copy rejection, no automatic copy, cancellation/wrong-password protection, fresh repeat authentication, unchanged ciphertext and original optional BIP39 passphrase warnings. Only a published synthetic phrase was intentionally copied to the private display clipboard; no user desktop clipboard or secret screenshot was used. |
| PASS | **39** canonical packaged recovery checks, **eight** Unlock/status checks and **seven** actual isolated-geth Send checks pass on the same final artifact. Build/install, all **12** packaged payload/fuse checks and runtime versions pass. |
| PASS | Fresh-copy Go recovery **six checks**, **27 matching descriptors** across nine networks/three indices; owned-process crash/canary **seven checks**, bound to the rebuilt Go binaries. |
| PASS — unchanged source | All **193 Go source/test/module hashes** match retained package/coverage evidence. No Go, derivation, network, signing or vault code changed. |
| FAIL / BLOCKED — unchanged | The retained Go aggregate is still **FAIL** because pinned **dcrd v1.10.9** is unavailable; all **25** measured package floors passed, and nine skipped aggregate tests remain explicit. |
| FAIL — production gate | `make verify-release` still rejects existing incomplete acceptance requirements, the blocked Go aggregate and missing external gates. Current frontend, artifact, packaged recovery and local Send bindings pass. |
| NOT_RUN | Full supported OS/Wayland/lifecycle acceptance, new hosted CI/profile execution, funded public-network/Bybit acceptance, full 24-hour soak and independent review. No real user secrets or mainnet spending. |

Source/artifact-bound evidence is in `reports/phrase-copy.json`; source/test logs remain under the existing ignored report/evidence directories. [Recovery instructions](RECOVERY.md) explain clipboard retention and the separate BIP39-passphrase requirement.

## Previous phrase-backup and CI update — 2026-10-27

First-run **Restore from recovery phrase** already existed; the page now makes that choice explicit. **Settings → Wallet → Back up recovery phrase** confirms the request and verifies the current app password before showing numbered words in the isolated Electron dialog. Every view authenticates again. The operation preserves the encrypted vault, generation, recovery completion, keys and journal and adds no file/clipboard/print/QR export. [Behavior and requirement-to-test checklist](PHRASE_BACKUP.md).

| Status | Current evidence and limits |
| --- | --- |
| PASS | **159 frontend tests**, zero skipped; every shipped JS file **100% statements**. **Four** modified Go packages pass fresh tests and focused race checks; both new backend display functions have **100% statements**. Strict prompt/method binding and cleanup are covered. |
| PASS | **13/13 actual installed X11 backup checks**, including first-run import, cancellation/wrong-password refusal, exact numbered words, repeat authentication, clipboard/ciphertext preservation, lock/restart and separate BIP39-passphrase warning. No private screenshots retained. |
| PASS | Full **39/39** canonical packaged recovery regression, **eight** Unlock/status checks and **seven** actual isolated-geth Send checks on the final installed artifact; **six** fresh-copy headless recovery checks and **seven** owned-process crash/canary checks pass on its Go binaries. |
| PASS | Four focused backup semantic mutants killed with passing controls; **16** short fuzz campaigns, explicit bounded stress, vet, public API/type checks and coverage/release-evidence regression tests pass. Full default **18-mutant** campaign was **NOT_RUN** for this change. |
| PASS / FAIL aggregate | All **25** current Go package coverage floors pass against **193** source/test/module hashes. Aggregate remains **FAIL** because pinned **dcrd v1.10.9** is unavailable; nine aggregate skips remain explicit, including optional local/network/soak prerequisites. No floor or prerequisite was removed. |
| PASS / NOT_RUN remote | CI/release workflows now grant userns only to explicitly started desktop-test processes using a temporary named AppArmor profile and always remove it before publication. **20** offline CI-helper checks, **42** build-tool tests, **three** startup-diagnostic checks and workflow lint pass. New hosted profile loading/workflow execution is **NOT_RUN**. |
| FAIL — intentional reproduction | The current wallet under an otherwise unconfined AppArmor context aborts before a window with Chromium's sandbox-helper fatal; the same artifact starts under the existing IDE's userns-enabled profile. This supports the runner-prerequisite diagnosis; the original remote artifact discarded startup logs, so remote resolution is not yet established. [CI evidence and scope](CI_DESKTOP.md). |
| FAIL — production gate | `make verify-release` still rejects incomplete acceptance, blocked Go aggregate and missing external gates. Current frontend, artifact, recovery, local Send and provenance/evidence bindings introduce no new verifier failure. |
| NOT_RUN | Physical-machine restore, full installed Ubuntu X11/Wayland/lifecycle matrix, funded public testnet/Bybit acceptance, complete 24-hour soak and independent security review. No real user secrets or automatic mainnet payments. |

Scoped source/artifact identities and report hashes are in `reports/phrase-backup.json`; the existing ignore policy keeps generated reports/evidence local. Earlier failed automation attempts remain separate from final passes. The original production release gates below remain required.

## Previous Unlock/status correction — 2026-10-27

The previous correction made the compact **Unlock local wallet** form fit without its internal scrollbar and rendered status messages without a rectangular focus highlight. Input-only bottom spacing was reduced; longer prompts kept their scrolling and reachable controls. Secret handling, authorization and seven-second notice expiry were unchanged.

| Status | Measured scope and limit |
| --- | --- |
| FAIL — reproduced before correction | The old installed artifact has a **14-pixel** internal rail, asymmetric input clearances and input movement under a wheel event. Its focused public error has long outline rows. The existing **620×304** size and **32–33-pixel** input/button gap still pass, exposing the previous test gap. Numeric credential geometry only; no private pixels retained. |
| PASS | **154 frontend tests**, zero skipped; all eight shipped JS files at **100% statements**. Error alerts retain focus/announcements without an outline/border/box. Wrapped long labels, full-size masked fields and reachable keyboard controls remain covered. |
| PASS | **Eight focused installed checks**: **zero** internal rail columns, symmetric **27/27-pixel** input clearances and identical input/button bounds under both wheel directions. Compact **620×304** size and **32-pixel** button gap preserved. No public status-outline runs; notice timing, cancellation byte preservation and log canaries pass. Private pixels stayed in memory. |
| PASS | Build/install, all **12** payload comparisons/fuses and matching versions; **42** build-tool tests; **seven** actual isolated-geth Send checks, **six** headless recovery and **seven** owned-process crash/canary checks bound to final installed binaries. |
| PASS | Full **39-check** source-stable packaged regression on the same final artifact. The targeted test supplements the existing workflow and is now required by `make test-packaged`, including its CI/release callers. |
| FAIL — existing release gates | `make verify-release` still reports incomplete required acceptance, blocked Go aggregate and unrun external gates. No new frontend, artifact, packaged recovery or isolated Send binding failure. |
| FAIL / BLOCKED — unchanged | Required pinned Decred prerequisite and other existing production acceptance gates remain incomplete. |
| NOT_RUN | Real user secrets, mainnet spending, private/funded Bybit acceptance, full installed X11/Wayland/lifecycle matrix, complete soak and independent review. |

The current scoped report is [unlock-scroll.json](evidence/unlock-scroll.json), with [focused installed checks](evidence/unlock-layout.json) and the [old failing reproduction](evidence/unlock-layout-before-fix.json). Detailed generated evidence remains under the repository’s existing ignored report/evidence directories; the ignore policy is unchanged. These passes cover the named local scopes, not the missing production gates.

The earlier **2026-10-27** interface update uses a larger display-bounded main window, compact masked Unlock dialog, a **52-pixel** status bar and **seven-second** automatic notice expiry without a Dismiss button. Bybit configuration remains in **Settings → Exchange**; the main Exchange page no longer has its settings shortcut. Secret, authorization, network and recovery rules are unchanged.

## Previous compact-window verification — 2026-10-27

| Status | Measured scope and limit |
| --- | --- |
| PASS | **154 frontend tests**, zero skipped; all eight shipped JS files have **100% statements**, main **98.53% branches**, renderer **98.26%**, remaining six 100%. Deterministic-clock tests cover exactly seven seconds, new-message renewal, obsolete callbacks, idle locks, focus, unload and preserved recovery/payment gates. |
| PASS | Final **0.1.0** portable build/install, all **12** packaged payload comparisons, production fuses and consistent versions. Main default **1440×960**, display bounded; Unlock **620×304 content pixels**, with larger scrollable layouts retained for other prompt types. |
| PASS | **Seven** actual isolated-geth packaged Send checks with independent receipt, exact recipient credit, source fee debit and delayed nonce; **six** headless recovery and **seven** owned-Go-process crash/canary checks match final installed binaries. |
| PASS | **39/39** source-stable actual packaged X11 checks: main **1248×960** on a **1280×1024** display; no locked-screen horizontal scrollbar/movement; Unlock **620×304** with a **32-pixel** input-to-OK gap; four fixed **52-pixel** footer/automatic-expiry observations; no Dismiss or Exchange settings shortcut. Pending recovery remains actionable beyond notice expiry. Private Unlock pixels stayed in memory and only numeric geometry was retained. |
| PASS | Final-producer-bound X11 driver regression: a vanished owned window is absent, original handler/getter restored, unexpected native errors remain fatal. Its intentionally failing child is required rejection evidence, not a passing wallet run. |
| FAIL | `make verify-release`: existing required acceptance remains incomplete, including the blocked Go aggregate and unrun external gates. No new frontend, artifact, packaged recovery or isolated Send binding failure. |
| FAIL / BLOCKED — unchanged | Full Go aggregate requires the unavailable pinned Decred simnet prerequisite. No Go code or dependencies changed for this interface task. |
| NOT_RUN | Funded/private Bybit acceptance, public-chain payments, complete installed Ubuntu X11/Wayland/lifecycle matrix, full soak and independent review. |

The scoped source/artifact-bound report is [window-compact.json](evidence/window-compact.json). An initial component test rewrite incorrectly moved every action into Settings; the retained failed log and corrected guard remain distinct from final passing coverage. The first 39-check desktop run passed its assertions but contained an unsupported metadata clause about component DOM-width measurement. Its exact source/report/log are retained; the clause was removed and all 39 checks reran on the unchanged application. The current measurements use public pixels and native input, not DOM geometry or private dialog screenshots.

The previous 2026-10-25 interface update uses a reserved fixed-height bottom status bar, including informational validation failures and pending Receive guidance. Wallet rows show exact native amounts with observation details on hover and complete untruncated addresses. Open wallets always show the receive address/copy icon, use asset buttons and identify the selected asset in their titles. Send uses whole-asset **Amount** wording and no QR-image scan control. Tab navigation no longer outlines page headings. See [WALLET_STATUS.md](WALLET_STATUS.md) for behavior, approval boundaries and current evidence.

The preceding balance update distinguishes a NEAR implicit account that has not been created on chain from an unavailable RPC. A zero requires structured absence at an exact fresh final block; network errors, removed/restricted keys and ambiguous responses remain unavailable. An unconfigured Decred wallet displays authenticated-TLS node guidance and skips futile balance queries. Its historical synchronization limits remain unchanged. See [BALANCE_CONNECTIVITY.md](BALANCE_CONNECTIVITY.md) for setup and that candidate's evidence.

The preceding launcher update filters only three complete, precisely identified Chromium X11 atom-cache messages. Other errors, unknown atoms, FATAL output, malformed/partial/binary data and oversized lines remain visible. The bounded stderr helper has no coordinator IPC descriptors; Electron still replaces the launcher on its locked creator thread, preserving coordinator parent-death behavior. See [X11_DIAGNOSTICS.md](X11_DIAGNOSTICS.md) for that historical candidate's implementation and verification.

The earlier interface update makes **Settings** a main-window tab immediately to the right of **Exchange**, with **Wallet**, **Exchange** and **Journal** subtabs. Bybit configuration remains under Settings → Exchange and the local payment journal under Settings → Journal. Asset controls distinguish owned addresses, EVM token contracts and HyperCore token identifiers. Clearer signer credential labels preserve the authentication design. [The Settings evidence](EXCHANGE_SETTINGS.md) describes its previous source/artifact; its floating notices have now moved into the status bar.

Earlier ASAR hashes, counts and candidate-specific PASS results below are historical. The previously RUNNING soak no longer has its recorded process or a completion report; it is **NOT_RUN/partial**, not active or passing.

The current local build is an **unsigned development candidate**, version **0.1.0**, with mainnet selected by default. Production acceptance and independent review remain incomplete. The owner deferred Decred-specific implementation work and public funded testnet campaigns for this hardening batch; their original release gates remain open. See [PRODUCTION_HARDENING.md](PRODUCTION_HARDENING.md) and the [remaining checklist](PRODUCTION_READINESS.md).

PASS means the named scope ran successfully. RUNNING does not mean complete. NOT_RUN identifies missing execution, and BLOCKED identifies a demonstrated obstacle. Local nodes, same-host installations and component tests do not establish public-network, physical-machine or complete installed-OS acceptance. Tests used public/disposable fixtures only; no real user profile or secret was accessed and no mainnet funds were automatically sent.

Every root specification and applicable instruction was read; no applicable AGENTS.md was present. Source/test hashes and binary/ASAR identities identify the modified working tree; a base Git commit alone does not. [candidate.json](evidence/candidate.json), [production-hardening.json](evidence/production-hardening.json) and generated source inventories retain the candidate bindings.

## Previous wallet/status interface verification — historical 2026-10-25 evidence

| Status | Measured scope and limit |
| --- | --- |
| PASS | **148 frontend tests**, zero skipped; all eight shipped JS files at **100% statements**, main **98.47% branches**, renderer **98.26%**, remaining six 100%. Tests cover all twelve assets, exact amount strings, asset-change approval invalidation, keyboard focus, complete addresses, signer-bound copy, locked cleanup and fixed safe error guidance. |
| PASS | Six added notice-boundary regression groups exercise actual main-process frame handling: only informational notices receive a negative automatic dismissal; password, phrase, Receive and approval windows retain explicit replies. Exact method/title/summary matches yield one fixed recovery mismatch code; raw notice text and injected backend codes remain unavailable to the portfolio. |
| FAIL — first packaged attempt | Actual Go creation can return a successful pending wallet after wrong words. The first notice-routing version reported completion. Final code reports the fixed mismatch and refreshes the existing wallet; new tests require actionable pending controls and no replacement creation, receive or send. This real integration failure remains retained separately. |
| PASS | Final **0.1.0** build/install, all **12** production payload comparisons, fuses and version checks. Five focused Go amount/coordinator/approval tests pass; `1` remains a whole-asset decimal value converted exactly in Go. |
| PASS | **Six** fresh-install headless recovery and **seven** owned-Go-process crash/canary checks against the final installed Go hashes. These do not establish separate physical-machine or complete Electron/OS crash acceptance. |
| PASS | **Seven** actual packaged Send checks on the final ASAR against isolated real geth: independent successful receipt, exact recipient credit, source fee debit and delayed nonce. Public genesis fixtures and no outside network route. |
| PASS | **34/34** source-stable actual packaged X11 checks on the final candidate: three fixed **88-pixel** footer/layout observations, four heading-outline observations, independently verified complete addresses, asset buttons and whole-asset units, Receive QR/copy, recovery, restart, profile isolation and secret-canary/log scans. Earlier failures remain retained separately. |
| PASS — unchanged source | All **192** Go source/test/module hashes exactly match the prior package-test and **25** coverage-floor evidence, including TON emulator and isolated USDT execution gates. Those full scopes were not rerun for frontend-only changes. |
| FAIL / BLOCKED | Overall Go coverage remains **FAIL** because required pinned **dcrd v1.10.9** is unavailable. Nine aggregate skips remain explicit; no original floor or prerequisite was removed. |
| NOT_RUN | Private Bybit/funded acceptance, live configured Decred balance, complete clean Ubuntu X11/Wayland/lifecycle matrix, complete soak and independent review. Local interface verification does not close production gates. |
| FAIL | `make verify-release`: required acceptance requirements remain incomplete, including the blocked Go aggregate and external gates. No new frontend, artifact, packaged recovery or isolated Send binding failure. [Verifier log](evidence/wallet-status-production-gate.log). |

The scoped report is [wallet-status.json](evidence/wallet-status.json), with commands and source/artifact identities. Earlier component expectation failures and VM coverage-attribution measurements remain distinct from the final passing suite. [WALLET_STATUS.md](WALLET_STATUS.md) explains the resulting interface and meaning of Bybit approvals.

## Previous NEAR and Decred balance candidate — historical 2026-10-25 evidence

| Status | Measured scope and limit |
| --- | --- |
| PASS | Ten new hermetic NEAR regression groups cover verified absence, exact final-block context, malformed/error ambiguity, stale/wrong chain, funding races, invalid descriptors, missing/restricted keys and transport/cancellation failures. Preparation still refuses an uncreated sender. |
| PASS | Actual pinned NEAR sandbox: fresh implicit account zero before creation, real signed local funding, exact funded balance afterward, independent recipient queries and rejected removed/restricted keys. Explicit **read-only** default-mainnet probe also verifies absence of a public-fixture account at a final block; no public transaction or user wallet involved. |
| PASS | **135 frontend tests**, zero skipped; all eight shipped JS files at **100% statements**, renderer **98.40% branches**, main **98.41%**. Missing Decred configuration avoids RPC calls; configured failures/stale replies remain unknown. |
| PASS | Full Go package tests and all **25** unchanged coverage floors, bound to **192** source/test/module hashes. NEAR measures **98.03%** statements. Actual TON V5R1 emulator, isolated USDT execution and focused NEAR/core race-detector checks pass. |
| FAIL / BLOCKED | Overall Go coverage gate remains **FAIL** because pinned **dcrd v1.10.9** is unavailable. Nine aggregate skips are explicit; the NEAR sandbox and public read-only probe ran separately. No gate or floor was waived. |
| PASS | Rebuilt and installed **0.1.0** wallet; all **12** packaged payload comparisons, production fuses and consistent versions pass. **Seven** actual packaged Send checks execute against isolated real geth; **six** headless fresh-install recovery and **seven** owned-Go-process crash/canary checks pass against these binaries. |
| PASS | **29/29** source-stable actual packaged X11 recovery checks, including two new Decred overview/wallet setup-guidance observations. Six live-log observations contain no Mesa, ordinary IPC stack or known X11 atom diagnostic. Public-pixel automation uses disposable profiles only; this is not the full installed OS matrix. |
| NOT_RUN / deferred | Authenticated live Decred mainnet balance: no configured synchronized trusted-TLS node. Persistent historical synchronization remains deferred; increasing the bounded scan budget alone does not overcome its sequential requests and 30-second deadline. |
| NOT_RUN | Public funded testnets, complete installed X11/Wayland/lifecycle matrix, complete soak, private Bybit acceptance and independent review. This fix does not establish production readiness. |

That candidate's scoped report is [near-dcr-balances.json](evidence/near-dcr-balances.json). The first public-probe command selected a nonexistent test and ran none; its result is **NOT_RUN**, retained separately from the correctly selected passing probe. The first formal frontend run caught a stale static-text assertion after the wording changed; that assertion was corrected and the full suite reran successfully. Earlier candidates below retain their historical results and bindings.

## Previous X11 diagnostic candidate — historical 2026-10-25 evidence

| Status | Measured scope and limit |
| --- | --- |
| PASS | Exact diagnostic filter: six top-level tests, 45 named subcases and bounded-line/chunk loops, with **100% filter-function statement coverage**. Existing **32** real launcher exec/private-pipe checks and **12** new isolated helper lifecycle cases pass. Final complete launcher coverage is **94.23%** (**147/156** statements), above its unchanged **90%** floor. Genuine queued diagnostics survive ordinary and abrupt parent termination. |
| PASS | `make install`, **12** artifact payload comparisons, production fuses/ASAR inventory and matching **0.1.0** versions; actual disposable X11 startup and **27/27** source-stable final-candidate packaged recovery checks. Six live-log observations contain no known X11 atom message, Mesa diagnostic or IPC stack trace. The same final runtime passes **seven** actual isolated-geth packaged Send checks. No real user profile, secret or mainnet funds used. |
| PASS — unchanged source | The **132** frontend tests and all eight files' **100% statement coverage** remain bound to unchanged frontend source; renderer branches **98.38%**, main **98.41%**. This does not reuse the earlier candidate's launcher or packaged acceptance. |
| PASS | Source-stable full Go package tests and all **25** coverage floors, bound to **190** source/test/module hashes; actual TON V5R1 emulator and isolated USDT execution gates pass. Complete launcher and supervisor race-detector runs pass; a new whole-repository race campaign is not claimed. |
| FAIL / BLOCKED | Full Go coverage aggregate **FAIL** solely because required pinned **dcrd v1.10.9** is unavailable and its simnet gate is **BLOCKED**; eight optional cases remain explicitly NOT_RUN. Owner-deferred Decred provisioning did not remove the prerequisite or reduce any floor. |
| PASS | **Six** fresh-install headless Go protocol recovery checks and **seven** owned Go-process crash/canary checks rerun against the changed launcher/final binaries; these do not establish separate physical-machine or full Electron/OS crash acceptance. |
| NOT_RUN | Full supported X11/Wayland OS matrix, complete soak, independent review and live private Bybit/funded network acceptance. The exact-message workaround does not close these gates. |

The scoped report is [x11-diagnostics.json](evidence/x11-diagnostics.json). Earlier source/artifact-specific reports below retain their historical scopes; current aggregate status must be read separately from individual passing checks.

## Previous Settings and notice candidate — historical 2026-10-25 evidence

| Status | Measured scope and limit |
| --- | --- |
| PASS | **132 frontend tests**, zero skipped; all eight shipped JS files have **100% statement coverage**. Renderer branches **98.38%**, main **98.41%**, other six files **100%**. Component checks cover main-window Settings order/navigation, journal isolation, fixed-position dismissible notices, token/owned-address labels, HyperCore/EVM identifier isolation, cancellation and sanitized stale/error handling. |
| PASS | Full Go package tests and all **25** measured package floors; native-input regressions, actual TON V5R1 emulator and actual isolated USDT execution also pass. The retained GTK input tests describe unshipped legacy code. |
| FAIL / BLOCKED | The full Go coverage wrapper reports **FAIL** because its required pinned **dcrd v1.10.9** executable is unavailable. Its Decred simnet gate is **BLOCKED**; eight optional cases are explicitly NOT_RUN. Decred provisioning is deferred by the owner. No floor, prerequisite or aggregate requirement was removed to obtain a pass. |
| PASS | **16** bounded local fuzz targets at **three seconds each**, source stable; **five** Exchange signer policy mutants killed. These do not establish long-duration stress or independent review. |
| PASS | Final **0.1.0** portable build/install, **12** payload comparisons and production fuses/ASAR inventory. The actual final-candidate X11 drill passes **26** source-stable checks: Settings/Journal navigation and persistence, full owned Ethereum address versus USDT contract through public-pixel OCR, three floating-notice layout observations, empty API-input/app-password cancellation with unchanged vault, receive QR/copy, Send-form navigation, restart, profile isolation, nine network wallets and synthetic secret canaries. API-secret field labels were not independently read by the packaged drill. |
| PASS | **Seven** final-candidate packaged Electron Send checks with independently queried actual isolated geth receipt, exact recipient credit, source fee debit and nonce; **six** headless fresh-install phrase-recovery checks and **seven** Go-process crash/canary checks bound to the final binaries. These do not establish separate physical-machine or complete Electron/OS crash acceptance. |
| NOT_RUN | Live private Bybit authentication, credited deposits, spot fills and independently verified withdrawals; no real API credential or mainnet funds used. Original full OS, soak, public-network and independent-review gates remain open. |

The scoped source/artifact-bound report is [settings-main.json](evidence/settings-main.json), with referenced frontend, packaged, recovery, crash, fuzz and mutation measurements. The broader [Exchange aggregate](evidence/exchange.json) is **FAIL** because the current Go wrapper's required Decred simnet prerequisite is blocked. Source bindings and aggregate status must be checked alongside individual passing results.

## Original Exchange implementation candidate — historical 2026-10-24 evidence

The explicitly requested Bybit extension was implemented and installed as `./wallet`, version **0.1.0**. The optional tab uses real Go Bybit V5 requests for separately approved deposits, spot asset/USDT swaps and withdrawals to the selected owned wallet. It retains all nine mainnet domains and adds canonical Ethereum USDT for twelve asset identities. This requires a Unified Trading Account; Classic-only flow and unsupported regional withdrawal questionnaires are not implemented. The hashes and counts in this section describe that original artifact, not the later Settings layout. See [EXCHANGE.md](EXCHANGE.md).

| Component | Original candidate SHA-256 |
| --- | --- |
| `wallet-core` | `79f9efee548a857e64f6c1a38097ef236c2030eb169d5b075ed294736afc8af7` |
| `wallet-launcher` | `164b9c11ee193392dab11ea0cf484bcd94fb1d91e02281609a5b6bbd13c34065` |
| `wallet-signer` | `6340d8f4a7773e873ffc6a98d8e5cafa20dddaa3d5b553158240fe5df09db077` |
| `app.asar` | `013c01b218cf05e1a5a295e9333e7d67a66a29488281c78ba8b82b2064c2d9d6` |

| Status | Original measured scope |
| --- | --- |
| PASS | Source-stable full Go/native coverage: **186** source/test/module hashes, **25** measured package floors; original dcrd/TON gates and the new actual isolated USDT EVM execution gate pass. Core **96.84%**, Bybit client **99.54%**, exchange routes **100%**, signer **96.75%**, vault **96.93%**. Critical floors remain 95%; launcher glue retains 90%. Seven optional cases explicitly skip, including the all-chain soak and separately gated local-node cases; the report lists every skip. |
| PASS | **125 frontend tests**, zero skipped; all eight shipped JS files have **100% statements**. That candidate's renderer branch coverage is **98.24%**, main **98.41%**. These are component tests, separate from packaged execution. |
| PASS | **365 core tests/subtests** and **131 Bybit client tests/subtests**, including hostile provider data, wrong routes, actual TON registry identity/memo, auth/state/persistence/lock faults, complete fills/taxes and overflow that must not become zero. The coordinator fixture models signer/provider responses; it is not a live exchange. |
| PASS | **23 actual packaged X11 checks**, including all three new Exchange checks: permanent tab navigation, visibly disabled disconnected money buttons, and cancellation at app-password/API-input dialogs with unchanged vault. No API key was entered or custody confirmation approved. The same candidate passes the separate **seven-check actual isolated geth Send** drill, with exact receipt, recipient credit, source fee debit and nonce. |
| PASS | Six headless recovery checks across three independent fresh application copies, and seven owned-process crash/canary checks, bound to the current binaries. These do not establish separate physical-machine or complete Electron/OS crash acceptance. |
| PASS | Three actual Go-signed USDT transactions execute against retained canonical USDT runtime in isolated Ganache, independently decoded with ethers; exact credit/debit/log/gas/nonce/no-extra-transaction checks and malformed/pending issuer-state regressions pass. No public consensus or funded Ethereum acceptance is implied. |
| PASS | Full Go race, vet, dependency/module checks, workflow lint, 16 actual bounded three-second fuzz targets, five client policy mutants, five signer policy mutants and 14 retained wallet mutants. The Bybit package also has two separately bounded 15-second fuzz campaigns. No reachable Go/npm vulnerabilities were reported; one advisory remains in an uncalled required Go module. |
| PASS | 42 build-tool, five coverage-integrity, five campaign-integrity, 18 release-authentication, **28** release-evidence and 13 notice-collection regressions. The evidence verifier now also requires the actual USDT execution gate; missing or duplicate substitutes cannot pass. |
| NOT_RUN — partial | A replacement source/test-binary-bound **86,400-second non-Decred soak** was started for this candidate, but it no longer has its recorded process or a terminal completion result. It exercised the production KDF/lifecycle and fake-ledger journal, not Bybit. Incomplete state is retained in `reports/soak.json` and `reports/nondecred-soak-run.json`; neither this campaign nor the earlier interrupted campaign passes the scoped or all-chain gate. |
| NOT_RUN | Actual private Bybit authentication, credited deposits, accepted spot fills, regional/whitelist eligibility and independently verified withdrawal credit. No real API key or mainnet funds were used. Independent review and the original public-network/full OS/release gates remain incomplete. |

The original candidate's detailed evidence remains in repository history. The current [exchange.json](evidence/exchange.json) is regenerated for the changed source and reports **FAIL** because the required Decred simnet prerequisite is blocked; historical passing counts above do not override it. [exchange-live.json](evidence/exchange-live.json) records the unrun private acceptance. A missing or changed report fails the local evidence collector. Reproduce it after the named checks with `python3 scripts/exchange-evidence.py`; this does not perform any exchange call. Local PASS never grants production acceptance.

## Previous hardening candidate — historical evidence

The hashes, counts and results in this section predate the Exchange extension. Current Exchange evidence is recorded separately in [EXCHANGE.md](EXCHANGE.md) and `evidence/exchange.json`; these older artifact hashes do not identify the changed build.

Both the 20-check packaged recovery report and the separate seven-check packaged Send report identify this same artifact:

| Component | SHA-256 |
| --- | --- |
| `wallet-core` | `d0a9cab95799361ed288a457e5bc5243e224ffc814546ae496bb2dee9ddd3e71` |
| `wallet-signer` | `5f9d835ff32180c58dd332e1e9e9fd708f46a0e577c2204b7375ab5d19497f59` |
| `wallet-launcher` | `39bf5716b5527b0af4df2a86083b24ae6641f74b65dad6754e41b93d016588d5` |
| `app.asar` | `3b3c86affdc042288f7b878c8dccb65b1a779bf67e060d9163b0491a9f2bfa10` |

| Status | Evidence and limits |
| --- | --- |
| PASS | Source-stable Go coverage: **163** source/test/module hashes, **23** measured floors in **64.214 seconds**. Twenty-two measured packages ship; the retained GTK package is unshipped legacy code. Core 95.36%, launcher 93.55%, signer command 96.77%, signer 97.56%, TON 96.26%, HyperCore 96.95%, supervisor 96.72%, isolation 100%, prompt protocol 95% and vault 96.31%. [COVERAGE.md](COVERAGE.md) reviews all **203** uncovered blocks/**219** statements. Go measures statements, not branches; C/Chromium/third-party internals remain outside these percentages. |
| PASS | **107 frontend tests**, zero skips; all eight shipped JS files at **100% statements**. Main branches 98.41%, portfolio renderer 98.18%, other six files 100%. Secret-bearing prompt logic retains the critical 95% statement/90% branch floor. [Frontend coverage](evidence/frontend-coverage.json). |
| PASS | Headless real Go prompt-protocol recovery: **six checks**, three fresh application copies with separate HOME/config/processes, same/different app passwords, restart locked, and **27 exact descriptors** across nine mainnet domains at indices 0/1/127. No original vault or keyring was copied. [Recovery report](evidence/phrase-recovery.json). |
| PASS | **20 uninstrumented packaged Electron X11 checks**: password/phrase validation, random phrase setup and wrong-word denial, pending-root cancellation/resume/reauthentication, fresh-profile phrase restore, independent public address, table balances/copy, Settings journal, independently decoded receive QR, tabs, restart and profile isolation. Default 12-word GUI scope is separate from all-five-length component tests. [Packaged recovery](evidence/packaged-recovery.json). |
| PASS | **Seven actual packaged Send checks** against pinned geth 1.17.9. The real Electron form, Go-authored approval, masked reauthentication and signing/submission produced a successful type-2 receipt, exact recipient credit of `12345678912345678` atomic ETH, source debit including the execution fee and delayed nonce 1. The app/node ran as an ordinary UID in a loopback-only network namespace with public genesis fixtures and no outside route. This is real local EVM execution with test Engine-API block production, not public-chain/beacon consensus acceptance. [Packaged Send](evidence/packaged-send.json). The recovery report's own unrun-send entry applies to that recovery drill, not this separate passing Send scope. |
| PASS | **Seven owned-process crash/canary checks** across 13 actual Go process clients: core kill during password/recording/challenge, signer kill/abort, failed reauthentication/cancellation, same-root locked restart and tested public IPC/log/file/argv/env canaries, including entropy, seed and derived-key encodings. The driver emulates only typed prompts; it does not establish Electron/host crash-collection, clipboard/browser-storage/QR/network or live-send crash acceptance. [Crash report](evidence/crash-security.json). |
| PASS | Complete desktop plus Go release inventories and publisher-bound OIDC/Sigstore verification implementation: **18 authentication tests**, including real frozen upstream signatures and tamper/wrong-key/identity rejection; **27 evidence-verifier regressions** reject stale source, partial coverage and substituted/incomplete artifacts. No LocalWallet publisher attestation or remote publication was fabricated. [Authentication](evidence/release-auth.json), [evidence verifier](evidence/release-evidence.json), [verification instructions](BUILD.md). |
| PASS | Technical upstream notice/provenance collection and bundling for **39 Go components/40 desktop components**, **13 regression tests** and repeated deterministic collection. Independent license/compatibility/redistribution review remains NOT_RUN. [Notice evidence](LICENSE_NOTICES.md). |
| PASS | Full Go race, final actual TON emulator race, vet, dependency checks, workflow lint, **42 build-tool regressions**, and **14/14 killed semantic mutants**. No reachable Go or npm vulnerabilities were reported; one uncalled advisory remains in a required Go module. Candidate-bound logs retain their exact scope. |
| PASS — scoped execution | **13 non-Decred fuzz targets**, **60 seconds each**, source unchanged. The excluded Decred target and full aggregate remain **NOT_RUN**. The scheduled workflow specifies 300 seconds per target; remote scheduled execution is NOT_RUN. [Long fuzz](evidence/fuzz-long.json). |
| NOT_RUN — interrupted | The pre-Exchange source/test-binary-bound **86,400-second non-Decred soak** was interrupted before its required duration when this extension changed the baseline: production 256 MiB Argon2id, encrypted signer restart/phrase recovery, bounded discovery/growing history and exact signed-journal EVM fake-ledger reconciliation. Live ignored files are `reports/soak.json` and `reports/nondecred-soak-run.json`; retained evidence is a snapshot. No final resources/latency/reconciliation result is claimed. The all-chain 24-hour gate remains **NOT_RUN**. |

For that historical candidate, the pinned local dcrd simnet and actual TON V5R1 emulator passed in its Go wrapper. Optional NEAR/Aptos/geth cases and the full soak explicitly skipped without separate prerequisites. Fourteen retained GTK tests passed with one optional five-minute skip; production signer dependency/linkage contained no GTK window implementation. The historical GTK/XTest long-input evidence does not establish current Electron/Wayland or OS lock/suspend acceptance. The current wrapper's missing dcrd prerequisite is recorded separately above.

## Implemented behavior and remaining milestones

| Milestone | Implemented and tested scope | Remaining release requirement |
| --- | --- | --- |
| 0 Repository/feasibility | Pinned Go 1.27.3, Node 24.19.0/npm 11.17.0, Electron 45.0.0; Ubuntu 26.04 amd64 local portable launch, seccomp and isolated Electron prompts | Independent build/supply-chain review and full supported clean-OS matrix NOT_RUN |
| 1 Vault/profiles/recovery | Argon2id 256 MiB/3/4, XChaCha20-Poly1305, private atomic generations, all BIP39 import lengths, immutable independent profiles, Go word/password checks and canonical phrase restoration | Separate physical-machine/complete OS recovery acceptance NOT_RUN; whole-directory rollback cannot be detected |
| 2 Desktop/receive/history | Mainnet default and separate explicit testnet profile; account/wallet tabs, table balances/copy, main-window Settings with Wallet/Exchange/Journal, floating notices, on-demand Send, verified Receive QR and account/history pages with session/epoch/query/revision-bound single-use cursors | Full Ubuntu 24.04/26.04 X11/Wayland, high-DPI/resize, screen lock/logout/suspend and install/upgrade matrix NOT_RUN |
| 3 Transaction foundation/EVM | Exact integer plans, Go-validated Electron approval/reauthentication, durable original signed journal/reservations, uncertainty handling, independent fixtures, earlier local ZEN runtime tests and current actual packaged geth Send | Required public ETH/HyperEVM/Base/Horizen acceptance NOT_RUN |
| 4 NEAR/Aptos | Real signing/serialization, authority/rotation checks and independent fixtures; earlier source-bound actual local validator/sandbox reports remain available | Current optional local-node cases skip in the aggregate; public acceptance and identified wallet-application imports NOT_RUN |
| 5 Decred/TON | Existing official Decred Child/script/maturity/tree checks and historical simnet regressions; TON exact V5R1 StateInit, actual TVM source/recipient execution, linked archival pages and bounded exact reconciliation | Current simnet gate BLOCKED by unavailable pinned dcrd; Decred-specific work deferred; public network acceptance and exhaustive archival recovery remain open |
| 6 HyperCore | Official pinned SDK fixtures, exact owner/spot/domain/precision checks, committed time-window pages, saturated-query/nonprogress handling and source/recipient effect matching | Actual accepted public Core action NOT_RUN; fresh recipient activation **BLOCKED** by unresolved authoritative fee-selection/debit/receipt semantics |
| 7 Security/recovery/stress | Current prompt/IPC/canary/coverage/race/mutation/fuzz scopes and bounded production-KDF/signed-journal workloads | Pre-Exchange scoped 24-hour run interrupted; all-chain 24-hour, complete Electron/host crash campaign, installed lifecycle matrix and independent review NOT_RUN |
| 8 Packaging/release | Ready-to-run local `./cryptowallet`, portable-only release asset with identical bare numeric tag/name/app version, retained runtime notices and provenance verification; manual Go packaging remains separate; no Debian package | Actual remote OIDC issuance/publication/consumer install, independent supply-chain and legal review NOT_RUN; production aggregate deliberately fails |

Account and journal pages now avoid transporting entire archives through a 1 MiB pipe. The signer keeps the complete authenticated state and reservations; public journal pages redact signed artifacts, and reconciliation retrieves one exact private operation. Tests unlock a real encrypted archive exceeding 1 MiB with 300 independently derived accounts/300 synthetic operations and page 10,000 public account descriptors. The portfolio installs one complete account revision and rejects stale, duplicate, truncated or oversized pages. [HISTORY_PAGINATION.md](HISTORY_PAGINATION.md) records the budgets and limits.

TON continuation follows exact BOC previous-transaction identities; its bounded search reaches beyond the former latest-100 window. HyperCore commits fetched pages, handles inclusive timestamp overlap and rejects unsafe nonprogress. Missing traces or exhausted budgets remain UNKNOWN/SUBMITTED with original reservations retained. Persisted incremental reconciliation beyond current budgets and exhaustive historical recovery remain **NOT_RUN**. Decred retains its existing bounded scan ceiling; no new Decred synchronization is claimed.

## Secret boundary and recovery policy

The owner requires Electron for every application window. Go generates and durably encrypts the BIP39 root, authors prompts, verifies recorded words/passwords, reconstructs immutable spend effects and signs. The separate temporary prompt origin/preload/session handles masked credentials and pending phrase recording; the ordinary portfolio has no prompt retrieval/reply API. Cancellation preserves the same pending root, resume requires reauthentication, under the former policy confirmed roots had no repeated display (the 2026-10-27 owner update adds authenticated manual backup), and changing the app password preserves addresses.

This intentionally trusts the coordinator/supervisor, Electron main and dedicated prompt renderer/preload with transient credential/phrase frames. Ordinary public requests/snapshots, logs, plaintext files, browser storage, clipboard, QR and network requests must stay secret-free; entropy/binary seeds/private keys remain in Go. Go generation does not protect against compromised trusted prompt/main code. Core and signer dump protections are tested; Electron inherits zero core-size limits but post-exec Electron non-dumpability and immunity to privileged host collection are not established. Runtime/OS copies prevent guaranteed erasure. See [ELECTRON_INTERFACE.md](ELECTRON_INTERFACE.md) and [THREAT_MODEL.md](THREAT_MODEL.md).

Written-phrase recovery supersedes the former encrypted-backup workflow; all-Go/GTK application windows were then superseded by the owner's Electron-only instruction. Earlier backup/native reports retain their historical scope. The release-banner removal report is also historical presentation evidence; the current hardening artifact was rebuilt and independently exercised as described above. SDK vectors do not establish imports into unidentified third-party wallet applications.

A phrase restores canonical keys/accounts, not unknown named/rotated associations, Decred discovery counters, RPC credentials, labels or the local payment journal. Same-host independent installations are not separate physical machines. Reconcile original uncertain identities before creating replacements. [RECOVERY.md](RECOVERY.md), [WALLET_ACTIONS.md](WALLET_ACTIONS.md) and the [traceability table](traceability.json) retain these distinctions.

## Distribution and open gates

`make build` creates Go components without Node/Electron or Debian packaging. `make install` separately builds the complete portable desktop and ready-to-run `./cryptowallet`; `.tools` is an ignored developer/test cache and is unnecessary for installed/downloaded runtime users. Local profiles remain separate from binaries and caches. The workflow publishes only **cryptowallet-<version>-linux-amd64.tar.gz**, with the matching automatically incremented bare numeric app version, tag and title. With **0.1.2** already published under the previous name, the renamed archive is available after the updated workflow publishes **0.1.3**. Detached signing records stay in its Actions artifact, not release assets. The public desktop executable is **cryptowallet**, inside **CryptoWallet-linux-x64/**; existing `~/.config/localwallet` and `$XDG_CONFIG_HOME/localwallet` profile paths remain the same to preserve keys and settings without migration or replacement wallets. See [the current release-layout evidence checklist](PORTABLE_RELEASE.md). Publisher verification pins repository ID, workflow, branch, source commit and trusted OIDC/Sigstore identity; checksums alone do not authenticate a publisher. Current local artifacts remain unsigned, and no remote workflow was triggered. Automatic updating remains disabled. See [BUILD.md](BUILD.md) and [DEPENDENCIES.md](DEPENDENCIES.md).

All nine mainnet domains remain represented; the Exchange extension adds canonical Ethereum USDT for twelve assets. Earlier read-only provider checks passed 29 assertions across eight domains; those observations do not establish payment acceptance. Authenticated, synchronized Decred mainnet access remains **BLOCKED** pending an operator-provided trusted TLS node. Earlier container startup probes remain **BLOCKED** by sandbox configuration; no sandbox bypass was used, and these probes do not prove failure on stock supported desktops.

Production remains incomplete: deferred Decred features/public funded testnet roundtrips; fresh Core activation; third-party wallet/version interoperability; complete clean Ubuntu/display/lifecycle/upgrade and separate physical-machine recovery; full Electron/host crash and network-canary acceptance; finished scoped and all-chain soak requirements; incremental reconciliation beyond budgets; independent security and legal review; and actual authenticated remote release/consumer installation. `make verify-release` rejects these missing mandatory gates. Local passes and a development release do not close them.
