# Product and implementation requirements

> Owner update (2026-10-29): the shipped Wails UI now uses one trusted main window with reusable in-page dialogs and ordinary Wails bindings. It no longer launches a private UI self-role, isolated prompt page or one-use UI pipe. Normal selection/copy/cut/paste are enabled. The main UI is trusted with entered/displayed secrets and approval intent. This supersedes the separate-renderer and noncopyable-secret requirements below, which describe earlier designs. Go signer/core isolation, cryptography, reauthentication and transaction validation remain. See [the current trust model](docs/TRUSTED_UI.md).


> Current owner update (2026-10-28): [START_HERE.txt](START_HERE.txt) supersedes earlier Horizen-network coverage and Connect-only password requirements. Active coverage is eight mainnet networks/ten pinned assets; standard Ethereum/Base ERC-20 tracking and bounded read-only Base discovery are supported. Light/18px defaults and Settings Appearance apply to public/private Wails views. All unrelated secret-handling, approval, recovery and test gates remain mandatory.

## 1. Objective

Create a noncustodial desktop wallet using Go and Electron, with first-class support for Ubuntu 24.04 and later supported LTS releases. The owner requests amd64 Go executable artifacts and the complete portable desktop runtime without a Debian package. Portable packaging must preserve sandboxing. Arm64 is a separate tested build target, not an inferred claim.

The application must create one cryptographically random BIP39 master recovery root and derive multiple deterministic accounts on the requested blockchains. It must be able to receive and send real protocol transactions after validation, not just display addresses. All secret material stays locally encrypted when not in active use. Local wallet creation, recovery and signing require no custodial backend account or remote signing service. The owner's 2026-10-24 Bybit extension adds an optional custodial exchange account; deposited assets are controlled by Bybit, while wallet private keys and signing remain local.

The security and test documents are normative parts of this task. Words such as MUST describe implementation requirements; they do not assert that the code has already passed them.

## 2. Supported assets and domains

| Requested ecosystem | Required v1 support | User-visible distinction |
| --- | --- | --- |
| Ethereum | Native ETH on Ethereum; mainnet and an appropriate supported application testnet | Network and gas currency |
| Owner-authorized USDT extension | Pinned six-decimal USDT ERC-20 on Ethereum mainnet | Exact token contract and Ethereum ETH gas; no TON USDT jetton |
| NEAR | Native NEAR; implicit accounts and verified associations with existing named accounts | Account ID, access-key authority, mainnet/testnet |
| Hyperliquid | Native HYPE on HyperEVM and spot HYPE on HyperCore | Separate balances, destinations, transaction formats, and receipts |
| Aptos | Native APT using an explicitly fixed Legacy Ed25519 profile | Authentication scheme and account address |
| ZEN / Base ERC-20 | Canonical ZEN and approved standard ERC-20 tokens on Base; ETH balance/send/receive for gas | Exact Base network/contract; ZEN is not the gas currency |
| Decred | Native DCR ordinary payments; receive/change discovery and synchronization | UTXO, confirmation and synchronization state |
| TON | Native Toncoin with the specified BIP39 + Wallet V5R1 profile | Wallet contract, mainnet/testnet, activation state |

Mainnet identifiers and migration facts are documented with primary sources in [BLOCKCHAIN_COMPATIBILITY.md](BLOCKCHAIN_COMPATIBILITY.md). Revalidate them during implementation. Do not show retired Horizen mainchain/EON as ordinary current receiving networks.

The owner-authorized Bybit spot/USDT extension below narrowly supersedes the original exclusion of trading/swaps and adds one pinned Ethereum USDT asset. The 2026-10-28 NEAR staking extension below separately authorizes only the specified staking-pool operations. General ERC-20/NEP-141/Aptos fungible-token/TON jetton support, NFTs, other DeFi/trading/staking, bridging, multisig, dApp browsers, WalletConnect, hardware wallets, legacy Horizen claims, and arbitrary message signing remain outside this first version. ZEN ERC-20 support remains required. There are nine mainnet network wallets and twelve supported asset identities; existing blockchain coverage and secret-handling requirements are preserved.

## 3. Wallet creation, accounts and recovery

### 3.1 Recovery root

Generate entropy with Go's operating-system-backed cryptographic random source. Never use time, usernames, hardware identifiers, passwords, Math.random, or a user-written sentence as seed entropy. Production builds must not contain deterministic RNG overrides.

