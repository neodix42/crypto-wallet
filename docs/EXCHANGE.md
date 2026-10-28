# Exchange with Bybit

This integration requires a **Bybit Unified Trading Account**, using Funding and Unified balances. A Classic-only account can authenticate its key but cannot complete this balance/transfer flow; authentication alone does not establish trading eligibility.

The owner's 2026-10-24 request adds an optional Exchange tab with **Deposit**, **Swap** and **Withdraw**. The later USDC direction request permits custodial Bybit spot asset/USDT or asset/USDC trading. Ethereum USDT remains the pinned on-chain stablecoin; selecting a USDC spot quote does not add a USDC wallet contract or withdrawal route. The existing blockchain wallets, recovery profiles, secret requirements and production gates remain in force.

The wallet controls local private keys. **Bybit controls funds deposited into its account**, including trading, fees, credit, withdrawal restrictions and availability. Connecting is optional and performs no transfer or trade. Mainnet/production endpoints are used explicitly; an app build, startup, quote or automated test never sends mainnet funds.

## Wallets and asset selection

The mainnet registry has eight network wallets and ten pinned asset identities:

| Network wallet | Assets |
| --- | --- |
| Ethereum | ETH and pinned USDT ERC-20 |
| HyperEVM | Native HYPE |
| HyperCore | Spot HYPE in its separate ledger |
| NEAR | Native NEAR |
| Aptos | Native APT |
| Base | ETH and canonical ZEN ERC-20 |
| Decred | Native DCR |
| TON | Native TON under the existing BIP39/V5R1 profile |

