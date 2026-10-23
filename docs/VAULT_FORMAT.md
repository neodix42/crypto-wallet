# Vault and recovery format v1

The live vault and portable backup use the same authenticated binary envelope. This document describes `internal/vault`; it is not a cryptographic audit. No plaintext keystore or OS-keyring wrapping is used.

All integers in the 72-byte header are unsigned big endian. The complete header is XChaCha20-Poly1305 associated data.

| Offset | Bytes | Meaning |
|---|---:|---|
| 0 | 8 | ASCII `LWVLT001` |
| 8 | 2 | Format version 1 |
| 10 | 1 | KDF identifier 1: Argon2id v1.3 |
| 11 | 1 | AEAD identifier 1: XChaCha20-Poly1305 |
| 12 | 4 | Argon2 memory, KiB |
| 16 | 4 | Argon2 passes |
| 20 | 1 | Argon2 lanes |
| 21 | 3 | Reserved, must be zero |
| 24 | 16 | Fresh OS-random salt |
| 40 | 24 | Fresh OS-random nonce |
| 64 | 8 | Ciphertext bytes including 16-byte tag |
| 72 | variable | Ciphertext and tag |

The derived storage key is 32 bytes. Default parameters are 262144 KiB, 3 passes, 4 lanes. Accepted memory is 65536–524288 KiB in multiples of 1024, passes 3–6, exactly four lanes. Format, lengths and these resource bounds are checked before any KDF. A process-wide mutex permits one active KDF. Plaintext is nonempty and at most 16 MiB. Truncation, appended data, unsupported versions/algorithms, nonzero reserved bytes and any authentication failure release no partial plaintext. Password creation accepts 12–4096 UTF-8 bytes with no normalization. Decryption permits legacy shorter inputs only to authenticate an existing envelope; no shorter password can be created by v1. Fresh salt/nonce are drawn with `crypto/rand` for every encryption, with no fallback.

The encrypted plaintext is strict JSON schema version 1: wallet ID, positive generation, `bip39-english-v1`, entropy (16/20/24/28/32 bytes), optional BIP39 passphrase, mainnet/testnet environment, creation birthday, verified-backup flag, account descriptors, transaction journal and encrypted RPC settings/credentials. Duplicate keys at every depth, unknown fields, trailing data and nesting above 32 are rejected. Account count and operation count are each bounded at 10000. Root/account/profile/journal metadata are authenticated together. Signed artifacts remain encrypted with the root. Public snapshots omit all signed bytes, entropy, passphrases, passwords and RPC credentials.

Descriptors preserve family, immutable profile, actual index, network, public key, full address, contract/discovery/association parameters and hidden status. Hidden accounts remain recoverable and retain their index. Profile IDs and exact protocol details are in source and [chain evidence](STATUS.md). Decred stores xpub, receive/change counters, birthday, gap, per-branch address limit and block budget. NEAR named associations and Aptos rotated-address associations are checked against the derived key during recovery and current network authority before signing. TON retains its V5R1 parameters. Password rotation changes envelope encryption, never derivation.

Live profiles are `$XDG_CONFIG_HOME/localwallet/{testnet,mainnet}`, defaulting to `~/.config/localwallet/…`. `vault.lwv` is current, `vault.previous.lwv` preserves the previous ciphertext, and `writer.lock` is a single-writer flock. Directory creation and traversal use verified directory FDs and no-follow semantics. Private final directories must be owned by the current UID with mode 0700; files are regular owner-only mode 0600 with one link. Symlinks, hardlinks, FIFOs and unexpected owners/modes are rejected. Atomic same-directory ciphertext writes use an unpredictable exclusive temporary file, full write, file fsync, rename and directory fsync. Backups never overwrite an existing filename. Temporary files contain ciphertext only. Tests inject failures at write/sync/rename/directory-sync and resource limits; either the complete old or complete new file may survive a crash. On persistence error the engine locks and returns no undurable signed artifact.

Generation detects inconsistent application state within a loaded session; it cannot detect an attacker restoring the entire directory or an older valid portable backup. There is no external monotonic counter or anti-rollback service. An older backup lacks subsequent issued indices, changed associations, settings and transaction records. The restoration procedure requires bounded discovery and careful reconciliation before another payment. Unknown outcomes preserve reservations; they are not automatically re-signed.

Only schema 1 is supported. Unknown future versions fail closed. There is no implemented migration to a different schema, so no successful cross-schema migration is claimed. Keep a separately verified backup before replacing an application version. The previous live file is a crash-recovery aid, not an independent portable backup. Manual recovery must preserve all candidate files, use an empty profile, and authenticate/validate the intended backup before making changes.
