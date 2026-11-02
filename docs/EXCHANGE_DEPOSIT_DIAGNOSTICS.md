# Exchange tabs and independent deposit checks

The header Exchange icon opens one closable Exchange tab beside the account
names. Switching accounts retains it; selecting the icon again reuses it.
Middle-click or its Close button closes the page and, when active, returns to
the remembered account. Closing a tab does not cancel or repeat any operation.
Wallet subtabs are hidden while Exchange is selected. Lock clears the open tab.

Deposit previously called the complete spot quote path. An exchange balance,
pair, price or trading-fee failure could therefore prevent an otherwise valid
on-chain deposit. Deposit now reads credentials, exact coin/network metadata,
deposit availability/minimum and the network's deposit address. It does not
query spot balances, instruments, prices or trading fees. A spot symbol whitelist
or unavailable pair cannot authorize a different asset or block this independent
deposit preflight. Trading and withdrawal retain their existing separate checks.

The verified network/token contract, exact amount and required TON memo still
enter the ordinary wallet preparation, fee estimation, confirmation, current
password, signing, durable original-operation record and reconciliation path.
Ambiguous routes, suspended deposits, below-minimum amounts, mismatched
destinations/contracts/memos and subaccount-only Bybit keys are rejected before
preparation. A chain receipt does not establish exchange credit.

Read failures carry the exact reviewed stage and a fixed reason or canonical
numeric Bybit `retCode`, rather than collapsing to an unidentified state error.
The wallet-fee stage also distinguishes insufficient native fee/reserve funds,
unavailable estimates and typed network failures before durable preparation.
Dialogs explain credential/region, signature, permission, account-mode, IP,
clock and rate-limit failures. Response messages/bodies, API credentials and
internal exception text never cross the public error channel. A read diagnostic
explicitly distinguishes this failed preflight from an older pending payment.
Failed status verification clears stale connected/quote controls.

The configured key's Contract/USDC-derivative/Convert permissions are not used
for this wallet's unleveraged Spot flow. Bybit reports `SpotTrade`,
`AccountTransfer` and `Withdraw` as distinct permissions. Its deposit-address
endpoint is for the master account. API region, IP restrictions, account mode,
coin/network availability and remote compliance can still prevent an API call.
A spot symbol whitelist and withdrawal-address whitelist serve separate purposes.
No broader permission or automatic retry is requested by this correction.

Official API contracts rechecked on 2026-11-02:
[key information](https://bybit-exchange.github.io/docs/v5/user/apikey-info),
[coin/network information](https://bybit-exchange.github.io/docs/v5/asset/coin-info),
[master deposit address](https://bybit-exchange.github.io/docs/v5/asset/deposit/master-deposit-addr),
[balance](https://bybit-exchange.github.io/docs/v5/asset/balance/account-coin-balance)
and [numeric error codes](https://bybit-exchange.github.io/docs/v5/error).
The actual production Go public client verifies `GRAMUSDT` and `ETHUSDT`
instruments without credentials; this does not verify a private deposit route.

## Evidence checklist

| State | Scope and evidence |
|---|---|
| PASS | Final complete `make test-ci`, 156 React tests, TypeScript/build and portable Go build, including fee diagnostics. `reports/exchange-deposit-tabs-test-ci-final.log`, `reports/exchange-deposit-tabs-build-final.log`. |
| PASS | Final full IPC, all exchange clients, coordinator, native controller and Wails bridge race suites and vet. `reports/exchange-deposit-tabs-race-final.log`, `reports/exchange-deposit-tabs-vet.log`. |
| PASS | Inherited-pipe coordinator/adapter fixtures: restricted balance read retains `BALANCE`/10005; ETH and GRAM deposits perform no trading reads and retain one original wallet payment, exact amount/destination and TON memo. Every preflight failure has its exact reason and zero preparations/submissions. This is protocol fixture evidence, not live exchange or chain execution. `internal/walletcore/exchange_deposit_preflight_test.go`, `reports/exchange-deposit-tabs-focused.log`. |
| PASS | Canonical numeric-code and fixed-stage parsing rejects injected text, nesting, overflow, unknown stages and untyped spoofed errors. Native dialogs retain reasons and exclude secret/body canaries and error status text. `internal/ipc/exchange_errors_test.go`, `internal/nativewallet/exchange_errors_test.go`. |
| PASS, scoped GUI before fee-diagnostic follow-up | Five actual uninstrumented X11 checks on binary `d942d35f67849845f99b141900d4ce66bf863be5a3ac46f27343fa6cddb2abde`, stable sources: Appearance/Bybit credential layout and explicit cancellation, one account-row Exchange tab, navigation/reopen, middle-click and keyboard Close. The later backend fee classification does not change this React layout; final exact-source hosted GUI remains separate below. Only disposable public roots/canaries, no private service credentials or funds. `reports/exchange-deposit-tabs-appearance.json`. |
| PASS, public read only | Production Go client verifies exact GRAMUSDT/ETHUSDT metadata at the official global endpoint. No wallet or account request. `reports/exchange-deposit-public-market.json`. |
| PASS, before fee-diagnostic follow-up | Stable-source full security and semantic mutation campaign. Final updated-source results remain separate below. `reports/exchange-deposit-tabs-security-final.log`. |
| RUNNING | Final updated-source security campaign and repeated actual GUI campaign after the native fee/reserve follow-up. `reports/exchange-deposit-tabs-security-complete.log`, `reports/exchange-deposit-tabs-appearance-final.log`. |
| NOT_RUN | Exact-source hosted CI and release verification for this correction. |
| NOT_RUN | Private Bybit account/key/region/IP/symbol-whitelist acceptance, credited deposits, funded trading/withdrawals, full physical OS/Wayland matrix and independent review. No real user storage/secrets or automatic mainnet funds are used. |

Retained development failures are separate: the first security campaign killed
all its mutants but correctly rejected a changed graphical-driver source hash.
One direct surface command selected the historical Electron suite; another
overlapped npm dependency installation and could not load Vitest. These attempts
are not passing evidence. Current Wails-only checks remain the acceptance path;
no security guard, legacy CI exclusion or test assertion was relaxed.