Use the standard English BIP39 word list and standard mnemonic-to-seed conversion. Default generation is 12 words for the TON interoperability convention documented in TEP-3. An advanced 24-word BIP39 creation option is permitted, explicitly labeled and saved as BIP39; it must not be represented as a native TON phrase. Import valid English BIP39 lengths 12, 15, 18, 21, and 24, with exact checksum and normalization rules. Cross-wallet acceptance of each length is a separate compatibility claim. Do not translate the words or silently repair a phrase.

After the app password is specified, Go generates the phrase and the dedicated Electron recovery dialog displays it for offline recording. Go verifies the selected words entered in a separate masked challenge. The ordinary portfolio renderer/public API never receives the phrase. The explicitly authorized prompt/reply channel carries transient phrase/password inputs through the coordinator and Electron main to a separate prompt origin, preload and nonpersistent session. These components are trusted with those secrets; Go generation does not make a compromised Electron process safe. Preserve the actual entropy/word-list/profile information inside the encrypted vault so restoration is deterministic.

New wallets default to an empty BIP39 passphrase. Import supports an explicit optional BIP39 passphrase using the secure input path, with independent vectors. A passphrase typo creates a different valid wallet; no checksum can prove the passphrase is correct. Confirm imported public addresses and warn against interpreting an unexpectedly empty balance as proof of an empty wallet.

### 3.2 Multiple accounts

Start with one wallet root and one account per required family. “Add account” increments the documented index, not a random new seed. EVM Account 1 can use the same EOA address across Ethereum, Base and HyperEVM; display the networks separately. HyperCore associates with the same explicitly selected EVM key, with its own balance domain.

Account index, derivation profile, authentication scheme, network, public key, TON contract parameters, Decred change branch, and any account association must be durable metadata. Labels are not identifiers. Removing an account from the UI hides it; it never renumbers remaining accounts or destroys its recovery mapping. Restoring a phrase must offer bounded rescan and manual index-range discovery for accounts added later.

Do not imply that keys on different chains are literally identical, or that identical EVM addresses imply balances can be transferred between networks without a bridge.

### 3.3 Electron phrase recording and fresh-install recovery

Owner policy updates: recovery uses a written BIP39 phrase, replacing encrypted portable-backup operations, and all application windows use Electron. Generate the phrase in Go after masked app-password entry, persist the root encrypted before showing it in a dedicated temporary Electron prompt, and require deliberate recording acknowledgment followed by correct masked word challenges verified in Go before enabling receiving or payment authorization. Cancellation leaves the same encrypted root pending; it must never create a replacement root. No production Go/GTK window is opened.

The phrase has no ordinary public IPC response, plaintext-file export, print, save or QR operation. Clipboard access is limited to the explicit authenticated backup Copy phrase action below. The dedicated prompt loads no remote resources, never uses browser storage, and has no Node access, generic IPC, reveal toggle or portfolio API. Clear secret fields and phrase DOM before submitting/cancelling and revoke the one-use prompt on lock, hide, navigation, crash or session change. JavaScript/Chromium copies cannot be guaranteed erased. Private keys, extended private keys, binary seeds and entropy remain in Go; imported optional BIP39 passphrases are masked input only. A pending or legacy vault can finish setup only after app-password reauthentication verified in Go. Owner update (2026-10-27): after setup is confirmed, Settings → Wallet offers **Back up recovery phrase**. Every display requires explicit confirmation and current app-password reauthentication verified in Go. A distinct one-use `recoveryBackup` prompt displays numbered words only in the isolated Electron dialog; its ordinary request/result contain no secrets. Cancellation, wrong password, lock or stale state must prevent display. This read-only operation cannot alter the root, recovery completion, vault generation or journal; it has no file/print/QR export and does not reveal an optional BIP39 passphrase. The later owner instruction authorizes an explicit **Copy phrase** button in this authenticated backup window only. Copy the current Go-provided words held in Electron main, with no secret input/output in the copy IPC. Bind it to the exact live delivered backup prompt, pending request and trusted main frame; deny setup, stale, locked, wrong-kind and ordinary-renderer calls. Clipboard errors are value-free. Warn that other apps/history can retain the copied phrase; do not claim guaranteed erasure. Whole-directory rollback to an older valid pending generation cannot be prevented without an external monotonic anchor.

A completely fresh installation must restore canonical wallets with the recorded phrase and a specified local app password; an optional original BIP39 passphrase is additionally required when one was used. The app password encrypts this installation and does not derive blockchain keys: either the same or a different app password must recover identical canonical addresses. Keep the encrypted local vault, current generation and prior crash-recovery generation protected by the existing file/cryptography rules.

