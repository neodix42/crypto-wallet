# GitHub Actions failure review — 2026-10-27

This is the earlier 17-run review snapshot. The subsequent timing failure and owner-requested independent build/publication workflow are recorded in [RELEASE_PIPELINE.md](RELEASE_PIPELINE.md).

The 2026-10-28 owner update removes legacy Electron tests and Go/frontend coverage measurement/enforcement from hosted jobs while retaining local commands. See [the current CI scope](CI_DESKTOP.md); the results below keep their historical scope.

Reviewed all 17 available repository runs and the logs of all 12 failed jobs. The newest failed CI and release jobs use source commit `d9f6a95f057612978df30e6f0a1a020b6688c3e7`. No user credentials, vaults or mainnet payments are used in this review or its local tests.

| Status | Finding and evidence |
| --- | --- |
| RESOLVED in newer hosted runs | Four October 23 jobs failed because `coverage-test.py` invoked the missing `coverage.py`. Current code invokes `go-coverage.py`; the latest CI and release jobs passed their mandatory coverage gates. |
| RESOLVED in newer hosted runs | Four October 24/27 jobs failed before a LocalWallet window appeared. The named CI AppArmor profile now loads, the sandboxed wallet starts and the profile is removed successfully. See [startup evidence](CI_DESKTOP.md). |
| FIX IMPLEMENTED; verification below | Four October 27 jobs at `b4ac37a1` and `d9f6a95f` passed fourteen packaged checks, then timed out observing the Decred connection tooltip. The [latest CI failure](https://github.com/neodix42/crypto-wallet/actions/runs/40478173873/job/122101481249) uploaded public pixels containing the required `authenticated TLS dcrd node. Balance unknown.` text. Its OCR threshold misread `dcrd` as `derd`. The same failure occurs in the [latest release job](https://github.com/neodix42/crypto-wallet/actions/runs/40478173969/job/122102282356). |
| PASS — existing hosted campaign | [Long fuzz run 40474466032](https://github.com/neodix42/crypto-wallet/actions/runs/40474466032) completed successfully: fifteen selected targets, 300 seconds of fuzzing each. Decred is explicitly excluded by the existing owner instruction; this does not satisfy the full all-chain fuzz gate. |
| REMOVED — owner request | The scheduled soak workflow and its custom runner lint configuration have been removed. At review time, [soak run 40458045602](https://github.com/neodix42/crypto-wallet/actions/runs/40458045602) was queued and three older runs had been cancelled before execution. Removing the workflow does not cancel an already queued remote run or establish passing soak evidence. Manual `make test-soak` remains available. |

The fix preserves grayscale and uses sparse-text OCR at three times the original resolution only for the native tooltip. All three exact text assertions remain required. Ordinary page OCR retains its existing threshold, mode and coordinates. OCR subprocesses now have a twenty-second deadline, and failures propagate. The credential-window exclusion still runs before any capture or decoding.

`tests/e2e/fixtures/decred-public-tooltip.png` is a tightly cropped public tooltip from the failing artifact, with its source run, crop and SHA-256 recorded alongside it. It contains no phrase, credential or private dialog pixels. `python3 scripts/desktop-ocr-test.py` runs the real decoder against that fixture and checks coordinate scaling, region filtering, credential exclusion and failed/timed-out decoder handling. It runs in the existing `make test` gate. No application or sandbox check is skipped, and no production payload changes.

| Status | Verification of this change |
| --- | --- |
| PASS | Five OCR regressions, including the exact hosted failure pixels. Source-bound result: `reports/desktop-ocr.json`. |
| PASS | Forty-two build-tool, twenty CI AppArmor helper, three startup diagnostic and five coverage-tool regressions; pinned actionlint accepts all four workflows. |
| PASS | Twenty-eight release-evidence and five campaign-tool regressions. These checks preserve failure/missing-evidence handling. |
| PASS | All thirty-nine uninstrumented installed wallet recovery/navigation checks, fourteen authenticated phrase-backup checks and eight Unlock layout checks on owned private X11 displays. Reports: `packaged-recovery.json`, `phrase-backup-packaged.json` and `unlock-layout.json` under `reports/`. The changed tooltip check passed with its exact original requirements. The local display uses the existing userns-enabled IDE AppArmor context; this is separate from the hosted profile evidence above. |
| PASS | Seven actual packaged Send checks, including Electron approval/password entry, real isolated geth execution and independent receipt/balance verification. No external network route or mainnet funds. `reports/packaged-send.json`. Packaged production fuses and payload hashes also pass verification. |
| NOT_RUN | The changed source on a GitHub-hosted runner. Push this change to run the CI and release workflows against it. Rerunning an old failed job uses its old source and cannot validate this fix. No push, release publication or remote rerun was triggered. |
| NOT_RUN | Full installed OS/display matrix, 24-hour soak completion and independent review; existing production requirements remain. |

The machine-readable review is `reports/actions-failure-review.json`. `scripts/record-evidence.py` includes it and the OCR report in future retained evidence inventories. Historical failures remain historical results; this checklist does not relabel them as passing runs.
