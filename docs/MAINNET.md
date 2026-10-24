# Mainnet connections and profile selection

The desktop starts in the **Mainnet** profile. Run `./wallet --profile=testnet` when deliberately using test/local networks. Profiles have separate vaults under `$XDG_CONFIG_HOME/localwallet/mainnet` and `$XDG_CONFIG_HOME/localwallet/testnet` (default `~/.config/localwallet/...`). Starting mainnet does not convert, copy or relabel an existing testnet vault. Phrase restoration explicitly selects a destination profile; the words do not carry local profile settings.

One root derives the initial account for every supported mainnet network. Network identity remains part of each wallet: HyperCore and HyperEVM have separate balances, and the same EVM address on Ethereum, Base and Horizen does not move assets between them. Generated and pending wallets require native recovery-phrase recording/confirmation before receive or send on either profile. This replaces the former encrypted-file backup prerequisite; the app no longer creates, verifies or restores those files. See [current phrase recovery](PHRASE_RECOVERY.md). Credentials and transaction approval remain in the isolated native signer.

## Configured providers

These public endpoints require no preinstalled development tools or bundled API credentials. They can rate limit, censor, change availability or provide incorrect data. A matching network identifier is a necessary check, not a consensus proof. Querying a wallet exposes its public address to its selected provider. Prefer a provider you trust or a node you operate for sustained use; no uptime or capacity guarantee is inferred from one successful request.

| Wallet network | Default HTTPS connection | Identity checked |
| --- | --- | --- |
| Ethereum / ETH | `https://ethereum-rpc.publicnode.com` | EVM chain 1 |
| HyperEVM / HYPE | `https://rpc.hyperliquid.xyz/evm` | EVM chain 999 |
| Base / ETH and ZEN | `https://mainnet.base.org` | EVM chain 8453; pinned ZEN contract code and 18 decimals |
| Horizen / ETH and ZEN | `https://horizen.calderachain.xyz/http` | EVM chain 26514; pinned ZEN contract code and 18 decimals |
| HyperCore / spot HYPE | `https://api.hyperliquid.xyz` | Mainnet HYPE token `0x0d01dc56dcaaca66ad901c959b4011ec`, index 150, 8 atomic decimals; provider time |
| NEAR | `https://rpc.mainnet.near.org` | `mainnet`, synchronization and recent state |
| Aptos / APT | `https://api.mainnet.aptoslabs.com/v1` | Ledger chain 1 and recent ledger timestamp |
| TON / Toncoin | `https://toncenter.com/api/v2/jsonRPC` | Official mainnet zerostate root/file hashes; recent account state |
| Decred / DCR | Explicit authenticated `dcrd` required | Official mainnet genesis, synchronization and block ancestry |

