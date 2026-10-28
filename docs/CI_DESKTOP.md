# Current desktop checks in GitHub Actions

## Hosted table-copy and race-stress follow-up — 2026-10-28

[Run 40695614368](https://github.com/neodix42/crypto-wallet/actions/runs/40695614368) at `ece6768` confirms the NEAR correction on the hosted runner: first-attempt navigation, exact 25-byte contract recognition at native resolution, and all staking assertions pass. The next GUI step fails waiting for the verified receive dialog after clicking the Ethereum table-row copy icon. That click used a fixed point 55 pixels from the window edge rather than observing the button. Workspace padding and table geometry make that assumption unreliable. The failed report does not distinguish a missed click from a click on the adjacent row cell; neither is a passing copy test.

A guarded actual-GUI probe reproduces a partly clipped copy button at the hosted window geometry `[80,100,1440,900]`: the table viewport ends at x=1488, while the control extends to approximately x=1496. The former x=1465 click is close to its left edge and can miss with font/layout variation. The new whole-button locator initially rejects this clipped control, correctly failing rather than clicking an unverified point. That failed probe is retained separately.

The correction must reveal the table's control through bounded ordinary horizontal scrolling, locate the complete button within the uniquely observed Ethereum row on the Main account table, require the Go-authored **Verify receive address** dialog, and verify that the table stays open. Its copied address must equal the independently verified Ethereum wallet address later in the same campaign. Only this read-only request may retry; unexpected private windows and accidental wallet navigation remain failures. No application signing, RPC or clipboard policy changes are justified by the current evidence. Source-specific validation is recorded in [STATUS.md](STATUS.md).

The same run's race job completes every stress workload without a reported data race, but fails its declared signer-lifecycle latency budget: 60.440580 seconds against 60 seconds. The instrumented stress package runs beside the much longer signer package under `go test -race -p 2` on a two-CPU runner. This is failed performance evidence, even though the functional workload completes. The repair isolates the measured stress package from other race-test packages; every package and race check must still execute, and the 60-second limit, production KDF, workload counts and resource budgets stay unchanged. Ordinary unit and vet routing remain unchanged.

The isolated full race stress package passes locally under two-CPU affinity with lifecycle p99 28.834226 seconds and stable sources. Seven routing regressions enforce the exact package partition, completion before the stress phase, uncached measurement and failure propagation from either phase. The final focused actual GUI passes with one table reveal and one copy click. The complete 20-step local campaign also passes with stable sources, including the independent table/wallet address comparison and fresh-copy recovery of all eight wallets. On a locator failure, only the guarded public right-row region (which can include an address suffix) and its hash are retained through the narrow `wails-*-main-row-public-controls.png` artifact path. Private or ambiguous windows block the original capture. These local results do not establish the next hosted result or a clean supported OS matrix.

## Hosted contract-text observation follow-up — 2026-10-28

The follow-up [run 40690429440](https://github.com/neodix42/crypto-wallet/actions/runs/40690429440) at `8e677a2` passes the repaired mutation/security step. Its new GUI diagnostics establish that NEAR navigation succeeds on the first attempt and the actual Staking page displays AstroStakers. Only the exact full-contract recognition fails. The earlier missed-click hypothesis did not establish the cause.

A static public-text probe reproduces the distinction: Ubuntu 24's Tesseract 5.3.4 with DejaVu Sans Mono reads `poolv1` as `poolvl`, including several ordinary upscale modes. The development machine uses Tesseract 5.5. The earlier “CI-font” probe matched the sans-serif brand metrics but incorrectly forced that sans face onto monospace content too; it did not reproduce the hosted contract font. This is a test-fixture gap, not evidence of a changed contract or a passing GUI campaign. The correction must preserve exact contract identity and reject near-miss addresses, without substituting `1`/`l` or forcing expected characters.

The guarded public crop then reproduces the same `1`/`l` misread under **both** OCR versions without image padding. Adding a white border and decoding only the complete read-only contract line produces the exact 25 bytes. Engine version alone is therefore not the demonstrated cause: field segmentation and padding matter. The new decoder anchors the unique **Exact contract** label in one atomic public capture, scrolls only when the complete line is outside the viewport, and uses native/3× single-line OCR with whitespace-only joining. It supplies no character whitelist or expected-word dictionary. It stops re-clicking an already-open Staking tab.

The committed positive fixture is actual local Ubuntu 26 app pixels; wrong-`l` and wrong-`I` fixtures are explicitly synthetic. Their metadata and hashes are in [near-public-contract.json](../tests/e2e/fixtures/near-public-contract.json). Both decoders reject the wrong images; 72 additional synthetic near-misses reject as well. Genuine mono-font false negatives remain failures, never character substitutions. On a mismatch, only the small guard-checked public contract crop is retained, bound by SHA-256 and uploaded through the narrow `wails-*-near-public-contract.png` artifact path. Private-window capture failures produce no image.

Local focused and complete 20-step GUI runs pass using the extracted official Ubuntu 24 OCR runtime. This checks that decoder against local rendering; it does not establish Ubuntu 24 WebKit/font rendering or a new passing hosted run. Source-specific results and pending gates remain in [STATUS.md](STATUS.md).

## Mutation and navigation repair — 2026-10-28

[Run 40683603591](https://github.com/neodix42/crypto-wallet/actions/runs/40683603591) at `59e063f` passed the current unit/frontend/helper step and Go security units. Four mutation campaigns then hit their unchanged 120-second deadline: recovery authorization, recorded-phrase confirmation, nonce reservation and ambiguous-payment reservation. The report marks them **INVALID**, not killed or passed. Each campaign reran the complete signer suite; the unmodified suite takes 68 seconds on this development machine. The old timeout handler discarded partial test output, so the hosted report cannot identify the exact test running at expiry.

A two-CPU diagnostic of the broad recovery mutant observes its intended security failure, completes 186 passing and 17 failing tests, then hits Go's diagnostic 110-second deadline in an unrelated receive/QR test. This is diagnostic evidence of excessive campaign work, not a passing mutation. The four repaired campaigns together take 42.950 seconds under the same two-CPU restriction; all twelve individual control/mutant executions stay below 7.077 seconds.

The Wails job passed real startup and generated-phrase setup/verification, then timed out under the broad “Open actual NEAR wallet Staking page” stage. That stage covered several different clicks and waits. Original-source targeted staking and a CI-like font probe pass locally; these results do not reproduce or explain the original hosted timeout. The repair adds precise navigation stages and bounded retries restricted to read-only NEAR Wallet/Staking navigation, requiring the actual target page and full validator contract. Private-window capture guards and all staking assertions remain required.

An intermediate stricter detector rejected the genuine wallet heading. Guarded, in-memory public OCR identified the actual same-line words `NEAR mainnet · NEAR`, with the visual middle dot read as a dash. The final detector preserves those exact words and network while ignoring punctuation; cross-line, different-network and wrong-wallet matches fail. Its actual CI-font staking run passes all five steps with stable sources. The intermediate failure is retained separately; no private pixels or raw phrase/credential text are retained.

The mutation repair binds the four guards to existing invariant tests, first requiring passing unmodified controls and then the exact security assertions from each mutant. Nonce and ambiguity guards retain both journal and signer evidence. Timeout, panic, compiler failure, missing assertion or skipped control cannot pass a guarded campaign. This avoids repeating unrelated production-KDF fixtures for every mutant without increasing the deadline or changing application behavior.

The [current evidence checklist](STATUS.md) separates local validation from the failed historical run and the next hosted execution. Legacy Electron tests and coverage collection remain local-only; build/publication are still separate.

## Hosted scope update — 2026-10-28

The owner removed legacy Electron tests and Go/frontend coverage collection and enforcement from GitHub jobs. The checks job now uses explicit `test-ci`, `test-security-ci`, `test-race-ci` and `check-deps-ci` targets. These avoid the legacy desktop dependency install, launcher unit suite, GTK input coverage collector, Electron surface tests and Electron build/license fixtures. Shared core/signer protocol tests, semantic security mutants, current Wails startup/appearance guards and current React tests remain hosted, alongside integration, interoperability, fuzz and recovery checks.

The Wails GUI job retains its real uninstrumented application checks, disposable profiles, namespace guards and always-run cleanup. The one-file archive still rejects Electron payloads; that verifies the current package. Build/publication remain separate from tests. The checks job no longer loads an unused desktop test profile.

Local `make test`, `make test-security`, `make test-race`, `make check-deps`, explicit Electron targets and `make coverage` remain available. Local coverage thresholds are unchanged. Prior coverage and Electron results below are historical evidence for their recorded commits; removing a hosted gate does not pass it. See [the current evidence checklist](STATUS.md).

## Historical status expiry observation repair — 2026-10-28

Run [40653531321](https://github.com/neodix42/crypto-wallet/actions/runs/40653531321)
passed the complete Wails job, security/race/interoperability/integration/fuzz
checks, Go/frontend coverage, dependency checks, recovery and crash checks. Its
remaining failure was the retained Electron Unlock layout observer. It recorded
an absent status message at 10.194 seconds against an 8.5-second limit, after
running two full-window OCR passes. That timestamp measured decoding completion
and cannot establish when the app actually cleared the message. The subsequent
legacy payment step was skipped after this failure.

The observer now captures only the public 52-pixel status bar at 6.8 and
8.0 seconds, before either OCR call. It records capture intervals and OCR
completion separately. The first capture must finish before 7.0 seconds and the
second before 8.5 seconds; late captures still fail. A visible cancellation must
be recognized before expiry, and only the static `Status` label may remain
afterwards. Blank or garbled OCR cannot prove expiry. An atomic guard on the
owned X server rejects private/unexpected windows and geometry drift before
pixels are read. No production timer, workflow assertion or coverage floor
changes.

| Status | Scope |
| --- | --- |
| PASS | 22 startup/timing regressions, including slow OCR, genuine early/persistent messages, unreadable text, late captures, private prompts and capture-error cleanup; seven existing real-OCR regressions, 45 release-evidence integrity checks and all 163 retained desktop tests. The exact 7,000 ms timer assertions remain enforced. |
| PASS, actual local UI | Fresh fixture restore, empty Unlock geometry/wheel checks, no status outline, visible cancellation at 6.812 seconds and absent at 8.014 seconds; unchanged encrypted vault, canary checks and complete disposable-profile/display cleanup. |
| PASS, actual delayed OCR | Same uninstrumented app and real Tesseract, with a test-only 3.4-second delay per status decode. Captures finish at 6.811/8.013 seconds; decoding finishes at 15.032 seconds. The check passes using capture times, with no mocked pixels or text. |
| PASS, scoped local payment | The previously skipped regression executes one real approved payment on isolated pinned geth; all seven receipt/balance/fee/nonce and canary checks pass. Only public genesis-funded fixtures are used. |
| NOT_RUN at this source checkpoint | Revised hosted workflow after commit/push. Earlier passing gates and local UI results do not establish a passing revised workflow. |

Generated reports are `reports/ci-unlock-layout-fixed.json`,
`reports/ci-unlock-layout-slow-real-ocr.json` and
`reports/ci-unlock-remaining-legacy-send.json`. The failed hosted evidence is
retained separately under `reports/hosted-4ae8a144/checks/`.

## Historical Wails coverage prerequisite — 2026-10-28

The [reported test run](https://github.com/neodix42/crypto-wallet/actions/runs/40632779639) passed its Wails application job, but the separate checks job failed while starting the instrumented Wails coverage workflow: `Private network namespace preserving ordinary UID unavailable`. The hosted Ubuntu runner denied the coverage driver's user/network namespace. The temporary CI permission was configured only after the combined test-and-coverage step, so that step could never inherit it. This was a test-infrastructure ordering error, not evidence of a wallet startup failure or a passing coverage gate.

At that source checkpoint, coverage had its own named step, after `scripts/ci-desktop.py setup`, and ran through the exact allowlisted command `python3 scripts/ci-desktop.py run -- make coverage`. Unit, security, race, interoperability, local integration and fuzz checks retained separate named steps. The same permission was available to the then-retained Electron regression checks, with an `always()` cleanup before artifact upload. The owner scope update above removes those hosted coverage and Electron steps. Build/publication remain separate and load no profiles or tests.

This named profile permits only the owned test namespaces required to keep disposable wallets away from public networks. It does not change the installed application's confinement, load policy on a developer machine, grant namespace permission to unrelated host processes, or alter a kernel setting. The portable Wails application still uses the owner-requested unconfined Go roles and explicitly disabled WebKit renderer sandbox. The helper rejects arbitrary commands and additional Make arguments.

| Status | Scope |
| --- | --- |
| PASS | 21 offline CI-helper regressions, including actual offline parser compilation, explicit coverage argv, setup/coverage/always-cleanup ordering, failure propagation, and rejection of additional Make arguments. No kernel policy was loaded locally. |
| PASS | Pinned actionlint v1.7.12 accepts all three workflows. |
| PASS, hosted at `4ae8a14` | Changed namespace/coverage wiring on a fresh GitHub-hosted runner: all 33 Go package floors, four real execution gates and frontend floors pass. Downloaded raw source/profile/runtime evidence was independently validated; the later legacy Unlock observer failure is recorded above. |

Run `python3 scripts/ci-desktop-test.py` locally; it never invokes sudo, loads a host profile or starts a wallet. The actual CI-only setup/run/cleanup commands intentionally reject local developer environments. Source-bound results are written to `reports/ci-desktop-tools.json`.

The combined local campaign also reproduced a separate public-pixel race during
address copying. A WebKit hover tooltip and one legitimate private window both
temporarily used GTK's `cryptowallet` title. Failure-only X11 metadata established
that the tooltip belonged to the public PID, had tooltip type/override-redirect
and was transient for the public window; the normal private window had a
different PID. The driver now moves the pointer to empty space after the
explicit read-only click. It still rejects unknown or multiple private windows,
and never sends private/loading pixels through public OCR. Three actual Decred
copy cycles and ten startup regressions pass. The `--decred-copy-only` and
`--tokens-only` modes label their evidence as partial; neither substitutes for
the full coverage/recovery campaign.

Hosted commit [`e014505`](https://github.com/neodix42/crypto-wallet/commit/e014505)
subsequently passed the full 18-workflow uninstrumented Wails GUI campaign with
stable sources in 19 minutes 25 seconds (07:12:09–07:31:34 UTC). The coverage
collector's former 15-minute subprocess limit was below this measured successful
duration. Its full instrumented GUI subprocess now has a 40-minute overall
deadline; the existing per-action timeouts and all acceptance checks remain
unchanged. A passing ordinary GUI run does not pass instrumented coverage. Both
were subsequently verified in hosted run `40653531321` at `4ae8a14`; its later
Unlock observer failure remains a separate failed check.

## Historical Electron startup diagnosis — 2026-10-27

The reported [release job](https://github.com/neodix42/crypto-wallet/actions/runs/40467178485/job/122065301167) built, verified and installed version 0.1.1, then failed before any mapped LocalWallet window. Its uploaded evidence contained an empty desktop and failed report but no launcher log. The remote report therefore does not establish the precise Chromium error.

Locally, starting the same current artifact through `aa-exec -p unconfined` reproduces that missing-window failure. The captured pre-input log shows Chromium's fatal sandbox-helper error because user namespaces are unavailable. Starting the same artifact under the existing IDE profile, which permits user namespaces, succeeds on the private X11 display. This supports the Ubuntu sandbox-prerequisite diagnosis; the changed GitHub job still needs to run to confirm remote resolution.

Ubuntu documents its user-namespace restriction and application-specific AppArmor `userns` profiles in the [24.04 release notes](https://documentation.ubuntu.com/release-notes/24.04/). Local testing previously inherited an IDE allowance, which failed to exercise an otherwise unconfined runner. The old packaged driver also discarded startup stderr. Both gaps are now explicit.

The test CI workflow installs `apparmor-utils`, loads a named temporary kernel profile containing only the documented `userns` permission, and starts the packaged checks explicitly under it with `aa-exec`. The profile has no path attachment or wildcard: other processes receive no new permission. The helper requires a non-root GitHub-hosted Linux runner and the matching owned repository workspace. It checks the profile's exact name/mode before execution, permits only fixed desktop test commands and removes the profile with an always-run cleanup step. The independent release workflow performs no GUI tests or profile loading. Kernel profile inventory reads use a fixed privileged command with output/time bounds. Policy files are private temporary files; parser caches and persistent `/etc` policy are not written.

Chromium's sandbox and the launcher's seccomp remain enabled. No system-wide namespace setting, setuid bit or developer/user host configuration is changed. Local `make install` and `make test-packaged` do not load this profile. Hosts that lack the required sandbox permission still fail closed.

The packaged recovery driver now detects exit before the public window and retains at most 16 KiB of startup diagnostics, only before any credential or phrase entry. It does not retain later secret-bearing workflow logs. The existing standalone `scripts/display-startup.py --backend x11` probe also records fresh-profile startup diagnostics.

## Evidence checklist — 2026-10-27

| Status | Scope |
| --- | --- |
| FAIL — intentional reproduction | Current installed artifact under an otherwise unconfined AppArmor context: no public window and Chromium sandbox fatal captured before secret input. `reports/phrase-backup-ci-unconfined.log`. |
| PASS | Same artifact starts on an owned private X11 display under the existing userns-enabled IDE profile. `reports/phrase-backup-ci-allowed.log`. This does not execute the new CI profile. |
| PASS | **20 offline CI-helper regressions**, including actual parser compilation without loading policy, bounded inventory reads, CI-only/workspace/command guards, failure propagation and cleanup wiring. **42 existing build-tool tests** and pinned workflow lint pass. |
| PASS | **Three** startup-diagnostic regressions, including a real owned failed subprocess, bounded timeout/log size and no workflow-log capture after successful startup. |
| PASS — hosted startup prerequisite | The subsequent [CI run](https://github.com/neodix42/crypto-wallet/actions/runs/40478173873/job/122101481249) and [release run](https://github.com/neodix42/crypto-wallet/actions/runs/40478173969/job/122102282356) loaded the named profile, started the sandboxed wallet and passed the first fourteen packaged checks. Both always-run cleanup steps removed the profile successfully. Their later tooltip OCR failure is separate; neither complete job passed. |
| NOT_RUN | Full clean supported OS/display acceptance and independent security review. The private X11 probes do not establish those gates. |

Run `python3 scripts/ci-desktop-test.py` and `python3 scripts/desktop-startup-test.py` for the local offline regressions. Workflow invocations of `scripts/ci-desktop.py setup/run/cleanup` are intentionally restricted to hosted CI. Source-bound tool evidence is in `reports/ci-desktop-tools.json`; [STATUS.md](STATUS.md) retains the broader release gates.

The [Actions failure checklist](CI_ACTIONS.md) records the later hosted tooltip failure and its pixel regression. Historical red runs retain the result of their original source commit; local fixes do not change those results.
