# Isolated desktop and coverage checks in GitHub Actions

## Current Wails coverage prerequisite — 2026-10-28

The [reported test run](https://github.com/neodix42/crypto-wallet/actions/runs/40632779639) passed its Wails application job, but the separate checks job failed while starting the instrumented Wails coverage workflow: `Private network namespace preserving ordinary UID unavailable`. The hosted Ubuntu runner denied the coverage driver's user/network namespace. The temporary CI permission was configured only after the combined test-and-coverage step, so that step could never inherit it. This was a test-infrastructure ordering error, not evidence of a wallet startup failure or a passing coverage gate.

Coverage now has its own named step, after `scripts/ci-desktop.py setup`, and runs through the exact allowlisted command `python3 scripts/ci-desktop.py run -- make coverage`. Unit, security, race, interoperability, local integration and fuzz checks retain separate named steps. The same permission remains available to the explicitly retained Electron regression checks, and an `always()` cleanup removes it before artifact upload even when a check fails. Build/publication remain separate and load no profiles or tests.

This named profile permits only the owned test namespaces required to keep disposable wallets away from public networks. It does not change the installed application's confinement, load policy on a developer machine, grant namespace permission to unrelated host processes, or alter a kernel setting. The portable Wails application still uses the owner-requested unconfined Go roles and explicitly disabled WebKit renderer sandbox. The helper rejects arbitrary commands and additional Make arguments.

| Status | Scope |
| --- | --- |
| PASS | 21 offline CI-helper regressions, including actual offline parser compilation, explicit coverage argv, setup/coverage/always-cleanup ordering, failure propagation, and rejection of additional Make arguments. No kernel policy was loaded locally. |
| PASS | Pinned actionlint v1.7.12 accepts all three workflows. |
| NOT_RUN | Execution of the changed namespace/coverage wiring on a fresh GitHub-hosted runner; the original red run records the old source. Numerical coverage and other gates remain independently enforced. |

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
