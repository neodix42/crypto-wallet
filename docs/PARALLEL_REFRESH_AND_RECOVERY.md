# Parallel balances, exact TON recovery and pending dialogs

The previous UI queued one wallet balance per background tick. The supervisor
also held its request mutex for each complete RPC-backed call. Those were
implementation choices; independent networks do not require that ordering.

The main wallet now uses one cancellable balance batch with four concurrent
provider workers. Each completed wallet updates immediately. Wallets sharing
the same configured RPC authority remain sequential, and provider-specific
rate limits such as TON Center's host quota still apply. A batch selects at
most 64 owned accounts and retains every configured asset, including unknown
token observations alongside a healthy native balance. Large portfolios use
further bounded batches.

Signer-owned descriptors and approved policies are collected serially before
network workers start. Progress binds the original request, session,
generation, owned account/network and configured adapter revision. Missing,
duplicate, foreign or malformed progress never becomes a zero balance. Writes,
password prompts and signing remain serialized. Foreground work cancels the
read request without killing the unlocked signer; completed observations stay
visible and unfinished accounts return to the refresh queue. Lock, close and
snapshot changes discard late observations.

TON recovery had two distinct problems. A fresh subwallet-42 → subwallet-0
recovery already succeeded in the backend, but the UI kept the old wallet tab
selected. An already stored profile returned a generic denial, including when
that account was hidden. Recovery now confirms and reuses the exact stored
identity, restores its visibility if needed and opens the requested wallet tab.
An already visible profile causes no redundant encrypted-storage write.
The returned public StateInit/address descriptor is validated against the
registered TON profile before selection. Index and profile must be explicit and valid.

Existing accounts, keys, contract parameters and signed operation bytes are
preserved. A wallet-ID change still creates a separate address; recovery does
not deploy a contract, move funds or copy another account's transaction
history. Recovery failures and cancellation have contextual messages instead
of unrelated balance/pending-payment advice.

Unresolved chain operations cannot always be eliminated: a node may accept a
transaction and lose its reply. Deleting that record would make a duplicate
payment possible. The encrypted original records and reservations therefore
remain. The UI now checks saved operations after unlock and when opening a
wallet, then displays them in an ordinary in-window dialog. It shows at most
16 operations per page, with exact original details, explicit selection and
**Later**. Dismissing it changes no operation. Unchanged dismissed states do
not repeatedly reopen; explicit conflict checks can reopen their details.

Selecting an operation rechecks its exact identity against current signer
state before using the existing review/cancel/status/original-byte broadcast
flow. An unsigned preview can be cancelled explicitly without waiting for its
expiry. No new signature, replacement transaction, unsigned cancellation or
rebroadcast occurs automatically. A generic error never proves that signing
did not happen. Newly persisted unresolved outcomes are queued for a dialog, including every
transfer, TON deployment, NEAR staking and token-registration preparation
completion. A lost acknowledgement, invalid returned preview or edited form
cannot postpone discovery until the wallet is reopened. These checks read
only saved public operation state and never infer safe cancellation from an error.
Transaction history remains an inert table.

The inbox reads bounded sanitized local journal pages, with separate global
query positions so it does not invalidate a wallet table's local-history
continuation. It never queries a blockchain merely to populate the list.
Changing wallet visibility cannot orphan its original operation; only the
visibility comparison is normalized, leaving the approved plan, digest and
signed bytes intact. Lock, close and foreground work revoke stale scans/dialog
choices, preserving the existing single trusted Wails window and cancellation rules.

