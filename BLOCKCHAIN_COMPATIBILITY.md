# Blockchain compatibility and recovery contract

Research baseline: **2026-10-23**. This is an implementation requirement, not evidence that a wallet has already passed these tests. Reverify network identifiers, contracts, SDK releases, protocol rules and endpoints against official sources before implementation and release. Pin reviewed dependency versions and fixture-generator commits; never ship floating `master` dependencies.

## 1. Required coverage and identity

Implement native ETH on Ethereum, NEAR, APT, DCR, TON, native HYPE on HyperEVM, spot HYPE on HyperCore, and canonical ERC-20 ZEN plus native ETH on Base. The owner update of 2026-10-28 removes Horizen mainnet/testnet from active support while preserving authentication of historical encrypted descriptors and journal. The eight active mainnet networks contain ten pinned asset identities, including the earlier pinned Ethereum USDT extension. Standard ERC-20 contracts may be explicitly approved on Ethereum/Base; bounded Base indexer discovery provides read-only candidates with RPC-verified metadata/balances and never grants signing authority. Other token protocols, trading/staking beyond authorized Bybit and NEAR staking-pool operations, NFTs, bridges, legacy migration claims and dApp signing remain outside v1; TON USDT jettons are not added.

Every asset is identified by network/environment plus native-asset ID, verified contract address or Core token ID. A ticker or `0x` address alone never identifies the network. Confirmation screens and receive QR views must show the exact network and asset. Importing a seed does not move funds between networks.

| Required entry | Mainnet identity | Testing counterpart | Transfer/gas model |
|---|---|---|---|
| Ethereum ETH | EVM chain ID 1 | Sepolia; verify current chain ID | Native ETH; EVM transaction |
| Ethereum USDT extension | EVM 1, contract `0xdAC17F958D2ee523a2206206994597C13D831ec7`, six decimals | Disposable local token/receipt fixtures; no fabricated public-testnet USDT identity | ERC-20 transfer; Ethereum ETH pays gas |
| HyperEVM HYPE | EVM 999 | EVM 998 | Native HYPE; EVM transaction |
| HyperCore HYPE | HyperCore Mainnet, spot ledger | HyperCore Testnet | User-signed Core transfer; separate balance |
| ZEN on Base / Base ETH | EVM 8453 | Base Sepolia 84532 | ZEN ERC-20; ETH pays gas |
| NEAR | NEAR mainnet | NEAR testnet/local sandbox | Native NEAR; account/access-key model |
| Aptos APT | Aptos mainnet; verify ledger chain ID | Aptos testnet/localnet | Native APT; selected authentication scheme |
| Decred DCR | Official mainnet chain parameters | Official supported testnet/simnet | Decred UTXO and script model |
| TON | TON mainnet global configuration | TON testnet/emulator | TON wallet contract; TON pays fees |

