# RPC providers, Arbitrum, TON activation and USDT totals

The 2026-10-30 owner update adds Arbitrum and retires active Decred support. Historical Decred records remain authenticated and encrypted; they cannot select an active network, RPC or signing operation. Existing EVM addresses and TON contract identities are preserved. Existing account indices acquire their deterministic Arbitrum descriptor on authenticated unlock, with one durable additive migration.

## Providers

| Network | Default | Verification and limits |
|---|---|---|
| NEAR mainnet / testnet | `https://rpc.mainnet.fastnear.com` / `https://rpc.testnet.fastnear.com` | Current FastNear provider; chain identity checked. |
| Base / Base Sepolia | `https://base-rpc.publicnode.com` / `https://base-sepolia-rpc.publicnode.com` | PublicNode service; EVM identity and Base fee-oracle methods checked. |
| Arbitrum One / Sepolia | `https://arb1.arbitrum.io/rpc` / `https://sepolia-rollup.arbitrum.io/rpc` | Official published network endpoints, chain IDs 42161 / 421614; Nitro gas estimate includes L1 posting. |

Sources: [NEAR providers](https://docs.near.org/api/rpc/providers), [FastNear RPC](https://docs.fastnear.com/rpc), [PublicNode Base](https://base.publicnode.com/), [Base builder providers](https://docs.base.org/get-started/builder-stack), [Arbitrum endpoints](https://docs.arbitrum.io/arbitrum-essentials/reference/node-providers), [Arbitrum gas estimation](https://docs.arbitrum.io/arbitrum-essentials/how-to-estimate-gas), [ARB issuer deployment addresses](https://docs.arbitrum.foundation/deployment-addresses).

Explicit saved RPC overrides stay unchanged. These are shared external services, without a bundled production SLA, light-client proof or provider quorum. They can see queried public addresses and can lie, censor or rate limit. Keys and app passwords stay local. For guaranteed capacity, use a provider account or your own node through **Configure trusted RPC**. No third-party credentials are embedded in the executable.

RPC and transaction-history indexing are separate services. Base/Arbitrum Blockscout returned HTTP 403 in current read-only probes. A configured Alchemy-compatible endpoint can supply address-indexed candidates, which are checked against selected-chain transaction/receipt/block data. Without a working index, the UI reports unavailable history; changing the balance RPC alone cannot manufacture older incoming history. No inaccessible query is reported as a successful empty account.

## TON contract activation and recovery

The wallet page observes `uninit`, `active` or `frozen`; provider failures remain unknown. A funded `uninit` address exposes **Deploy Wallet V5R1 contract**. Preparing checks current network, authority, seqno and estimated fees/storage. **Review and deploy** requires approval of the exact address, recorded wallet ID/code, zero outgoing transfers and estimated fee reserve, followed by the current app password. Preparation never broadcasts. The durable journal retains the original signed message for retry/reconciliation after uncertainty.

Wallet IDs are part of StateInit and therefore the receive address. An existing funded address must deploy its recorded ID. **Settings → Wallet → TON recovery profiles** can explicitly add/recover the original subwallet-0 profile or the separate standard V5 subwallet-42 profile at a recorded account index. The latter's raw on-chain IDs are 2147483451 on mainnet and 2147483607 on testnet; standard V5 network binding prevents signing the same message on both networks. These raw IDs are not the number 42. New canonical accounts default to standard V5 subwallet 42. Existing accounts remain on their exact recorded profile; original subwallet-0 addresses can also be explicitly recovered. Keep any additional profile and index with the phrase; local app passwords only encrypt installations.

## Indicative USDT value

The account heading shows a spinner and `USDT` while loading, then an approximate value, or an explicitly partial/unavailable subtotal. Go fetches at most two fixed unauthenticated public ticker requests per minute from official Bybit spot and Binance's market-data-only service. Requests contain no account addresses, holdings, API keys or wallet secrets. Arbitrary token symbols are never trusted as price identities. Reviewed contracts/chain IDs/decimals select supported quotes; exact whole-asset rational values are summed and only the final display rounds to cents.

Known zero balances need no quote. Unknown balances, positive unsupported tokens, stale quotes and unavailable prices cannot become complete zero values. The tooltip lists each affected currency and network, separating missing balances from missing prices. USDC uses its observed USDT price; it is not assumed equal to one. Staking positions, hidden wallets and exchange custody are excluded. Equal EVM addresses on different chains remain separate holdings. Testnet assets receive no real USDT valuation. The currency is GRAM on the TON network. Its unchanged internal `:TON` identity maps to the reviewed GRAMUSDT pair; old TONUSDT quotes are not substituted. The rename changes no balances, addresses, nine-decimal amounts or contracts. See [TON media assets](https://ton.org/media/), [Bybit rebrand announcement](https://announcements.bybit.com/en/article/bybit-toncoin-ton-gram-gram--blt617f4bf48950ab77/) and [Binance explanation](https://www.binance.com/en/academy/articles/what-is-the-ton-to-gram-rebrand). Prices are observations, not executable exchange offers.

## Evidence checklist

Evidence is being collected for this candidate. Passing local tests do not imply public funded acceptance, a production SLA, complete historical indexing, a Nitro-node integration, an OS matrix or an independent audit. See the current section in [STATUS.md](STATUS.md).

- PASS: production Go read-only identity/capability probes of the replacement providers and Arbitrum.
- PASS: independent EVM vectors and isolated Go-signed native/ERC-20 execution across Ethereum/Base/Arbitrum-shaped ledgers; no public funds used.
- PASS: actual TON emulator activates the exact funded V5R1 contract with no outgoing transfers, rejects replay/expiry/wrong network; critical TON package coverage exceeds 95%.
- PASS: public price client, strict identity, rational valuation, cancellation/privacy/cache regressions and read-only live price observations.
- PASS: 71 semantic security mutants, 11 real local integration gates, 29 actual generated-root Wails/X11 steps, four scoped TON GUI checks, fresh-copy 24-descriptor recovery, ten portable installer checks, dependency/workflow checks and 109 frontend tests.
- PASS: final complete current unit/helper/React aggregate and all 23 permanent fuzz targets with source stability.
- IN_PROGRESS: complete race aggregate, final security binding rerun and hosted CI/release.
- BLOCKED: Base/Arbitrum public history index HTTP 403; no private indexing key supplied.
- NOT_RUN: public funded Arbitrum/TON deployment acceptance, actual Nitro local node, complete X11/Wayland OS matrix, 24-hour soak and independent security review.
