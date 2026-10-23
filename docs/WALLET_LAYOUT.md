# Wallet table, settings and receive/send interactions

The overview uses a semantic wallet table with Network, Assets and Receive address columns. A primary click on a row opens that wallet's tab; Enter or Space works on a focused row. The separate copy-icon button uses the native verified-copy flow and does not open the row. Addresses stay on one line with an ellipsis and full tooltip.

Wallet tabs initially show balances and history. Send reveals that wallet's payment form; Close send form clears its local draft and invalidates pending view replies. Closing a form or wallet tab never cancels a durable preparation, broadcasts, retries or releases a reservation. The journal retains the explicit unsigned-cancellation control. Middle-click or Delete closes an opened wallet tab; deterministic account-group tabs remain. Closing an inactive wallet preserves the active wallet's form, focus and pending replies. Closing the active wallet returns to its account table and discards late view results.

Receive opens the authoritative Go window with its QR code, full independently derived address and an explicit Copy address button. Copying keeps the window open and shows confirmation; Done does not copy automatically. The exact clipboard value is passed separately from the display summary by the signer. There is no duplicate QR embedded in the wallet page. Decred advances and durably saves its receive descriptor once before displaying it; a successful reply updates that descriptor across view changes while lock invalidates the old session. Mainnet backup requirements remain enforced.

The top-right Settings button contains the former sidebar's create/import/unlock, backup/restore, password change, additional-account and recovery-range actions. It opens an opaque modal panel, makes the background inert and traps keyboard focus. Escape, Close or the backdrop dismisses it and returns focus. Credential actions close the panel before opening the existing native input window. Named/rotated account association has its own public-address field in wallet diagnostics, so it does not depend on the hidden send form.

## Verification checklist

| Check | Status | Evidence and scope |
|---|---|---|
| Semantic table, all network rows, one-line addresses, copy without opening a row and keyboard row access | PASS | Frontend suite using actual bundled HTML/JS |
| Send hidden by default; explicit open/close; pending prepare/scan cannot restore a closed form | PASS | Frontend suite; no automatic transaction operation on dismissal |
| Middle-click active/inactive wallet tabs, account tabs retained, pending replies and durable reservations preserved | PASS | Frontend suite including inactive-close during balance/preparation/native receive requests |
| Settings actions, modal focus/inert state, Escape/backdrop closing and lock cleanup | PASS | Frontend suite |
| Native QR/full-address popup and separately bound explicit-copy target | PASS | Per-network signer regression suite and 13 short native tests; optional five-minute native case skipped. Separate 1024×768 GTK allocation proof keeps Copy/Done visible with 100 guidance lines |
| Final source-stable Go/native coverage and race checks | PASS | All 22 shipped package floors; local DCR simnet/TON emulator; full Go race suite and vet. Optional external/local-node skips remain recorded |
| Rebuilt uninstrumented packaged settings/table/receive/send/tab-close flow | PASS | 22 checks with disposable profiles, production KDF and actual installed runtime; displayed QR pixels decoded independently and exact clipboard value checked |
| Full supported OS/Wayland/session-lock matrix and independent review | NOT_RUN | Private-display and component checks do not establish these gates |
| Mainnet funds sent by automation | NOT_RUN | Tests use disposable/public fixtures and do not broadcast mainnet transactions |

The current frontend suite passes all 53 tests, with 100% statements in all five shipped JS files and 98.77% renderer branches. Go/native coverage passes in 49.076 seconds, including signer 96.88% and native UI 99.27%. The [source/artifact-bound report](evidence/wallet-layout.json) records commands, log hashes and limits. Build and install succeeded; the repository-root `./wallet` points to the tested runtime. The first packaged attempt failed at native file-selection timing in the driver; `reports/wallet-layout-first-packaged-attempt.{json,log}` retains that failure, and a fresh run passed all 22 checks after the driver waited for GTK path resolution. No production automation bypass was added.

Run `make coverage`, `make test-native` and `make test-packaged`. Compact source/artifact-bound results are retained in `docs/evidence/`; detailed command logs and public/masked screenshots stay under `reports/`. The earlier mainnet transition evidence retains its own narrower source and runtime identity in [MAINNET_TABS.md](MAINNET_TABS.md). Production acceptance remains governed by [STATUS.md](STATUS.md).