| State | Evidence |
|---|---|
| PASS, TON diagnosis and backend | Real framed encrypted signer and coordinator/private-process tests prove fresh 42 → 0 recovery, visible reuse, hidden cancellation/recovery, restart and unchanged real-signed UNKNOWN bytes. Explicit-index/schema, public descriptor and stale consent guards pass. `reports/ton-profile-recovery-fix/`. |
| PASS, parallel backend | Full model/core/supervisor race and vet, actual blocked HTTP sockets, four-worker/shared-authority limits, per-token unknown observations, queued cancellation and malformed/replayed/incomplete progress tests pass. All eight active networks and nineteen defaults remain. `reports/parallel-balances-core-supervisor-race-final.log`, `reports/parallel-balances-core-supervisor-vet.log`. |
| PASS, independent negative controls | Compiling mutations remove selected-account binding or raise the worker limit. Unmodified controls pass; each mutation fails the expected runtime assertion, with stable sources. `reports/parallel-balances-negative-controls.json`. |
| PASS, native controller | Fast-wallet paint before a blocked provider, foreground cancellation/requeue, stale lifecycle results, exact TON selection and context-specific failures pass under the race detector. The full native suite passes. `reports/parallel-refresh-native-full-race-final.log`. |
| PASS, pending dialogs | Full native/bridge/prompt race suites, 114 frontend tests, TypeScript and bounded prompt fuzz pass. Complete journal paging, table-cursor isolation, explicit unsigned cancellation, Later, changed-state prompting, stale choices and original-ID substitution are checked. Visibility changes preserve original signed records in both directions; all immutable descriptor changes still reject. `reports/pending-dialog-redesign/summary.json`, `reports/parallel-refresh-pending-visibility-final.log`. Another 24 race cases cover every preparation family across success, lost unsigned/signed acknowledgement, UNKNOWN, invalid preview and stale form; full native races pass after those completion hooks. `reports/pending-dialog-redesign/prepare-completion-full-native-race.log`. |
| PASS, local aggregate and build | Complete `make test-ci`, portable Go build and managed installation pass. The final graphical helper suite passes all 168 tests. `reports/parallel-refresh-test-ci-final.log`, `reports/parallel-refresh-build-install-final.log`, `reports/parallel-refresh-desktop-helper-final.log`. |
| PASS, actual TON GUI | Five checks on the uninstrumented, network-isolated Wails binary pass with stable sources. Actual fresh and existing original-profile recovery verify exact network/index/profile and SDK-oracle address through fresh native clipboard observations before confirmation; the requested wallet opens and repeat recovery keeps exactly two TON tabs. Binary SHA256 `0a7f0a251c174a3da752fd5582df6f007dde10eb7868ac80e11ea4dffa35ecd3`. `reports/wails-ton-recovery-final-candidate.json`. |
| PASS, hosted desktop | Exact final source `7acef4b7e1e3217c704cafea018384c7a128ce30` passes [run 41286603695](https://github.com/neodix42/crypto-wallet/actions/runs/41286603695), attempt 1: 29 full graphical checks, 5 TON checks, 7 independent recovery steps and 9 actual private-geth steps. Artifact 13075932096 matches SHA256 `9afda798846472b7885cf6cc0f2808ee3bb4a25095151a10edf7bb54ee0de87a`; all 9 JSON reports pass, runtime-bearing reports bind to binary `ce5d758557eb987f948ac5152a79492abf964322e5e1721aab5570833d283c17`, and reported source fingerprints match checkout. Both TON confirmations verify all four exact native fields before consent; SDK-oracle recovery and existing-profile reuse pass with stable sources. `reports/parallel-refresh-hosted-gui/summary.json`. |
| PASS, complete hosted checks | Both jobs in the same exact-source run succeed on attempt 1: current units/frontend/helpers, 81 compiling semantic mutants across 10 campaigns, full races/stress, interoperability, 13 actual local integration gates, 25 executed fuzz targets, USDT execution, dependency/recovery/crash checks. Artifact 13076227474 matches SHA256 `bbb75e95ec5f0997d9a4a4f4539aedc42115ace4effaa08c466f02536d3b646c`; all 30 JSON reports pass, all 17 declared stable snapshots are true and all 1607 unique reported source hashes match checkout. Recovery checks cover 24 wallets across 8 networks. `reports/parallel-refresh-hosted-checks/41286603695/verification.json`. |
| PASS, publication | Normal [release 0.1.35](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.35), [build 41286603694](https://github.com/neodix42/crypto-wallet/actions/runs/41286603694), tag and executable version match the exact final source. Publisher/source/provenance and published-byte equality are independently verified. One archive contains exactly the Go executable, user installer and PNG/SVG icons; archive SHA256 `4181119e8456dd14c548f42cc79c067e4c59c84bcccc55a748634db57abb3c20`. `reports/parallel-refresh-release-final/summary.json`. |
| NOT_RUN, external gates | Actual graphical pending-operation selection, real user profiles/secrets, funded public transfers, full installed OS/Wayland matrix and independent security review. Tests use isolated local nodes, disposable roots or public fixtures. |

The first native race run caught a fixture reading mutable UI network state;
the result is now frozen before concurrent snapshot replacement. A separate
in-progress pending-inbox fixture failed to exercise its renamed missing-ID
case and was corrected. Neither failed run is counted as acceptance; logs are
retained separately from final passing runs. The first new graphical recovery
run also failed its OCR identity-prefix check. Fresh exact native copies prove
both confirmation identities correct while the OCR prefix is incorrect. The
repaired harness uses OCR only for static-label geometry and independently
checks every copied identity field, without glyph repair or expected-value
geometry. That original failed report is retained as
`reports/wails-ton-recovery-parallel-first-failed.json` and is not acceptance.
The preceding `0d1c983` CI run was superseded and cancelled, not counted as a
passing or failing final run. Its verified release 0.1.34 remains historical;
only final source `7acef4b` and release 0.1.35 qualify the preparation-completion
hooks as well.