Ethereum USDT uses contract `0xdAC17F958D2ee523a2206206994597C13D831ec7`, six decimals and Ethereum chain ID 1. It uses the same Ethereum account and needs **Ethereum ETH for gas**. [Tether's official integration guidelines](https://tether.to/en/supported-protocols/) identify the contract and its legacy empty transfer return. This feature adds no TON USDT jetton, USDT on another wallet network or arbitrary token support.

The asset selector displays the asset name. **Token contract** is separate metadata identifying an EVM token's issuing contract, not an owned wallet address. HyperCore spot assets use **Token identifier** rather than an ERC-20 contract label. For example, an Ethereum receive address such as `0x73410EA1FcbE1b8bCA883762dC56beF46403A469` identifies a wallet account, while USDT's `0xdAC17F958D2ee523a2206206994597C13D831ec7` identifies the shared Tether contract. ETH and supported Ethereum USDT for the same account use the same owned receive address. The app labels the selected wallet address separately; choose Ethereum at the sender and never send to the token contract as a deposit destination. This example does not verify ownership of the example address.

A supported local wallet asset does not guarantee a Bybit deposit/withdrawal network or active USDT/USDC spot pair. The backend queries the selected service, matches full native/token/network identity and rejects ambiguous or suspended routes. A shared EVM address or ticker does not identify a network. HyperCore is never inferred from HyperEVM; unsupported routes and deferred Decred exchange work stay unavailable while the original wallets remain present. No implicit bridge is created.

## Connect a Bybit account

1. Complete wallet recovery-word confirmation and unlock the wallet. Select **Settings**, immediately to the right of **Exchange** in the main tab bar, then select its **Exchange** tab.
2. Choose the API region matching your Bybit account. The list is explicit; the app does not infer a region from your IP or machine timezone.
3. Select **Connect Bybit**. In the current unlocked, recovery-confirmed session, enter the **API key** and matching **API secret** from a system-generated key created in Bybit, using the separate masked wallet dialogs. Review the exact service/custody confirmation. Connect alone no longer repeats the storage-password prompt; disconnect and every funds-moving operation retain their own authorizations.
4. Configure only the Bybit permissions needed for your intended actions: spot trading, internal account transfers and withdrawal. Withdrawals require a master-account key. Use an appropriate API IP restriction and add each exact owned destination address to Bybit's withdrawal whitelist. Read-only or insufficient permissions remain visible and cannot authorize writes.

Connection, region, key changes and Disconnect live in **Settings → Exchange**. Settings is a page in the main window, with **Wallet**, **Exchange**, **Journal** and **Appearance** tabs; it is not a popup. Select the main **Exchange** tab for Deposit, Swap and Withdraw, or select an account/wallet tab to return to it. You can configure the connection directly from Settings without opening the trading page.

The API key identifies the account connection; the API secret signs authenticated requests using HMAC. A key alone cannot authorize trading, transfers or withdrawals. “System-generated” names Bybit's key type, which provides both values; this app does not generate a replacement key. Self-generated RSA keys are not supported. Both fields remain masked. See [Bybit's authentication guide](https://bybit-exchange.github.io/docs/v5/guide) for its distinction between HMAC system-generated and RSA self-generated keys.

Information and error messages appear in the compact fixed bottom status bar and clear after seven seconds without changing page layout. They do not authorize a retry. If a message reports an unknown balance or uncertain request, refresh or reconcile the original operation before considering another write.

The ordinary portfolio receives status and a key suffix. It cannot read the key, secret or beneficiary fields or submit a generic signed API payload. Credentials stay in the authenticated encrypted local vault. Go core performs HTTPS requests. The default product now uses Wails/React and system WebKitGTK, with Go roles and its renderer unconfined under the explicit owner instruction; no OS-enforced signer remote-IP denial is claimed. The isolated prompt and transient Go transport are trusted with entered credentials, just as they are with entered wallet passwords. Their compromise can capture those inputs; masked fields and backend generation do not remove that trust.

## Deposit

Choose the source wallet/network and wallet asset, refresh its market, enter an exact amount and select **Deposit**. The backend obtains the matching Bybit deposit address and builds an ordinary wallet transfer. Go checks the complete network/token identity, destination, amount, fee and applicable memo before its existing approval/password/signing process. USDT deposits require enough Ethereum ETH gas. A TON deposit includes the returned account memo in the actual TON payment; an absent required memo is rejected.

The deposit-address API provides a token-contract suffix. That suffix can corroborate the already matched full coin-info contract; it cannot identify the token by itself. A wrong chain, contract, memo or suspended route prevents sending.

Track the original payment in **Settings → Journal**, under **Local payment journal**. Its accepted/confirmed chain result does not prove that Bybit has credited a spendable balance. Wait for a fresh Bybit balance observation. Daily caps, confirmations, AML/Travel Rule checks and account restrictions can delay or prevent credit. No Swap follows a deposit automatically.

## Swap

Choose a USDT or USDC direction for an asset with an active matching spot pair, such as ETHUSDT or ETHUSDC when offered by the selected Bybit service. Refresh the latest price and enter the amount to spend. Sell spends the selected wallet asset and receives the selected quote currency; Buy spends the selected USDT or USDC quote currency. The backend requires the instrument to identify that exact base and quote, and unavailable pairs stay unavailable. USDT remains the default for older requests. Select an application slippage limit of 0.01–1%, with at most two decimals.

The displayed last/bid/ask prices are observations. The submitted Market order uses Bybit's relative slippage control and can fill partially or be cancelled; the last price is not a guaranteed quote. The backend validates current precision, minimum notional, maximum market size, safe balances and the exact base/quote unit without floating point or silent rounding. Leverage and borrowing are disabled.

Trading uses Bybit's Unified account. If only part of the requested amount is available there, the app may first request a separate approval/password confirmation to move only the missing amount from Funding. That internal transfer has its own durable identity. A subsequent order requires another approval; pending transfers or stale target balances do not authorize a trade. USDC buys fund only the approved USDC shortfall, separately from USDT. The private order confirmation shows the exact pair, spending currency, indicative price currency and expected received-asset fee.

Reconciliation compares the original order identity and complete, bounded execution pages, including partial fills and their fee currencies. Documented additional taxes, including UAE VAT and Indian GST, are included. Unknown/missing fee metadata, duplicate/foreign fills, cursor loops, unexpected input-asset fees or fees exceeding gross receipts keep the result unresolved. A service-reported fill remains a custodial exchange observation. Sells reconcile fees and known taxes in the selected quote currency; buys reconcile them in the received wallet asset. A USDT fee on a USDC sell cannot silently become a valid USDC fee. Persisted operation symbols already bind their quote currency, so old USDT journal records retain their original payloads, digests and reconciliation identities.

## Withdraw

The destination controls are independent of the deposit/trading selection. Select the owned destination wallet and asset, use **Check withdrawal network & fee**, enter the total debit and select **Withdraw**. The signer derives the selected account's owned destination and matches the exact network/token; the ordinary renderer cannot substitute an arbitrary address. The address must match its Bybit whitelist entry exactly.

Withdrawals use Funding and force on-chain delivery. If a Unified → Funding move is needed, it has a separate confirmation before the withdrawal. The entered amount includes Bybit's processing fee. Fixed/percentage estimates use exact integer arithmetic and round the fee estimate upward at the withdrawal precision; the user's amount is never rounded. Bybit determines the actual processing fee. The service's record does not define whether its returned amount is gross or net, so the app exposes reported amount and fee separately rather than inventing an amount equation.

For the selected UAE endpoint, the signer requests the eight documented legacy beneficiary fields through masked wallet inputs: country, state, city, building, street, postal code, date of birth and place of birth. These fields are validated, encrypted with the operation and sent only to `api.bybit.ae`; they do not appear in the ordinary portfolio or public journal projection. The linked generic questionnaire schema does not specify UAE, so that schema is not guessed.

Türkiye, Kazakhstan, EU and Indonesia withdrawals are gated **before any internal funding transfer** because their required questionnaires are not implemented. Other service/account restrictions may still reject an operation. No jurisdiction's API acceptance has been established by local fixture tests.

`EXCHANGE_COMPLETED` means Bybit reports the acknowledged withdrawal completed. It does not prove destination-chain credit or finality. Refresh the destination wallet and obtain independent chain evidence when needed.

## Pending, unknown and recovery

Each exchange write has an immutable, encrypted local operation record and a signer-issued one-use authorization. The signer binds it to credentials, endpoint, wallet, exact payload, session and expiry, and records submission before transmission. Lock, cancellation, changed credentials and stale sessions cannot release a new authorization. An internal transfer does not grant authority to a later order or withdrawal.

An accepted request can lose its response. UNKNOWN/PENDING operations retain their original identities and block conflicting exchange writes; use **Reconcile original exchange operation** on that operation. Do not create another order/withdrawal to resolve a timeout. Only individually allowlisted, documented preacceptance rejection codes can become terminal without a receipt.

Bybit's withdrawal-record API queries the returned withdrawal ID and does **not** expose request-ID lookup. If a withdrawal was accepted but its acknowledgment was lost, the app cannot prove a match using amount, address or time. It preserves UNKNOWN and never automatically retries or replaces that withdrawal. Verify the original request independently with Bybit.

The BIP39 phrase restores this wallet's keys, including the Ethereum address used for USDT. It does not restore Bybit API credentials, beneficiary data, exchange account access or the local exchange journal. A password change preserves the encrypted credentials and journal; a phrase-only fresh installation has no remote-operation history to reconstruct automatically. Check the exchange account independently before reconnecting after loss of local metadata. Disconnecting removes the local connection; it cannot cancel an accepted request or revoke the API key in Bybit. Manage/revoke that key through Bybit itself.

## Security boundary and evidence

Bybit's HMAC signs timestamp, API key, receive window and query/body; it excludes HTTP method/path/endpoint. The signer validates those descriptor fields and the trusted client uses them exactly, but a compromised Go network core could replay an unexpired signature elsewhere if Bybit accepts the same payload. Core is trusted exchange authorization transport. The production client accepts only exact official HTTPS endpoints, follows no redirects, ignores environment proxies, bounds transport/JSON resources and exposes redacted numeric API errors. There is no generic authenticated exchange API in the public Electron bridge. [The API contract](BYBIT_API.md) links the official sources and describes these limits.

Evidence is local unless explicitly stated otherwise. Historical reports retain their original candidate/source scope; a passing unit report cannot certify a later artifact or a live exchange account.

| Check | Status and scope |
| --- | --- |
| Bybit client, independent HMAC vector, real loopback TLS redirect/proxy rejection, exact decimal/schema tests, UAE and tax parsers | **PASS** in [the source-bound client report](evidence/bybit-client.json); synthetic credentials and local HTTP/TLS fixtures |
| Route, wallet-journal coordinator, approval ordering, exact market units/fills/fees, ambiguity and ownership tests | **PASS** in [the source-bound coordinator report](evidence/exchange-core.json); inherited-pipe signer and hostile-provider fixtures are explicitly test doubles |
| Client security mutants | **PASS**, five killed in [the mutation report](evidence/bybit-client-mutations.json); Go overlays leave production sources untouched |
| Signer security mutants | **PASS** in [the signer mutation report](evidence/exchange-signermutation.json); exact source and selectors are retained |
| Race checks and bounded Bybit authorization/tax parser fuzz campaigns | **PASS** within the reports' exact package/source scopes; current whole-repository results remain in [the release checklist](STATUS.md) |
| Actual local Ethereum USDT transaction execution | Separate local-node receipt/state evidence; [current acceptance status](STATUS.md) identifies the executed source/candidate and any blocked prerequisites |
| Actual packaged desktop Exchange/Settings interaction | **PASS**, **26** final-candidate X11 checks, including main-window Settings/Journal navigation, public-pixel verification of the owned address versus USDT contract, three notice-layout observations, disconnected money-action gating and app-password/empty API-input cancellation with unchanged vault. No API key entered or custody approval given; API-secret field-label OCR and private Bybit acceptance remain NOT_RUN. See [the current scoped report](evidence/settings-main.json), [packaged evidence](evidence/packaged-recovery.json) and [the Settings checklist](EXCHANGE_SETTINGS.md). |
| USDC quote/buy/sell, separate Funding approval, exact private spending/price/fee summaries, tampered quote rejection, encrypted restart and no replay | **PASS** in the local `USDC` Go regressions (`internal/exchange/bybit/usdc_test.go`, `internal/walletcore/exchange_usdc_test.go`, `internal/signer/exchange_usdc_test.go`); synthetic credentials and provider fixtures, not live acceptance |
| Private Bybit key/account/IP/whitelist/region acceptance and credited deposits | **NOT_RUN**; no real private credential was used |
| Funded Bybit spot trade and withdrawal back to an owned wallet | **NOT_RUN**; no mainnet funds were sent |
| Independent review, complete OS matrix and original production gates | Remain separate and incomplete; this extension is not a production-readiness or audit claim |

Reproduce the implemented local checks with `make test-exchange`. Its explicit local USDT integration needs its documented test prerequisites and fails when they are missing. Use `make test-race`, `make coverage` and the broader README commands for other gates. No target uses real Bybit secrets or automatically sends mainnet funds. The original Decred/public-testnet deferrals and all existing wallet security requirements remain visible in [STATUS.md](STATUS.md) and [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md).

The original Exchange candidate's 125 frontend/23 packaged checks and associated artifact evidence remain in repository history. Current source has **132** passing frontend tests, **26** passing packaged X11 checks, **seven** isolated actual-geth payment checks, **six** headless phrase-recovery checks and **seven** Go-process crash checks in [settings-main.json](evidence/settings-main.json). Its passing component floors and local interface checks do not override the current broader [Exchange aggregate](evidence/exchange.json): **FAIL**, because the mandatory pinned dcrd v1.10.9 simnet prerequisite is unavailable (**BLOCKED**). Decred provisioning remains owner-deferred and no gate or floor was weakened. [The Settings checklist](EXCHANGE_SETTINGS.md) records this scope separately. The local collector `python3 scripts/exchange-evidence.py` verifies reports against their source and artifact; it cannot establish live exchange acceptance. The recorded non-Decred 24-hour soak no longer has its process or a completion report; it is NOT_RUN/partial, not active or passing.

The current coverage profile retains 28 unexecuted one-statement blocks in the Exchange implementation files: mostly defensive persistence/state/association/error exits, an impossible fee-list normalization fallback and inner arithmetic checks already guarded by earlier bounds. They remain included in the denominators. `reports/go-uncovered.json` records their exact locations/excerpts. Passing statement floors is not branch coverage or independent security review. Integration review found and regression-tested two real defects: the TON registry identity mismatch and decimal overflow previously formatted as zero.
