# Default stablecoins and maximum sends

The 2026-10-31 owner update adds eight mainnet asset identities, retaining all eight networks and the original eleven assets. Defaults appear without importing a contract, including after phrase restoration. They reuse the existing owned receive address and derivation profile. Testnet defaults remain unchanged; no public testnet USDT identity is invented.

| Network | Default addition | Decimals | Exact contract / spot token ID |
|---|---|---:|---|
| Ethereum | USDC, Circle native | 6 | `0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48` |
| Arbitrum One | USDT0 | 6 | `0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9` |
| Arbitrum One | USDC, Circle native | 6 | `0xaf88d065e77c8cC2239327C5EDb3A432268e5831` |
| Base | USDT, canonical bridged representation | 6 | `0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2` |
| Base | USDC, Circle native | 6 | `0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913` |
| HyperEVM | USDC, Circle native | 6 | `0xb88339CB7199b77E23DB6E890353E22632Ba630f` |
| TON | USDT, canonical Tether jetton | 6 | `0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe` |
| HyperCore Mainnet spot | USDC, canonical spot token 0 | 8 | `USDC:0x6d1e7cde53ba9467b783cb7c530ce054` |

Identity sources checked on 2026-10-31: [Circle's issuer registry](https://developers.circle.com/stablecoins/usdc-contract-addresses), [USDT0 deployments](https://docs.usdt0.to/technical-documentation/deployments), [OP canonical token list](https://github.com/ethereum-optimism/ethereum-optimism.github.io/blob/master/data/USDT/data.json), [Tether's TON deployment](https://tether.to/en/supported-protocols/) and [HyperCore spot metadata](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/spot). Base USDT is explicitly bridged; it is not presented as native Tether issuance or a Base USDT0 deployment. HyperEVM uses native USDC, not USDC.e. A token's symbol alone never establishes its identity.

## Wallet behavior

Each wallet lists its assets in the Token / Balance / Decimals table. Select a row before Send. Amount uses whole tokens, with exact integer conversion in Go. Receive always uses the owned wallet address, including TON USDT; a jetton master or holder's jetton wallet is not the receiving address to give another user. Hover identifies the ERC-20 contract, TON master or Core spot ID. Failed token reads stay unknown while independently verified balances remain visible. USDT and USDC identities also enter the indicative account valuation; missing balances/prices remain explicitly named and partial.

EVM defaults check the network, precision, current runtime and reviewed proxy implementation where applicable. Base USDT additionally binds the Ethereum remote token and canonical Base bridge. These checks are not an issuer audit or consensus proof. Issuer pause/blacklist/upgrade controls remain external; changed implementations fail pending review. Only the existing direct ERC-20 transfer is enabled, with native ETH or HYPE paying gas. No allowance, permit or bridge operation is added.

TON USDT verifies the pinned Tether master and wallet library and locally derives each owner-bound jetton wallet. The V5R1 signer permits only the canonical transfer operation, exact six-decimal amount, owner recipient, query identity, response address and comment. It attaches 0.1 GRAM for jetton processing, including a one-nanogram notification; unused excess returns to the sender. The estimated native reserve includes that attachment and cannot be treated as a signed protocol fee ceiling. Confirmation requires the linked source wallet, source jetton and recipient jetton execution; missing or contradictory evidence remains UNKNOWN with the original signed message/reservation. Both original subwallet-0 and standard subwallet-42 addresses remain unchanged. General jetton imports and arbitrary contract calls are not enabled.

HyperCore USDC remains a separate spot ledger with eight atomic decimals. The constrained official `spotSend` signing domain and exact token ID apply; there is no perps, bridge or arbitrary typed-data operation. Existing fresh-recipient activation and unsupported unified/portfolio-account restrictions remain visible. Total holdings and spendable funds differ when orders place funds on hold.

Previously imported contracts that become defaults keep their original encrypted metadata and pending operations. The current default shadows that historical watch policy without deleting it or permitting its old precision/hash to override the reviewed registry. Normal unlock does not rewrite unchanged ciphertext. Imported custom contracts remain separately approved policies and are not restored from the phrase; these defaults are.

## Max

Every Send form has **Max** beside Amount. Enter the recipient first because activation, contract execution and fees can depend on the destination. Go reads the selected asset's verified spendable balance, estimates the exact transfer and prepares the final amount. Max itself never signs or broadcasts. Review and the current app password remain mandatory.

For native currency, Max subtracts the estimated fee and required storage/spendability reserve. For tokens, Max uses the full spendable token balance and checks the separate gas balance; USDT is never subtracted to pay an ETH/GRAM fee. Core order holds are excluded. Base transaction-size and TON Coins-encoding fee increases use a bounded recalculation from the adapter’s checked local native budget quote. Provider errors, rate limits, token gas and malformed quotes cannot trigger that recalculation. Unknown, stale, malformed or duplicate observations fail; an unsuccessful calculation preserves the original journal and reservation.

Exact zero cannot be guaranteed: actual charges can be below fee caps/reserves, gas can be refunded, TON jetton excess can return, and NEAR/TON may require storage balances. Max does not secretly destroy a wallet contract, abandon storage or allow an unbounded variable-value transfer to promise zero dust.

## TON deployment diagnosis

A public read-only probe reproduced HTTP 429 at TON Center's fee-estimation stage despite the existing per-adapter pacing. A separate single-request estimate was about 0.0007 GRAM; that observation is not a promised fee for another wallet or a funded deployment acceptance. [TON Center documents its keyless quota](https://docs.ton.org/api/rate-limit).

The same Go request sequence succeeds with single-use TLS connections. Official keyless TON Center endpoints therefore use that connection policy and a shared per-host limiter across adapter instances. The limiter holds its lease until the bounded response body is closed and waits 1.1 seconds after completion; measuring only request starts allowed server arrivals to bunch when TLS latency changed. TLS certificate verification, configured environment proxies, authenticated endpoints and explicitly supplied client transports remain intact. No request or broadcast is automatically retried. Rate limits and preparation/recording failures use allowlisted fixed error categories instead of implying the balance is necessarily unknown.

An alternative Orbs TON Access candidate was rejected after repeated availability failures and responses carrying testnet zerostate on mainnet paths. Those checks failed closed; no Orbs resolver or default replacement ships. TON Center remains a third-party provider without a bundled SLA or consensus proof; other applications sharing its public quota may still require a configured API key or trusted custom RPC. Exact 0.2-GRAM zero-outgoing deployments pass actual TVM execution for both immutable V5R1 profiles on mainnet/testnet contexts, including replay, expiry and insufficient-fee rejection. This is local execution evidence, not a funded public deployment. Current completion and remaining external gates are recorded in [STATUS.md](STATUS.md).

## Evidence checklist

- PASS: six EVM defaults' live read-only network/precision/runtime/implementation checks; twelve actual Go-signed transfers across four owned EVMs with independent receipt/balance/history verification.
- PASS: canonical Tether jetton execution in the actual TVM with independent official SDK bytes/signatures, both V5R1 profiles, exact token credit/debit and replay/expiry rejection.
- PASS: independent official HyperCore SDK USDC signatures at indices 0/1/127, bounded metadata/state/history and no replacement after uncertainty.
- PASS: focused race checks for default asset UI, whole-unit Max routing, current-password approval and nondestructive encrypted promoted-policy/UNKNOWN-operation recovery.
- PASS: final local aggregate, security and full race/stress checks; 25 executed fuzz targets; 13 actual local protocol gates; 86 killed compiling security mutants; final portable build/recovery/installer/private-geth send and 29 complete desktop checks. Critical statement coverage: EVM 95.5%, TON 95.839%, HyperCore 96.6%, pricing 96.1%. Source-specific reports and corrected failures remain separate.
- NOT_RUN: funded public stablecoin/TON deployment acceptance, complete combined Go/native coverage acceptance, complete supported OS/display matrix, 24-hour soak and independent review. Existing inaccessible history-index gates remain separate. Hosted results are recorded in STATUS after publication.

Reproduce focused checks with `make test-default-tokens`; `make test-integration` includes the canonical EVM and TON jetton execution gates. Use only disposable/public fixtures. No development command automatically spends mainnet funds.
