# Packaged desktop startup in GitHub Actions

The reported [release job](https://github.com/neodix42/crypto-wallet/actions/runs/40467178485/job/122065301167) built, verified and installed version 0.1.1, then failed before any mapped LocalWallet window. Its uploaded evidence contained an empty desktop and failed report but no launcher log. The remote report therefore does not establish the precise Chromium error.

Locally, starting the same current artifact through `aa-exec -p unconfined` reproduces that missing-window failure. The captured pre-input log shows Chromium's fatal sandbox-helper error because user namespaces are unavailable. Starting the same artifact under the existing IDE profile, which permits user namespaces, succeeds on the private X11 display. This supports the Ubuntu sandbox-prerequisite diagnosis; the changed GitHub job still needs to run to confirm remote resolution.

Ubuntu documents its user-namespace restriction and application-specific AppArmor `userns` profiles in the [24.04 release notes](https://documentation.ubuntu.com/release-notes/24.04/). Local testing previously inherited an IDE allowance, which failed to exercise an otherwise unconfined runner. The old packaged driver also discarded startup stderr. Both gaps are now explicit.

The CI and release workflows install `apparmor-utils`, load a named temporary kernel profile containing only the documented `userns` permission, and start the packaged checks explicitly under it with `aa-exec`. The profile has no path attachment or wildcard: other processes receive no new permission. The helper requires a non-root GitHub-hosted Linux runner and the matching owned repository workspace. It checks the profile's exact name/mode before execution, permits only fixed desktop test commands and removes the profile with an always-run cleanup step before release publication. Kernel profile inventory reads use a fixed privileged command with output/time bounds. Policy files are private temporary files; parser caches and persistent `/etc` policy are not written.

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
