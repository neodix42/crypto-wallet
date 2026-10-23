# Product and implementation requirements

## 1. Objective

Create a noncustodial desktop wallet using Go and Electron, with first-class support for Ubuntu 24.04 and later supported LTS releases. Produce amd64 `.deb` packages first. An AppImage is optional and must preserve sandboxing. Arm64 is a separate tested build target, not an inferred claim.

The application must create one cryptographically random BIP39 master recovery root and derive multiple deterministic accounts on the requested blockchains. It must be able to receive and send real protocol transactions after validation, not just display addresses. All secret material stays locally encrypted when not in active use. There are no custodial backend accounts or remote signing services.

The security and test documents are normative parts of this task. Words such as MUST describe implementation requirements; they do not assert that the code has already passed them.

## 2. Supported assets and domains

| Requested ecosystem | Required v1 support | User-visible distinction |
| --- | --- | --- |
| Ethereum | Native ETH on Ethereum; mainnet and an appropriate supported application testnet | Network and gas currency |
| NEAR | Native NEAR; implicit accounts and verified associations with existing named accounts | Account ID, access-key authority, mainnet/testnet |
| Hyperliquid | Native HYPE on HyperEVM and spot HYPE on HyperCore | Separate balances, destinations, transaction formats, and receipts |
| Aptos | Native APT using an explicitly fixed Legacy Ed25519 profile | Authentication scheme and account address |
| Horizen/ZEN | Canonical ZEN ERC-20 on Base and ZEN ERC-20 on current Horizen L3; ETH balance/send/receive on both for gas | Base versus Horizen; ZEN is not their gas currency |
| Decred | Native DCR ordinary payments; receive/change discovery and synchronization | UTXO, confirmation and synchronization state |
| TON | Native Toncoin with the specified BIP39 + Wallet V5R1 profile | Wallet contract, mainnet/testnet, activation state |

Mainnet identifiers and migration facts are documented with primary sources in [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md). Revalidate them during implementation. Do not show retired Horizen mainchain/EON as ordinary current receiving networks.

General ERC-20/NEP-141/Aptos fungible-token/TON jetton support, NFTs, DeFi, trading, staking, swaps, bridging, multisig, dApp browsers, WalletConnect, hardware wallets, legacy Horizen claims, and arbitrary message signing are outside this first version. ZEN ERC-20 support is required, not optional. Design adapters so later token support is possible without weakening signing policy. Do not add unrelated features before the required wallet is complete.

## 3. Wallet creation, accounts and recovery

### 3.1 Recovery root

Generate entropy with Go's operating-system-backed cryptographic random source. Never use time, usernames, hardware identifiers, passwords, Math.random, or a user-written sentence as seed entropy. Production builds must not contain deterministic RNG overrides.

Use the standard English BIP39 word list and standard mnemonic-to-seed conversion. Default generation is 12 words for the TON interoperability convention documented in TEP-3. An advanced 24-word BIP39 creation option is permitted, explicitly labeled and saved as BIP39; it must not be represented as a native TON phrase. Import valid English BIP39 lengths 12, 15, 18, 21, and 24, with exact checksum and normalization rules. Cross-wallet acceptance of each length is a separate compatibility claim. Do not translate the words or silently repair a phrase.

The app never displays the generated phrase. The word-count option describes the underlying recovery format, not a promise of paper backup. Preserve the actual entropy/word-list/profile information inside the encrypted vault so restoration is deterministic.

New wallets default to an empty BIP39 passphrase. Import supports an explicit optional BIP39 passphrase using the secure input path, with independent vectors. A passphrase typo creates a different valid wallet; no checksum can prove the passphrase is correct. Confirm imported public addresses and warn against interpreting an unexpectedly empty balance as proof of an empty wallet.

### 3.2 Multiple accounts

Start with one wallet root and one account per required family. “Add account” increments the documented index, not a random new seed. EVM Account 1 can use the same EOA address across Ethereum, Base, Horizen and HyperEVM; display the networks separately. HyperCore associates with the same explicitly selected EVM key, with its own balance domain.

Account index, derivation profile, authentication scheme, network, public key, TON contract parameters, Decred change branch, and any account association must be durable metadata. Labels are not identifiers. Removing an account from the UI hides it; it never renumbers remaining accounts or destroys its recovery mapping. Restoring an older backup must offer bounded rescan and manual index-range discovery for accounts added later.

Do not imply that keys on different chains are literally identical, or that identical EVM addresses imply balances can be transferred between networks without a bridge.

### 3.3 Strict no-reveal and portable backup

The generated/imported phrase, binary seed, BIP39 passphrase, private keys and extended private keys have no display/export/debug endpoint. A native Go input dialog may accept a known phrase, but must not provide a reveal toggle or echo it in errors. This is an intentional product choice.

For a generated wallet, require creating an encrypted portable backup before enabling its mainnet receive/deposit or send workflow. Reopen the written backup, authenticate/decrypt it in the secure component, and compare public recovery descriptors with the active vault. Do not call a successful filesystem write a verified backup. Verification must not overwrite the live vault or broadcast anything.

The backup includes the encrypted root material, any imported BIP39 passphrase, exact derivation profiles, account indices, network descriptors, TON contract parameters, NEAR named associations, Aptos address associations, DCR discovery settings and wallet birthday, plus the schema version. It must restore on a fresh supported Ubuntu installation without the original OS keyring, machine identity, user account name, or application installation.

The app password is a storage-encryption password. Changing it must not change any blockchain address. Old backups still require their old password; password rotation cannot revoke already copied backups. Communicate this during rotation and offer creation and verification of a replacement backup.

