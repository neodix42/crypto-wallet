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
diagnostics. User cancellation is informational. Inline field validation and
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
| PASS | 212 graphical-helper tests and portable build/managed installation. `reports/account-recovery-helpers-final-v2.log`, `reports/account-recovery-build-final.log`. |
| PASS | Final `make test-ci` aggregate, supervisor races and vet. `reports/account-recovery-aggregate-v2.log`, `reports/account-recovery-supervisor-race.log`, `reports/account-recovery-final-vet.log`. |
| PASS, scoped GUI | Existing and archived recovery, persistent complete invalid-range dialog, cancellation and encrypted restart: six stable-source checks on binary `b0bd995677c29cb0a4ca460397e648da98444025852fbd6afaf5d0d411187eb9`. `reports/account-recovery-dialogs-final.json`. |
| NOT_RUN | Final full graphical workflow with the later wrong-password check, TON workflow, exact-source hosted CI and publication; results will be recorded after execution. |
| NOT_RUN | Funded public transactions, live exchange custody operations, full installed physical OS/Wayland matrix and independent security review. |

The first expanded native race run caught a TON-specific error-context
regression: generic transaction text replaced the profile-recovery explanation.
The corrected dialog retains the operation-specific reason and safe category;
the strengthened regression also requires an empty error status bar. The failed
development run remains at `reports/account-recovery-error-final-race.log` and
is not passing evidence.
