# Deterministic-account recovery and error dialogs

Recovery previously skipped existing network/index slots, then passed an empty
addition list into code that rejected it. An existing-only range therefore
failed, while stored hidden wallets stayed hidden. The response collapsed into
generic status text and concealed the cause.

Settings → Wallet now accepts a canonical starting index and a count of 1–20,
with every index below 1,000,000. The signer confirms the inclusive range,
derives missing active-network slots, restores visibility and removes matching
archived-group markers. Repeating an already visible range succeeds without
rewriting either encrypted vault generation. Derivations are staged before any
change; failure does not leave partially recovered accounts. A failed storage
commit retains the existing durable-recovery/lock behavior.

Stored TON profiles, named NEAR or rotated Aptos descriptors, labels, keys,
original signed transaction bytes and reservations are preserved. Recovery
performs no blockchain scan or payment. A phrase alone cannot reconstruct
installation-local names, associations, credentials or operation records.
The existing legacy Add account duplicate-rejection policy remains separate.

Every foreground operational failure uses a main-window error dialog with its
operation, fixed error code and complete reviewed reason. It remains until
acknowledged. Distinct queued failures retain separate identities; stale
acknowledgements cannot dismiss a later error. Real input/approval prompts take
priority and queued errors resume afterwards. Lock clears previous-session
diagnostics. Explicit `REQUEST_CANCELLED` outcomes are informational. Inline field validation and
unknown/unavailable balance/history states remain beside their data; they are
not successful or zero-balance observations.

The status bar shows progress, completion, cancellation and other informational
feedback only, with its existing seven-second timeout. Errors never authorize
automatic retries. Typed storage, authentication, TLS, timeout, provider HTTP/API
and recovery failures carry safe reasons through signer/core IPC. Raw exception
messages, secrets, paths, credential-bearing URLs, response bodies and stacks
are not copied into dialogs or logs. An unclassified failure is explicitly
identified as such. The signer no longer asks the user to dismiss a duplicate
notice before the main UI can show the structured error.

## Evidence checklist

All local inputs use disposable generated roots or public fixtures. No real
user profile, user secret or automatic mainnet payment is used.

