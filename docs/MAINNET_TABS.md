# Mainnet profile and wallet tabs: implementation evidence

The complete desktop now starts in mainnet mode. Its Go coordinator and signer use a separate mainnet profile; existing testnet ciphertext is preserved and remains accessible with `./wallet --profile=testnet`. No profile is copied, reclassified or automatically initialized with a new root. Wallet creation/import/restore still requires an explicit action and native credential input.

Each deterministic account group has a top tab, starting with Main account. Its page contains every configured network wallet: nine mainnet networks and eleven assets across all six adapter families. Each card has a one-line address with ellipsis/full tooltip and a copy icon. Copying passes only the captured account ID to Go, which independently derives and displays the authoritative address before copying it.

Open wallet creates or selects a wallet tab. Its page shows that wallet's selected-asset balance, native gas balance where relevant, Send/Receive controls, provider configuration and scoped history. Balance formatting uses integer strings and registry decimals, including values beyond JavaScript Number precision. A missing or failed result remains unknown. The journal filters account ID, network and asset; changing tabs or assets never cancels durable reservations, retries a payment or sends automatically. Form/QR/preview state is cleared on selection changes, and late responses cannot populate another wallet's tab. Lock clears all tabs and cached public data.

Mainnet receive/copy and signing retain verified-backup requirements and native authorization. Mainnet connectivity is separate from production acceptance: this change does not complete the funded-network, installed-OS, 24-hour or independent-review gates.

## Evidence checklist

| Check | Result | Executed scope |
|---|---|---|
| Default mainnet and explicit testnet process profiles | PASS | Actual launcher bootstrap and strict duplicate/invalid-option tests; locked snapshot identifies its profile |
| All-chain initial wallets and encrypted recovery | PASS | Real OS-random mainnet root, nine network descriptors, all six families, encrypted backup and fresh restore; testnet registry remains twelve networks/fourteen assets |
| Existing mainnet data stays separate from testnet | PASS | Uninstrumented packaged launch under one disposable config root: explicit testnet is empty, existing mainnet ciphertext unchanged, default restart returns to locked mainnet |
| Top account tabs, card copy icon and opened-wallet page | PASS | Actual packaged native pointer/key input; additional account at index one; native copying matches independent EVM fixture; wallet Receive opens authoritative native confirmation; Send focuses its own form without preparing/broadcasting |
| Cross-wallet data and asynchronous isolation | PASS | 46 component tests, zero skips; exact account/network/asset journal filtering, delayed balance/history/receive/plan responses, view invalidation and session-bound credential refresh |
| Single-line addresses and visible controls | PASS | Actual portable screenshots visually reviewed; CSS enforces nowrap/ellipsis and full address tooltip; full native copied address matches independent fixture |
| Mainnet backup eligibility at each signing boundary | PASS | Actual metadata change revokes a prepared plan's eligibility before native input; confirmation/reauthentication revocation also refuses a signature; a fresh verified backup restores eligibility |
| Decred settings belong to the selected wallet | PASS | Two-account regression and independent public-fixture reproduction; saved values loaded on selection, cleared on lock |
| Go/native coverage | PASS | All 22 shipped package floors, source-stable 49.186-second run; local dcrd simnet and TON emulator executed; optional skips recorded |
| Frontend coverage | PASS | All five shipped JS files: 100% statements; four critical files: 100% branches; renderer: 98.54% branches; existing floors unchanged |
| Race and security mutation suites | PASS | Full Go race suite; all eleven compiled critical mutants killed, including removal of mainnet backup authorization |
| Packaged recovery and rejected-input flows | PASS | Twenty uninstrumented private-X11 checks, production KDF, startup/backup/restart/fresh restore and tested persisted/log canaries |
| Mainnet provider identity and public metadata | PASS, limited scope | 29 read-only checks across eight public mainnet domains, canonical ZEN code/decimals and selected fee/protocol methods; no addresses/secrets/transfers in that probe |
| Authenticated live Decred connection | BLOCKED | Operator must configure a synchronized authenticated TLS dcrd endpoint; its wallet remains present with connection-required guidance |
| Private X11 and virtual Wayland startup | PASS, limited scope | Installed runtime, mapped X11 screen and live Wayland process; no live Mesa loader warnings; native Wayland workflows not established |
| Build tools, vet, workflow lint and dependencies | PASS | 37 build-tool tests; module verification and vet; workflow lint; no reachable Go vulnerability or high npm finding |
| Full installed OS/Wayland/lifecycle matrix | NOT_RUN | A private display is not a clean supported OS installation |
| Funded mainnet transactions | NOT_RUN | No automatic mainnet send occurred; packaged Send testing stops at opening the form |
| Public testnet roundtrips, third-party wallet applications, 24-hour workload and independent security review | NOT_RUN | Existing production acceptance gates remain incomplete |

Compact evidence is retained in [mainnet-tabs.json](evidence/mainnet-tabs.json), [RPC probes](evidence/mainnet-rpc.json), [packaged recovery](evidence/packaged-recovery.json), [Go coverage](evidence/go-coverage.json), [frontend coverage](evidence/frontend-coverage.json), [mutation results](evidence/mutation.json) and [display startup](evidence/display-startup.json). Reports retain exact runtime identities, source/profile/log hashes, public/masked screenshots and detailed commands. Prior reports retain their own source-bound scope and are not recertified by this change.

Two packaged harness attempts failed after successful product steps because OCR treated tab/button borders as text. Their reports/screenshots are retained under `reports/mainnet-packaged-initial.*` and `reports/mainnet-packaged-ocr-controls.*`. The harness now matches complete label tokens and separately normalizes primary-button contrast; the final run passed all twenty checks without changing production code or adding a debug/automation bypass.

Reproduce with `make coverage`, `make test-race`, `make test-security`, `make check-deps` and `make test-packaged`. The packaged drill defaults to a disposable mainnet profile; `python3 scripts/packaged-recovery.py --profile=testnet` explicitly selects its developer variant. Opening the packaged wallet can query its configured RPC with disposable public fixture addresses. These commands never use real user secrets or automatically send mainnet funds. Provider setup and Decred scan/TLS limits are documented in [MAINNET.md](MAINNET.md).