Remove encrypted-backup creation, file verification and file restore from product UI/public IPC. Phrase recovery does not carry named/rotated associations, account labels, discovery counters, RPC settings or the local payment journal. Offer explicit account-index/discovery controls and explain those limits; never invent missing history or claim an incomplete scan means zero funds. Do not silently change derivation profiles or contract parameters.

## 4. Receive flow

Select asset, network and account. Show the complete public receive address with checksum/canonical formatting, its network, and a locally generated QR. Show account activation or deployment prerequisites where applicable. Never use a remote QR service or URL shortener.

Provide a dedicated Electron receive dialog containing the network/address/QR authored and verified in Go, independently of ordinary renderer input. Copy uses the current Go-provided address held in main, never a string supplied by the prompt or portfolio renderer. The ordinary portfolio can be compromised without being allowed to replace that data; compromise of Electron main or the dedicated prompt itself can spoof the display and is outside this separation claim.

Default QR payload is the plain public address/account ID for broad compatibility. A plain EVM address cannot identify its network: display a clear network label next to it and instruct the user to choose that network at the sender. Offer network-aware payment URIs only where a specific standard is implemented and tested. Never invent a universal URI and imply other wallets understand it.

Support copying the public address and saving the QR image locally. The secure component must also support generating the verified QR directly. Neither operation may access a secret. Clipboard managers may retain public addresses; clipboard clearing must never be presented as secret-erasure protection.

For Decred, maintain the receive-address sequence and change tracking without address reuse caused by crashes. For TON, preserve network/bounce flags and correctly receive into an uninitialized wallet. For HyperCore, display only verified recipient/activation guidance; a shared EVM-format address is not proof of a funded or activated Core account.

## 5. Send flow

1. Select account, asset and actual ledger/network.
2. Enter the recipient or decode a QR image locally. Optional camera scanning requires explicit camera permission and must work without granting general renderer network access.
3. Validate the address/account, network, amount, memo/comment constraints and account authority in Go. No automatic send after scanning or following a link.
4. Obtain balance, spendability, fee estimates and the relevant network state. Stale, unavailable or incomplete data must be visible. Do not show unknown data as a confirmed zero.
5. Build an immutable, chain-specific transaction plan in Go. Show recipient, exact amount, asset identity, network, source, all outputs/change, native fee currency, maximum charge, and any deployment/activation requirements.
6. Request a dedicated Electron confirmation whose text is generated in Go from the exact transaction to be signed, with password reauthentication verified in Go for each send by default. The ordinary renderer cannot supply approval text, authorize a different prompt or fabricate its one-use reply.
7. Sign only the approved transaction, verify the produced signature locally, durably journal the signed transaction and its identity, then submit through the Go network component.
8. Reconcile submission, execution success and finality separately. Show pending, accepted, failed, unknown, replaced, reorged and confirmed states accurately.

An altered recipient, amount, asset contract, fee ceiling, chain, nonce/sequence, memo, expiry, input, output or transaction bytes invalidates the approval. Do not expose raw sign-byte, arbitrary typed-data, generic calldata or arbitrary derivation endpoints. ZEN sends must construct only the supported transfer operation; there is no automatic approval or bridge action.

Support exact decimal entry and “Max” using integer atomic amounts. An asset with more balance than the requested amount can still lack its separate gas asset. Calculate reserves and total fees using the relevant chain's current rules, including rollup charges on Base/Horizen where applicable. No floats for amounts, fees or nonces, and no silent truncation of excess precision.

Never automatically create a second payment after a timeout. Retry of the same signed transaction is different from signing another transaction; recovery must reconcile the persisted original identity. Concurrent sends must reserve nonce/sequence/UTXO resources, and a crash must not free a reservation that may already have reached the network.

## 6. Application UX and local operations

Provide creation, import, unlock/lock, account management, asset/network selection, balances, receive, send, transaction history, Electron phrase recording/restore, password change, network settings and an accurate support-status screen. Show advanced derivation metadata in a read-only public diagnostics view without exposing keys.

Use clear mainnet/testnet labels throughout. Test wallets and production wallets live in distinct profiles and directories; never import a test fixture automatically into a real vault. State the synchronization/trust mode for each chain, especially Decred SPV or trusted node access. Allow approved custom RPCs with explicit network verification and encrypted credential storage.

Automatic lock defaults to five minutes of real user inactivity and also occurs on OS session lock, suspend, logout, parent-process failure and application exit. Background polling must not reset inactivity. Lock cancels unapproved/in-progress signing and destroys secret-bearing process state; it cannot recall a transaction already signed or broadcast. Reopening always starts locked.