| State | Scope and evidence |
|---|---|
| PASS | Final full signer and walletsigner race suites; staged derivation/storage failures, no-op ciphertext stability, archived visibility, preserved profiles/names/signed UNKNOWN bytes and safe cancellation. `reports/account-recovery-backend-checklist.json`. |
| PASS | Actual coordinator and signer subprocess recovery, bounded confirmation, invalid-range codes, metadata cancellation, encrypted restart and original signed-operation preservation. `reports/account-recovery-actual-process-race.log`. |
| PASS | Native controller/bridge/prompt races: persistent error queue, exact acknowledgement, bounded contracts, typed reasons, canary redaction and informational-only status. `reports/account-recovery-error-native-final-race.log`. |
| PASS, scoped | Typed IPC, transport and provider classification, including malformed numeric API data and response-body canaries. `reports/public-error-category-checklist.json`; its recorded core run precedes the final notification follow-up. |
| PASS | 153 frontend tests and project TypeScript: escaped complete dialogs, focus, prompt/reply priority, clipboard/bridge failures, stale acknowledgement and queue bounds. `reports/account-recovery-frontend-final.log`. |
| PASS | 214 graphical-helper tests and portable build/managed installation. `reports/account-recovery-cancel-helpers.log`, `reports/account-recovery-cancel-build.log`. |
| PASS | Final `make test-ci` aggregate, supervisor races and vet. `reports/account-recovery-final-unit-v5.log`, `reports/account-recovery-supervisor-race.log`, `reports/account-recovery-final-vet.log`. |
| PASS, scoped GUI | Existing and archived recovery, persistent complete invalid-range dialog, cancellation and encrypted restart: six stable-source checks on binary `b0bd995677c29cb0a4ca460397e648da98444025852fbd6afaf5d0d411187eb9`. `reports/account-recovery-dialogs-final.json`. |
| PASS | Final local uninstrumented full workflow: 35 source-stable checks on binary `6aa6ee8d74d1c31d030b1d600fa19e14c60c8357ed97ab553a01a9a9ed322243`, including same-wallet recovery from an independent app copy, explicit offline RPC/address-book errors, persistent invalid-range details, original recovered identity, completed Bybit cancellation and one authentication dialog after a wrong password. `reports/account-recovery-full-v3.json`. Five earlier local TON checks on `b0bd9956…` remain scoped evidence; final exact-source TON acceptance is hosted below. |
| PASS | Exact-source hosted desktop job 125208821140/run 41424837883: 35 graphical, five TON, seven recovery and nine actual private-geth steps. Artifact 13132737132 digest/source/binary bindings verify. `reports/account-recovery-hosted-gui-v5/summary.json`. |
| PASS | Normal release 0.1.41/build 41424837829: authenticated publisher, exact source `aea16d899ee233a5dc3d56570c0cecc7e5d4e198`, tag/version, four-file archive inventory and published-byte checks. Seven stable-source GUI checks pass on that exact downloaded executable. `reports/account-recovery-release-final-v5/summary.json`, `reports/account-recovery-published-v5.json`. |
| PASS | Both jobs in [run 41424837883](https://github.com/neodix42/crypto-wallet/actions/runs/41424837883), attempt 1, succeed for exact source `aea16d899ee233a5dc3d56570c0cecc7e5d4e198`. Backend job 125208821059 passes units/frontend/helpers, full races/stress, independent interoperability, 81 semantic mutants in ten campaigns, 13 actual local integrations, 26 executed fuzz targets, signed USDT execution, dependency checks, 24-descriptor fresh-copy recovery and crash/secret canaries. Artifact 13132907444 SHA256 `11079adacd22b8f370946d9b5da6a00a3f6950c04784345fed2118077bd0e981` verifies: all 30 JSON gates pass, all 17 declared stable snapshots are true, and all 1,651 reported source paths and producer references match exact Git objects without mismatches. `reports/account-recovery-hosted-checks-v5/verification.json`. Earlier failed/superseded runs remain distinct below. |
| NOT_RUN | Funded public transactions, live exchange custody operations, full installed physical OS/Wayland matrix and independent security review. |

The first expanded native race run caught a TON-specific error-context
regression: generic transaction text replaced the profile-recovery explanation.
The corrected dialog retains the operation-specific reason and safe category;
the strengthened regression also requires an empty error status bar. The failed
development run remains at `reports/account-recovery-error-final-race.log` and
is not passing evidence.

The first hosted security job reached and passed its semantic mutation campaigns,
then rejected new public account metadata because the separate DTO allowlist had
not been updated. The correction explicitly reviews `presentation` and
`nextAccountIndex`, their concrete types and the nested three-field label/archive
schema. Negative policy checks reject secret fields and changed byte-array/vault
types. The policy regression suite and real Go/React surface check pass locally.
`reports/account-recovery-surface-policy-tests.log`,
`reports/account-recovery-surface-policy-runtime.log`. The old run's later gates
were not executed; it was superseded after the correction was pushed. No runtime
or secret-handling requirement was weakened.

The next hosted GUI run exposed a consent-cancellation defect: declining Bybit
connection approval returned `REQUEST_DENIED`. The previous local driver could
return during the brief gap between the closing prompt and the following error,
so that earlier passing checkpoint did not prove completed cancellation. The
new driver requires the final `Request cancelled.` informational feedback and
fails on a following error dialog. Running it against authenticated release
0.1.38 reproduces the defect with `REQUEST_DENIED` (expected FAIL control,
`reports/account-recovery-cancel-old-binary.json`, stable sources).

The corrected signer returns typed cancellation only when the original
credential/session guards still hold. Changed sessions remain denials; declined
consent writes no credentials. All three providers pass framed category,
ciphertext and secret-redaction tests. The strict actual Appearance/Bybit flow
passes three checks on binary
`6aa6ee8d74d1c31d030b1d600fa19e14c60c8357ed97ab553a01a9a9ed322243`,
with stable sources (`reports/account-recovery-cancel-fixed.json`). Focused
signer races, final walletsigner framed races, vet, all 214 graphical helpers,
`make test-ci` and four recompiled semantic controls pass. Earlier development
framed-test attempts used a nonexistent fixture field and a testnet-only unlock
helper for a mainnet fixture; those failures are retained separately, and the
final test uses explicit mainnet dispatch without changing any protocol policy.
The final current full graphical campaign passes 35 source-stable checks;
both final hosted jobs pass as recorded in the table above.

The subsequent hosted private-geth drill still expected the former generic
error for an unrecorded preview. The wallet correctly returned typed
`REQUEST_DENIED`; no authority was granted. The corrected drill requires that
exact code, a null response body, no authorization/password prompt, unchanged
ciphertext/journal/balances/nonces, then one explicitly approved real private
transaction and restart recovery. New fast driver controls distinguish a wrong
category from actual authority or a response payload. All nine actual geth steps
pass locally with stable sources (`reports/account-recovery-portable-send-final.log`),
as do the independent owned-process crash/security drill and secret canaries
(`reports/account-recovery-crash-final.log`). No public-chain payment is used.