A backup contains secrets only in authenticated encrypted form. No plaintext seed JSON, recovery QR, email/cloud upload, or clipboard export. Forgotten passwords have no reset. Do not implement a hidden master password. A local, offline-capable recovery mode may restore and sign within the same security boundaries; it must not become a key-dumping utility.

Phrase-only import must reproduce this app's canonical accounts when the user already knows the phrase and any passphrase. It cannot guarantee recovery of arbitrary named associations, historical TON contracts, rotated Aptos accounts, or unknown third-party paths without discovery/metadata. Explain exactly what was searched and what remains unresolved.

## 4. Receive flow

Select asset, network and account. Show the complete public receive address with checksum/canonical formatting, its network, and a locally generated QR. Show account activation or deployment prerequisites where applicable. Never use a remote QR service or URL shortener.

Provide a Go-owned authoritative receive verification window containing the same network/address/QR, derived independently of renderer input. It is the confirmation surface for a deposit address. Electron's ordinary display can be compromised, so a renderer-supplied PNG or address string must not be treated as authoritative.

Default QR payload is the plain public address/account ID for broad compatibility. A plain EVM address cannot identify its network: display a clear network label next to it and instruct the user to choose that network at the sender. Offer network-aware payment URIs only where a specific standard is implemented and tested. Never invent a universal URI and imply other wallets understand it.

Support copying the public address and saving the QR image locally. The secure component must also support generating the verified QR directly. Neither operation may access a secret. Clipboard managers may retain public addresses; clipboard clearing must never be presented as secret-erasure protection.

For Decred, maintain the receive-address sequence and change tracking without address reuse caused by crashes. For TON, preserve network/bounce flags and correctly receive into an uninitialized wallet. For HyperCore, display only verified recipient/activation guidance; a shared EVM-format address is not proof of a funded or activated Core account.

## 5. Send flow

1. Select account, asset and actual ledger/network.
2. Enter the recipient or decode a QR image locally. Optional camera scanning requires explicit camera permission and must work without granting general renderer network access.
3. Validate the address/account, network, amount, memo/comment constraints and account authority in Go. No automatic send after scanning or following a link.
4. Obtain balance, spendability, fee estimates and the relevant network state. Stale, unavailable or incomplete data must be visible. Do not show unknown data as a confirmed zero.
5. Build an immutable, chain-specific transaction plan in Go. Show recipient, exact amount, asset identity, network, source, all outputs/change, native fee currency, maximum charge, and any deployment/activation requirements.
6. Request an authoritative native confirmation owned by the signing component, with password reauthentication for each send by default. Its text is generated from the exact transaction to be signed, not renderer descriptions.
7. Sign only the approved transaction, verify the produced signature locally, durably journal the signed transaction and its identity, then submit through the Go network component.
8. Reconcile submission, execution success and finality separately. Show pending, accepted, failed, unknown, replaced, reorged and confirmed states accurately.

An altered recipient, amount, asset contract, fee ceiling, chain, nonce/sequence, memo, expiry, input, output or transaction bytes invalidates the approval. Do not expose raw sign-byte, arbitrary typed-data, generic calldata or arbitrary derivation endpoints. ZEN sends must construct only the supported transfer operation; there is no automatic approval or bridge action.

Support exact decimal entry and “Max” using integer atomic amounts. An asset with more balance than the requested amount can still lack its separate gas asset. Calculate reserves and total fees using the relevant chain's current rules, including rollup charges on Base/Horizen where applicable. No floats for amounts, fees or nonces, and no silent truncation of excess precision.

Never automatically create a second payment after a timeout. Retry of the same signed transaction is different from signing another transaction; recovery must reconcile the persisted original identity. Concurrent sends must reserve nonce/sequence/UTXO resources, and a crash must not free a reservation that may already have reached the network.

## 6. Application UX and local operations

Provide creation, import, unlock/lock, account management, asset/network selection, balances, receive, send, transaction history, encrypted backup/restore, password change, network settings and an accurate support-status screen. Show advanced derivation metadata in a read-only public diagnostics view without exposing keys.

Use clear mainnet/testnet labels throughout. Test wallets and production wallets live in distinct profiles and directories; never import a test fixture automatically into a real vault. State the synchronization/trust mode for each chain, especially Decred SPV or trusted node access. Allow approved custom RPCs with explicit network verification and encrypted credential storage.

Automatic lock defaults to five minutes of real user inactivity and also occurs on OS session lock, suspend, logout, parent-process failure and application exit. Background polling must not reset inactivity. Lock cancels unapproved/in-progress signing and destroys secret-bearing process state; it cannot recall a transaction already signed or broadcast. Reopening always starts locked.

No analytics, remote fonts, remote script execution, background advertisements, browser-based seed import, mandatory cloud account, or unsolicited update execution. Essential public-chain queries reveal public addresses to the chosen providers; that privacy limitation must be accurately described.

## 7. Completion definition

Deliver all required chain adapters, not a reduced EVM-only wallet. Each required domain must pass independent derivation/signing fixtures, meaningful local protocol integration, clean recovery, transaction-state tests and the defined external testnet gate. Record PASS, FAIL, BLOCKED and NOT RUN honestly.

The repository must include reproducible build instructions, the actual Ubuntu package, dependency lockfiles and inventory, versioned vault and recovery-format documentation, an offline restoration procedure, a requirement-to-test map, coverage reports, retained fuzz regressions, mutation evidence, threat model and release checklist. CI must enforce the security floors instead of printing advisory numbers.

Independent review of the vault, secret-handling boundary and every signing adapter is a production release requirement. Automated tests are necessary evidence, not a substitute for that review. Finish everything that can be implemented and tested without real funds, then identify any genuinely unavailable external gate. Do not fabricate faucet success, blockchain receipts, benchmark results, audit approval or compatibility with a wallet version that was not tested.
