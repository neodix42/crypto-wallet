# NEAR and Decred balance connectivity

The balance-adapter evidence below describes the 2026-10-25 balance candidate. The later [status-bar and wallet presentation update](WALLET_STATUS.md) keeps the backend checks and moves observation metadata into tooltips; current frontend/artifact evidence is recorded separately.

The same former generic error concealed different conditions: NEAR can report that a freshly derived implicit account does not exist on chain, while Decred requires a configured authenticated node and an explicit complete discovery range. A failure never establishes a zero balance.

## NEAR: an uncreated implicit account

Deriving a NEAR implicit address does not itself create its on-chain account. The [official account model](https://docs.near.org/protocol/accounts-contracts/account-id) describes initialization through a native NEAR transfer. The old adapter treated an absent account as a failed balance query.

The corrected adapter reports **`0 NEAR · NOT_CREATED (final state)`** only for its validated canonical implicit account descriptor. It checks the selected chain and recent synchronized head, obtains a fresh final block, and queries the exact account at that block. A structured `HANDLER_ERROR` / `UNKNOWN_ACCOUNT` must identify the same account, height and block hash. Missing fields, wrong identities, stale blocks, ambiguous responses, HTTP failures and timeouts remain unknown. A matching account created between the original query and the final-block query is verified and displayed with its actual balance instead.

For an existing account, the expected key must still have **FullAccess** authority at the same observation. A removed/restricted key never becomes an inferred empty account. Named associations do not use the missing-implicit-account exception. Preparation and signing retain their existing authority requirements. These are checks of trusted RPC responses, not independent consensus proofs. See the [official account and access-key RPC methods](https://docs.near.org/api/rpc/contracts).

The default remains `https://rpc.mainnet.near.org`. This fix does not rewrite saved endpoint settings or silently fall back to another provider. Public providers can fail or rate limit; change providers deliberately through **Configure trusted RPC** if necessary. Consult the [official provider list](https://docs.near.org/api/rpc/providers), and never load-test shared endpoints. No deposit is required merely to display verified absence, and the app never sends funds automatically.

## Decred: configure a trusted node and discovery range

An unconfigured Decred wallet displays **Connection required** in both the account overview and wallet page, with **configure an authenticated TLS dcrd node; balance unknown** guidance on hover, and skips the unavailable balance RPC. Once configured, a failed query remains unavailable; configuring a URL does not establish a balance.

Follow the [complete setup steps](MAINNET.md#decred-trusted-node-setup): use a synchronized mainnet `dcrd` on HTTPS, normally port **9109**, with HTTP Basic username/password credentials and a matching OS-trusted TLS certificate. Limited RPC credentials suffice for balance. A stock node's self-signed certificate needs deliberately verified trust configuration; this wallet has no custom-CA picker or skip-verification option. Enter credentials only in **Configure trusted RPC**. Neither `dcrwallet` nor importing wallet secrets into the node is required. Official references: [node setup](https://docs.decred.org/wallets/cli/dcrd-setup/), [RPC options](https://docs.decred.org/wallets/cli/dcrd-and-dcrwallet-cli-arguments/) and [limited RPC methods](https://github.com/decred/dcrd/blob/master/docs/json_rpc_api.mediawiki).

Under **Network details & public diagnostics**, explicitly confirm the wallet's birthday height and address range. Defaults are birthday **0**, gap **20**, **1,000 addresses per branch** and **10,000 blocks**; budget `0` also means 10,000. The maximum block budget is **2,000,000**, and address discovery is separately bounded. Restored wallets may require much earlier history and more derived addresses; a recent birthday is valid only for a root known to have been newly generated then.

The adapter recomputes sequential full-block scans and the balance request has a **30-second** deadline. Raising the budget does not solve historical mainnet synchronization: persistent incremental synchronization remains deferred. Incomplete scans remain unknown, and a successful balance includes only ordinary confirmed spendable outputs. Maturity and unsupported staking/mixing scripts have separate limits in [DCR/TON](DCR_TON.md). `txindex` is unnecessary for balance but needed by the current mined-transaction reconciliation path; see the [official node flag](https://github.com/decred/dcrd/blob/master/doc.go). This change adds guidance, not a new Decred synchronization implementation.

## Evidence checklist

The current candidate's exact commands, source/artifact identities and results are recorded in [near-dcr-balances.json](evidence/near-dcr-balances.json). Scopes are separate:

| Check | Status and scope |
| --- | --- |
| NEAR missing-account and hostile-response regressions | **PASS** — canonical absent implicit accounts only; stale/mismatched/malformed replies, named accounts, key failures and transport failures do not become zero. |
| Pinned private NEAR sandbox | **PASS** — a fresh missing implicit account reports zero; a real local transfer creates/funds it; the adapter then reads the actual `FINAL` balance. Disposable test material only. |
| Desktop component suite | **PASS** — 135 tests, including missing-DCR connection guidance and skipping those balance RPCs; configured RPC failures remain unknown. |
| Frontend coverage | **PASS** — all eight shipped JS files have 100% statement coverage; renderer branches 98.40%, main 98.41%. Source and runtime hashes are bound to the measurements. |
| Built artifact | **PASS** — `make install`, matching 0.1.0 versions, production fuses and all 12 payload comparisons. |
| Packaged desktop acceptance | **PASS** — 29 source-stable final-candidate X11 checks, including actual Decred overview and wallet-page connection guidance. Public pixels only; no GUI instrumentation or user profile used. |
| Recovery, crash and transaction integration | **PASS** — six fresh-install headless recovery checks, seven owned-Go-process crash/canary checks and seven actual packaged Send checks against an isolated real geth. Separate physical machines and full Electron/OS crash acceptance remain unrun. |
| Go package tests and coverage floors | **PASS** — all package tests and 25 unchanged coverage floors; NEAR 98.03% statements. Near/core race-detector checks pass. |
| Full Go coverage aggregate | **FAIL / BLOCKED** — required pinned dcrd v1.10.9 executable is unavailable. TON emulator and isolated USDT execution gates pass; nine optional tests are explicitly skipped in that aggregate. NEAR sandbox and public read-only cases ran separately as recorded here. |
| Public NEAR read-only observation | **PASS** — explicit probe through the default mainnet endpoint verified an absent public-fixture account at a final block. No public transfer or user profile involved; this is not funded-network acceptance. |
| Authenticated live Decred mainnet balance/scan | **NOT_RUN** — no authorized synchronized trusted-TLS node configured for this campaign. |
| Full historical Decred synchronization | **NOT_RUN / deferred** — bounded recomputed scanning remains subject to the existing work and timeout limits. |
| Funded public testnets, full installed X11/Wayland OS matrix, 24-hour all-chain soak and independent security review | **NOT_RUN** for this change; earlier scoped evidence does not complete those acceptance gates. |

No real user secrets, existing wallet profile or mainnet funds are used by the local regressions. Passing component/sandbox checks do not establish public-network availability, complete historical recovery or production readiness.