No analytics, remote fonts, remote script execution, background advertisements, remote/web-browser seed import, mandatory cloud account, or unsolicited update execution. The authorized local isolated Electron prompt is the only phrase-input/display interface. Essential public-chain queries reveal public addresses to the chosen providers; that privacy limitation must be accurately described.

### 6.1 Owner-authorized Bybit Exchange extension — 2026-10-24

Provide an Exchange tab with Deposit, Swap and Withdraw. Connecting a system-generated HMAC API key uses the dedicated masked Electron input path and encrypted local storage. The ordinary renderer receives no key, secret, beneficiary data or authenticated request payload. No connection, quote, startup, build or test automatically sends funds.

Deposit through the existing real on-chain wallet plan, approval, signing and journal pipeline, after matching the exact Bybit chain/native or full token identity and required memo. Swap only an active spot asset/USDT pair in either direction, using exact decimal spending amounts, fresh last/bid/ask observations, explicit relative slippage, no leverage or borrowing, immutable signer approval and password reauthentication. An internal Funding/Unified transfer requires a separate durable identity and approval. Withdraw only on-chain to the selected owned wallet on its verified network, with master-key permissions and Bybit address whitelisting. Account eligibility, IP restrictions, custody, fees and limits remain under Bybit's control.

UAE withdrawals may collect the eight documented beneficiary fields privately for the UAE endpoint. Required but unimplemented Türkiye/Kazakhstan/EU/Indonesia withdrawal questionnaires must gate withdrawal before an internal transfer. Unknown/suspended/ambiguous routes stay explicit; do not guess chain IDs or imply every wallet asset has a Bybit listing. Preserve all existing wallet networks.

Persist operation identities before transmission. Lost acknowledgments and incomplete receipts remain UNKNOWN, block conflicting writes and must never cause automatic replacement. Bybit does not expose withdrawal request-ID lookup; do not infer a match from amount/address/time. Last prices are not execution guarantees, market orders can fill partially, and exchange-reported withdrawal completion is not independently verified chain finality. API credentials, beneficiary details and local remote-operation journals are not recoverable from the BIP39 phrase. [Exchange requirements and evidence](docs/EXCHANGE.md) supplement all existing security and release gates.

### 6.2 Owner-authorized NEAR staking extension — 2026-10-28

Provide a Staking tab within each NEAR wallet. Default mainnet to the exact `astro-stakers.poolv1.near` contract; require an explicit supported pool on testnet. Support stake (`deposit_and_stake`), partial/all unstake (`unstake`, `unstake_all`) and withdrawal (`withdraw_all`). Only stake attaches the approved NEAR; other operations attach zero. Read `get_account` using the actual owned account, display exact staked/unstaked amounts and `can_withdraw`, and retain unknown states on unavailable RPC. Staked balances include compounded rewards; do not invent a separate earnings figure or fixed withdrawal date.

Serialize a single allowlisted Borsh FunctionCall, preserve Ed25519/nonces, and reuse the existing private approval, password authentication and encrypted journal. Show exact pool, method, arguments, gas, attached NEAR, effect amount and fee reserve in signer-authored confirmation. All-method amounts are observations, not limits on the contract's current balance. Root-call success alone cannot establish successful downstream withdrawal: failed/missing/ambiguous receipts retain UNKNOWN and the original reservation. Actual local-contract acceptance and optional fresh-secret, capped public-testnet acceptance are separate evidence. No mainnet spending or external seed import is authorized.

## 7. Completion definition

Deliver all required chain adapters, not a reduced EVM-only wallet. Each required domain must pass independent derivation/signing fixtures, meaningful local protocol integration, clean recovery, transaction-state tests and the defined external testnet gate. Record PASS, FAIL, BLOCKED and NOT RUN honestly.

The repository must include reproducible build instructions, the actual Ubuntu-compatible portable artifacts, dependency lockfiles and inventory, versioned vault and recovery-format documentation, an offline restoration procedure, a requirement-to-test map, coverage reports, retained fuzz regressions, mutation evidence, threat model and release checklist. CI must enforce the security floors instead of printing advisory numbers.

Independent review of the vault, secret-handling boundary and every signing adapter is a production release requirement. Automated tests are necessary evidence, not a substitute for that review. Finish everything that can be implemented and tested without real funds, then identify any genuinely unavailable external gate. Do not fabricate faucet success, blockchain receipts, benchmark results, audit approval or compatibility with a wallet version that was not tested.