Sources: [Ethereum networks](https://ethereum.org/developers/docs/networks/), [HyperEVM](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/hyperevm), [Base networks](https://docs.base.org/get-started/connect-to-base), [Horizen mainnet](https://docs.horizen.io/horizen-chain/network/mainnet/), [Horizen testnet](https://docs.horizen.io/horizen-chain/network/testnet/). A matching RPC chain ID is necessary but does not authenticate a malicious provider; apply the project's RPC trust policy as well.

## 2. One phrase, explicit derivation profiles

Generate **12 English BIP39 words by default** from 128 bits of OS cryptographic randomness. Offer advanced 24-word BIP39 generation from 256 bits only with an explicit multichain-BIP39 profile. This follows current [TON TEP-3](https://github.com/ton-blockchain/TEPs/blob/master/text/0003-wallets.md) interoperability guidance: its native 24-word TON scheme differs from BIP39 despite the shared word list. Import the valid BIP39 lengths declared by this app: 12, 15, 18, 21 and 24 words, validating checksum and normalization. Do not claim external TON wallets accept all those lengths.

Apply [BIP39](https://github.com/bitcoin/bips/blob/master/bip-0039.mediawiki) mnemonic-to-seed exactly, including NFKD normalization and its standard 64-byte output. This KDF is a compatibility rule, not the vault password KDF. The application password never participates in address derivation. New wallets default to an empty BIP39 passphrase; importing a nonempty passphrase is required with separate secure input, explicit offline recovery requirements and independent vectors. Never guess, trim or substitute a passphrase.

Store immutable profile IDs with each account:

| Profile | Algorithm and exact path | Address/key identity |
|---|---|---|
| `evm-bip39-bip32-v1` | BIP32 secp256k1, `m/44'/60'/0'/0/i` | EVM address; increment final `i` |
| `near-bip39-slip10-v1` | Hardened Ed25519 derivation, `m/44'/397'/i'` | Ed25519 implicit account by default |
| `aptos-bip39-legacy-ed25519-v1` | Hardened Ed25519, `m/44'/637'/i'/0'/0'` | Explicit **Legacy Ed25519** authentication |
| `ton-bip39-slip10-v5r1-v1` | SLIP-0010 Ed25519, `m/44'/607'/i'` | Pinned V5R1 StateInit; workchain 0; network-specific wallet ID |
| `decred-bip39-64-child-v1` | Full 64-byte BIP39 seed; official Decred `Child()`; `m/44'/42'/a'/b/j` | secp256k1 ECDSA P2PKH; external `b=0`, change `b=1` |

Here `i/a/j` are zero-based bounded integers. Decred testnet uses its explicitly pinned official testnet coin-type/chain-parameter profile, ordinarily coin type 1; it must not accidentally reuse mainnet parameters. Other network variations must be recorded, not inferred from displayed labels.

EVM accounts deliberately use the same derivation profile across supported EVM networks; the same owner address does not combine balances. Reference paths: [Go Ethereum](https://github.com/ethereum/go-ethereum/blob/master/accounts/hd.go), [NEAR reference](https://github.com/near/near-seed-phrase/blob/master/index.js), [Aptos Go accounts](https://aptos.dev/build/sdks/go-sdk/account), TEP-3 above, and [Decred HD derivation](https://pkg.go.dev/github.com/decred/dcrd/hdkeychain/v3).

### Recovery boundaries

Go generates the recovery phrase; the dedicated isolated Electron prompt displays it during pending setup for offline recording, and Go verifies the separate masked word challenge. The ordinary portfolio/public API never receives it. The owner-authorized transient prompt transport, Go coordinator, Electron main and dedicated prompt renderer/preload are trusted with phrase/password inputs. Private keys, extended keys, binary seeds and entropy remain in Go and are never displayed. Fresh-install restoration uses the phrase and any original optional BIP39 passphrase; a specified app password encrypts the new local vault and does not affect canonical keys.

A known phrase can recreate this app's canonical keys, subject to the same BIP39 passphrase and profiles. It does not recreate arbitrary account names, labels, historical custom paths, rotated authorities or every contract choice. The encrypted local vault preserves profiles, indices, discovery ranges, contract parameters and associations. A phrase alone does not carry that metadata; recovery must use the immutable registered profile and explicit index/discovery/association controls. Provide bounded discovery with explicit rescan controls; a search limit or offline RPC is never proof of zero funds.

Do not auto-convert native TON phrases, Decred PGP-word seeds, legacy ZEN seeds or unknown external-wallet profiles. Reject unsupported schemes clearly before generating a misleading empty wallet. Never change existing account derivation when upgrading dependencies or defaults.

## 3. EVM networks and ZEN

Use reviewed Go Ethereum signing/serialization primitives, local address validation and integer amounts. Prefer supported typed transactions; require chain-bound replay protection and refuse unprotected pre-EIP-155 signing. Validate sender, network, destination, native value, ERC-20 calldata, gas and total fee against the approved immutable transaction plan. Sources: [EIP-155](https://eips.ethereum.org/EIPS/eip-155), [EIP-1559](https://eips.ethereum.org/EIPS/eip-1559).

Legacy Horizen mainchain/EON balances migrated to Base ERC-20 ZEN on 2025-07-23; they are not new receiving networks for this app. Current Horizen is an EVM L3 with ETH gas, but this wallet no longer exposes it under the 2026-10-28 owner instruction. Official [ZEN deployment documentation](https://docs.horizen.io/horizen-chain/tokens-and-gas/zen-token/) identifies:

| Network | Required token contract |
|---|---|
| Base mainnet | `0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229` |
| Base Sepolia tZEN | `0x107fdE93838e3404934877935993782F977324BB` |

Reverify deployment bytecode, decimals and network before pinning the registry. The historical `0x57da2D504bf8b83Ef304759d9f2648522D7a9280` address is an **OFT adapter on Base**, not Base's ZEN token. Never select contracts by symbol. A ZEN transfer uses `transfer` and needs ETH on that same network; it never implicitly triggers a bridge or approval. Include applicable rollup data fees in estimates. Sources: [migration](https://docs.horizen.io/migration/overview/), [Horizen fees](https://docs.horizen.io/horizen-chain/tokens-and-gas/gas-on-horizen/).

Test wrong-chain RPCs, mixed-case checksum errors, token/recipient confusion, zero/maximum values, insufficient ETH despite sufficient ZEN, contract reverts, fee spikes, nonce races, replacements, reorgs and a timeout after acceptance. Persist signed bytes/hash before broadcast and reconcile the original payment before any retry.

### Owner-authorized Ethereum USDT extension — 2026-10-24

Pin USDT to Ethereum mainnet chain ID 1 and the exact contract above, independently of the ticker and Bybit metadata. The existing Ethereum account/profile remains unchanged. [Tether's official integration guidelines](https://tether.to/en/supported-protocols/) identify that Ethereum contract and document its older `transfer` behavior without a Boolean return. Permit that documented empty simulation return only for this pinned USDT identity, while still rejecting reverts, malformed return data and mismatched transfer receipts. This exception must not weaken Boolean-return checks for ZEN or arbitrary contracts. Verify six-decimal amounts, exact transfer calldata/event/source/destination/amount, ETH gas coverage and original-transaction reconciliation. Independent local token acceptance is evidence for that fixture, not a claim of a funded Ethereum USDT roundtrip.

Bybit routes must match a wallet's actual ledger and full token contract before obtaining or spending to a deposit address. Ethereum uses Bybit's verified `ETH` chain identifier; other supported routes use authenticated coin metadata with explicit accepted network labels, not guessed chain strings. The deposit-address response exposes only a contract suffix, which may corroborate an already verified full coin-info contract but cannot establish token identity itself. TON account credit can depend on its returned memo; include it in the actual signed TON payment. HyperCore is a separate ledger and cannot be inferred from an EVM-format address. Unsupported, suspended or ambiguous Bybit routes remain unavailable without removing the original wallet network.

Spot prices and fills are custodial service observations and do not establish chain interoperability. An exchange-completed withdrawal still requires destination-chain observation for credit/finality. [Exchange requirements and acceptance status](docs/EXCHANGE.md) supplement the unchanged per-chain compatibility gates below.

## 4. Hyperliquid: two explicitly separate domains

HyperEVM native HYPE uses the EVM adapter and 18 decimals. HyperCore spot HYPE uses separate balance/history APIs and user-signed actions. Implement ordinary peer-to-peer Core HYPE with `spotSend`, or a pinned `sendAsset` implementation constrained to `sourceDex="spot"`, `destinationDex="spot"`, `fromSubAccount=""` and the verified HYPE token ID. Do not expose arbitrary actions, orders, perps, API-agent approvals or raw typed-data signing.

`hyperliquidChain` selects Mainnet/Testnet. `signatureChainId` belongs to the signing domain, not the recipient ledger; do not substitute HyperEVM 999 automatically. Match a pinned [official Python signing implementation](https://github.com/hyperliquid-dex/hyperliquid-python-sdk/blob/master/hyperliquid/utils/signing.py) with independent fixture generation. Check exact fields/types, amount normalization, address normalization and nonce/time handling. [Signing documentation](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/signing) explicitly distinguishes user-signed and L1 signing schemes.

Verify Core HYPE metadata and atomic precision independently of HyperEVM; the [official metadata example](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/spot) has eight atomic decimals. Trading size precision is a different field. Never silently round. Test fresh recipients, existing recipients without HYPE, existing HYPE holders, activation prerequisites, insufficient balances, response errors and replayed requests. Recipient activation rules/fees must be established by implementation evidence; do not invent them from unrelated API sections. Sources: [exchange actions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint).

Receive QR screens must distinguish Core from EVM even when the owner address matches. Ordinary send must not target the Core/EVM system bridge as an implicit operation. Bridging is outside v1.

## 5. NEAR

Default to Ed25519 implicit accounts: lowercase 64-character hex public keys. Named accounts are registered independently and can have multiple or removed keys. The default phrase profile matches `near-seed-phrase` at index zero; Ledger paths differ. Do not claim universal NEAR import compatibility. Sources: [account IDs](https://docs.near.org/protocol/accounts-contracts/account-id), [access keys](https://docs.near.org/protocol/accounts-contracts/access-keys).

Allow explicit association/discovery of named accounts only after verifying the derived key's on-chain authority. Native transfer requires adequate permission; a function-call-only key is insufficient. Use exact yoctoNEAR integers, current storage/fee rules, per-access-key nonces and recent block hashes. A missing implicit recipient can be created by transfer; a missing named recipient fails. Test both, revoked keys, restricted keys, expired block references and nonce races.

Distinguish submission from execution and finality. The [transaction guide](https://docs.near.org/integrations/create-transactions) discusses double-payment risks from naive retrying. Reconcile existing signed transactions after ambiguous errors. Compare derivation with the pinned NEAR reference and transaction bytes/signatures with an independent official SDK.

### Owner-authorized NEAR staking — 2026-10-28

Support one Borsh `FunctionCall` action (variant 2) for `deposit_and_stake`, `unstake`, `unstake_all` or `withdraw_all`. Transfer remains variant 3 with unchanged fixtures. Preserve the existing Ed25519 account profile and full-access-key/nonce checks. Canonical arguments, 100 Tgas and exact attached deposit are signed; unstake and withdrawal attach zero. Pool state comes from final-block `get_account` for the owned account. Four epochs and compounded rewards follow the [pinned official contract](https://github.com/near/core-contracts/blob/1b0436c96379c63aee7d2429c6d5d24019031ab8/staking-pool/src/lib.rs).

Restrict mainnet contracts to direct children of `poolv1.near` or `pool.near`, testnet to `pool.f863973.m0`, and local acceptance to `pool.sandbox`. Factory namespace membership is not a code audit. Require exact private approval and preserve ambiguous child-receipt execution as UNKNOWN; never replace an unresolved staking transaction automatically. Independent SDK bytes/signatures, actual local WASM execution, graphical presentation and funded public-testnet execution have separate evidence in [NEAR_STAKING.md](docs/NEAR_STAKING.md).

## 6. Aptos

Use official reviewed Go SDK releases and pin **Legacy Ed25519**. Its initial authentication key/address is SHA3-256 of the public key with the legacy scheme byte. SingleKey Ed25519 is distinct; an SDK default change must never alter account identity. After rotation, an address remains stable although its authentication key changes. Sources: [Aptos accounts](https://aptos.dev/network/blockchain/accounts), [SDK account construction](https://github.com/aptos-labs/aptos-ts-sdk/blob/main/src/account/Account.ts).

Verify restored accounts' current authority and use supported authentication-key-to-address discovery or explicit association. Detect unsupported rotation/schemes; never present a new empty derived address as complete recovery. Native APT transfers need integer octas, validated address normalization, ledger chain ID, sequence reservations, expiration, simulation and bounded gas. Test failed-but-committed transactions as well as rejected submissions. Compare Go output with official TS SDK/CLI for address, BCS bytes, signing message, signature and node acceptance. Keyless, sponsored, orderless and multi-agent flows are outside v1.

## 7. Decred

Feed the full standard BIP39 seed into official Decred HD key construction. Do not use raw BIP39 entropy or truncate/hash the seed into a new scheme. Native Decred wallets use a different 33-word PGP encoding; [official recovery documentation](https://docs.decred.org/advanced/mnemonic-seed/) describes BIP39 restoration via binary/hex seed conversion. This is not direct mnemonic interchangeability and does not authorize a secret-export UI here.

Use `hdkeychain.Child()` at every path segment for dcrwallet compatibility. It preserves historical leading-zero behavior; `ChildBIP32Std()` can produce different descendants. Include a fixture that triggers this divergence. Legacy coin type 20 is distinct from new-wallet coin type 42 and is outside automatic import/discovery. Sources: [HD package](https://pkg.go.dev/github.com/decred/dcrd/hdkeychain/v3), [official wallet](https://github.com/decred/dcrwallet).

Use official `wire`, `txscript`, `stdaddr`, chain parameters and wallet transaction-authoring rules. Default receiving is ECDSA P2PKH. Decred hashes, address checksums, transaction format, expiry and sighash are not Bitcoin equivalents. Outpoints include regular/stake tree identity. Verify generated signatures in the official script engine before broadcast. Sources: [addresses](https://devdocs.decred.org/developer-guides/addresses/), [transaction format](https://devdocs.decred.org/developer-guides/transactions/transaction-format/).

Require supported dcrwallet-based synchronization/SPV or authenticated dcrd integration, with visible scan progress. Test change ownership, fake/duplicate UTXOs, tree identity, maturity, unknown scripts, atoms overflow, dust/fees, gap recovery, regular-tree disapproval and reorgs. Persist UTXO reservations and broadcast state across crashes. Do not spend unimplemented staking/mixing outputs or report incomplete discovery as zero balance.

## 8. TON

Use the multichain BIP39/SLIP-0010 profile, not TON-native mnemonic processing. Pin official **Wallet V5R1** code and code hash. Record public key, workchain, wallet ID, version/revision and StateInit; all contribute to identity. Current documented V5 wallet IDs are mainnet `0x7FFFFF11` and testnet `0x7FFFFFFD`; verify with the pinned wrapper and protocol. Sources: [wallet interaction](https://docs.ton.org/blockchain-basics/standard/wallets/interact), [official V5 contract](https://github.com/ton-blockchain/wallet-contract-v5).

Show a non-bounceable receive address for ordinary wallet funding, retaining testnet marking. Validate CRC and normalize user-friendly/raw formats without discarding network restrictions. Outbound bounce behavior depends on destination state; it is not merely copied from a UI string. Sources: [address formats](https://docs.ton.org/foundations/addresses/formats).

Test unfunded receipt, uninitialized accounts and first-send StateInit deployment; seqno/expiration races; duplicate signed messages; fees/storage reserves; incorrect code/wallet IDs; mainnet rejection of testnet-only addresses; and action success rather than seqno advancement alone. Verify signature authority if extensions/settings changed. Compare addresses, BOCs and signatures against official TS wrappers/emulator. Existing V3/V4 or native-phrase wallets require separate explicit profiles; never silently relabel them as V5R1.

## 9. Evidence required before compatibility claims

All fixtures use public synthetic seeds. Commit generator version, inputs, profile, expected outputs and provenance. Reference SDKs run only in tests with synthetic secrets; production key derivation and signing remain in Go. Only the owner-authorized dedicated Electron prompt and transient transport handle entered credentials or displayed/imported phrases; no production JavaScript SDK receives binary seeds or private keys.

| Area | Independent expected oracle | Remaining implementation gate |
|---|---|---|
| BIP39/SLIP-0010 | Published standard vectors; independent implementations | All imported lengths; normalization; invalid checksum; enabled passphrases |
| EVM | Pinned independent SDK and published EIP vectors | Multiple indices; signatures/bytes; all network receipts |
| NEAR | `near-seed-phrase`, official transaction SDK/CLI | Implicit/named authority and testnet acceptance |
| Aptos | Official TS SDK/CLI against Go | Explicit legacy scheme; BCS and rotation detection |
| DCR | Official dcrwallet/hdkeychain and script engine | Leading-zero fixture, discovery and simnet/testnet spends |
| TON | Independent SLIP-0010 plus official V5 wrappers/emulator | StateInit, network IDs, deployment and confirmed send |
| HyperCore | Official Python SDK against Go | Typed-domain parity; recipient activation; accepted HYPE transfers |
| ZEN | Official registry plus deployed contracts | Correct network/token/decimals; ETH fee coverage; successful transfer |

SDK-level vector parity is not proof that a third-party wallet application imports the same account. Claim application interoperability only after testing an identified app/version/profile using synthetic phrases, recording multiple indices and transaction acceptance. Every required asset needs fresh-install phrase restoration, deterministic address restoration where applicable, QR round trips, real local/testnet receive/send evidence and adversarial failures. Unverified or blocked cases remain visibly incomplete; coverage percentages cannot replace these gates.
