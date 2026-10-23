# Password and backup failure analysis

The reported failures had two independent causes. A three-byte password is below the existing 12–4096 UTF-8-byte policy; arbitrary three-character text is also not a valid English BIP39 recovery phrase. Both should have produced useful guidance. Instead, signer errors were collapsed to a generic denial, then to a generic core failure. Electron rejected its IPC handler and logged a stack trace; the renderer's vague status message could be offscreen and could be cleared by locked-status polling. The absence of a readable error was an application defect.

Backup used the live vault's directory manager, which requires a `0700` profile and creates `writer.lock`. This incorrectly rejected ordinary owned `0755` folders selected through the file dialog. Portable encrypted files now use a separate anchored directory API. Existing owned folders that other users cannot modify are accepted; every path component must be free of symbolic links, and backup files remain regular single-link `0600` files. The operation creates no directory or lockfile and changes no folder permissions. Atomic no-replace publication and reopening/decrypting/deriving the backup remain required. The live profile still requires `0700` and retains its writer lock.

## What the earlier tests missed

The earlier backend tests verified rejection of invalid input without requiring an actionable visible message. Frontend tests explicitly expected generic failure text, so they certified the defective behavior. Backup success fixtures used `0700` directories, while a negative test required `0755` rejection. The actual packaged recovery drill covered eight successful steps and never selected an ordinary folder or submitted invalid native credentials. Component Electron doubles also did not establish how the real context bridge transports Error objects.

Those were errors in test selection and expectations. High statement/branch coverage did not establish usable failure handling. The revised tests require specific safe categories and visible guidance, and the production packaged drill exercises both rejected requests and recovery through actual native dialogs and Electron context isolation.

## Resulting behavior

Only a fixed allowlist of value-free error categories crosses signer, supervisor and core boundaries. Raw errors, input values and selected paths never become public error text. The signer displays fixed guidance after rejected input is cleared, while native inactivity handling remains active. Electron resolves ordinary failures with a schema-checked result envelope; preload turns the safe code into an Error message, which survives the actual context bridge. The renderer translates only those exact codes into fixed, focused alert text. Repeated locked polls preserve that alert; the next action clears it. Invalid origins/schemas and transport failures retain their fail-closed handling. Errors never trigger an automatic transaction retry.

Selecting an existing backup no longer offers GTK's misleading Replace confirmation. It reports that a new filename is needed, preserves the existing bytes and leaves the active wallet usable. Unavailable/unsafe destinations, write failures, failed verification and unsafe restore sources have distinct fixed guidance.

CPU rendering is selected before Electron readiness using [Electron's documented API](https://www.electronjs.org/docs/latest/api/app#appdisablehardwareacceleration), with sandbox and network restrictions retained. This avoids unnecessary hardware-driver initialization. The exact host Mesa permission warning was **not reproduced**, so private-display results do not establish the cause of that hardware-specific diagnostic.

## Evidence checklist

All inputs were disposable generated roots or public fixtures. No real user secret or automatic mainnet send was used. The uninstrumented packaged test records the installed Go binary and ASAR hashes, uses an owned private X11 display and drives native input using key/button events. It uses no credential arguments, credential environment variables, secret clipboard transport, debug port or sandbox bypass.

| Check | Result | Evidence |
|---|---|---|
| Actual packaged rejected short/mismatched passwords and invalid BIP39 input: native notice, focused GUI guidance and no vault creation | PASS | `docs/evidence/packaged-recovery.json`; short-password guidance also survives the next locked poll |
| Actual packaged backup in `0755`, encrypted `0600`, no chmod/lockfile; existing-file and unsafe-folder rejection; wallet remains usable | PASS | Same report; existing ciphertext compared byte for byte |
| Generated and imported root backup, restart/unlock, fresh-profile restore, independent public address and tested log/file canaries | PASS | Current report preserves the original checks and adds mainnet/tab flows; see `MAINNET_TABS.md` |
| No ordinary IPC handler stack traces or Mesa loader errors during the packaged drill | PASS | Same report; logs inspected while each app instance was live, before forced teardown |
| Private X11 mapped screen and virtual Wayland process startup, without live Mesa warnings | PASS, limited scope | `docs/evidence/display-startup.json`; Wayland visibility/native input was not verified |
| Go/native suite, source-stable coverage of all 22 shipped packages | PASS | `docs/evidence/go-coverage.json`, `coverage.json`; existing floors unchanged |
| All shipped desktop JS, 46 component tests | PASS | `docs/evidence/frontend-coverage.json`; 100% measured statements in five files; critical branches 100%, renderer branches 98.54% |
| Full Go race suite and build-tool tests | PASS | `docs/evidence/failure-feedback.json`; exact commands/log hashes retained; 37 build-tool tests |
| Actual host GPU/display reproduction, clean OS recovery/lifecycle and native Wayland workflows | NOT_RUN | Disposable displays do not establish these gates |
| Remote execution of the new CI gate | NOT_RUN | Workflow implementation and local equivalent checked; no remote workflow triggered |
| Public funded network acceptance, 24-hour campaign and independent review | NOT_RUN | Existing production gates remain open in `STATUS.md` |

Reproduce the packaged checks with `make test-packaged`. CI now installs Tesseract and runs that target after its existing checks. `make test-e2e` runs the same drill followed by the separate container probes; their sandbox prerequisites can remain **BLOCKED**. The Go-only release assets remain Go-only. Run `python3 scripts/display-startup.py` separately for the optional disposable display probes. `reports/` retains detailed logs and public/masked screenshots; compact measured results are copied into `docs/evidence/`.