Endpoint provenance: [Ethereum provider](https://ethereum.publicnode.com/), [HyperEVM](https://hyperliquid.gitbook.io/hyperliquid-docs/onboarding/how-to-use-the-hyperevm), [Base](https://docs.base.org/get-started/connect-to-base), [Horizen](https://docs.horizen.io/horizen-chain/network/mainnet/), [HyperCore metadata](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/spot), [NEAR](https://docs.near.org/api/rpc/providers), [Aptos](https://aptos.dev/network/nodes/networks), [TON API](https://docs.ton.org/api/v2/overview) and [TON configuration](https://ton.org/global.config.json). Reviewed 2026-10-23.

Canonical ZEN is `0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229` on Base and `0x57da2D504bf8b83Ef304759d9f2648522D7a9280` on Horizen. The latter address is an OFT adapter on Base, not Base's ZEN token. Both wallets require ETH on their own network for gas. Legacy Horizen mainchain/EON is not a current receiving network. [Official token registry](https://docs.horizen.io/horizen-chain/tokens-and-gas/zen-token/).

## Changing a provider

Open the wallet and choose **Configure trusted RPC**. Enter the endpoint and any supported credentials in the native window, then approve the stated provider trust/privacy implications. Settings are encrypted in the local vault. A phrase-restored installation must configure its own provider settings; the words do not contain RPC credentials. Never put credentials in a URL, command line, issue, screenshot or log.

The optional API key field is used by the TON adapter. Decred uses its username and password fields. Other current adapters use the configured URL; they do not support arbitrary authentication headers. A provider requiring unsupported authentication needs an appropriate trusted local gateway or a future reviewed adapter extension. The wallet does not invent credentials, disable certificate verification or silently switch to another provider on failure.

TON Center permits one request per second without an API key and documents [rate limits](https://docs.ton.org/api/rate-limit) and [authentication](https://docs.ton.org/api/v2/authentication). NEAR publishes provider-specific limits in its [provider list](https://docs.near.org/api/rpc/providers). A failed, stale or incomplete query must remain unavailable/unknown; it must not appear as a confirmed zero balance. Do not stress test shared public endpoints.

## Decred trusted-node setup

This adapter uses authenticated trusted `dcrd` RPC and bounded full-block scanning. It does not use a public block explorer as a substitute, run a second secret-bearing wallet, or claim SPV verification. Follow the official [dcrd setup](https://docs.decred.org/wallets/cli/dcrd-setup/) and [RPC arguments](https://docs.decred.org/wallets/cli/dcrd-and-dcrwallet-cli-arguments/): run mainnet without testnet/simnet options, synchronize the node, enable RPC authentication, and keep its listener private. Mainnet RPC normally uses port 9109.

For a node you operate, create unique RPC credentials in its protected configuration file, not command-line arguments. Configure this wallet with the node's `https://` URL and those credentials. The certificate must match the hostname and chain to an OS-trusted issuer. A local `dcrd` commonly generates a self-signed certificate; this app currently has no custom-CA upload field. Deliberately install the operator's verified CA/certificate into the OS trust store or use a local TLS gateway with a trusted certificate. Restart the app after trust changes. Do not use `notls`, a skip-verification flag, a public unauthenticated endpoint or sample credentials for mainnet. Decred configuration remains unavailable until this setup is complete.

Open **Network details & public diagnostics** in the Decred wallet tab, choose the birthday height, address gap, address limit and maximum blocks, then confirm in Go. The controls load that wallet's saved parameters. The default 10,000-block budget cannot scan mainnet from genesis. The maximum selectable budget is 2,000,000 blocks; address search is separately bounded. A recent birthday is appropriate only for a root known to be newly created at that height. A restored phrase may control earlier payments and later derived indices; deliberately select the necessary historical range. An incomplete range is not proof of no funds. Current scans are recomputed rather than an optimized persistent SPV database; large histories and balances need further acceptance/performance evidence. Ordinary expiry-bearing payments also have Decred maturity constraints, documented in [DCR/TON implementation evidence](DCR_TON.md).

## Executed evidence and remaining gates

The historical [read-only probe report](evidence/mainnet-rpc.json), 2026-10-23: **29 PASS**, **1 BLOCKED**, **0 FAIL**. Eight public mainnet domains returned their expected identities. Required EVM priority-fee calls, both rollup fee-oracle methods, canonical ZEN runtime hashes/decimals, NEAR transfer protocol fields, Aptos gas estimation and TON zerostate matched the adapter's public requirements. The older Aptos fullnode alias also responded, but the default uses the current documented API host. Decred connectivity is **BLOCKED** because no authorized authenticated mainnet node was configured. No user profile, wallet address, balance, secret or signed transaction was read or transmitted by these probes, and nothing was broadcast.

These retained checks establish provider connectivity and selected public metadata only for their recorded run. They do not certify the subsequently changed recovery policy or rebuild. Funded mainnet transfers were **NOT_RUN**. Required public testnet acceptance, identified external-wallet interoperability, complete installed OS/recovery testing, 24-hour stress work and independent security review retain their existing incomplete statuses in [STATUS](STATUS.md). Selecting mainnet does not mark those release gates passed.
