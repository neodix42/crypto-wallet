# Storage directory and upgrades

The active storage root is `$XDG_CONFIG_HOME/cryptowallet`, normally
`~/.config/cryptowallet`. Mainnet and testnet remain separate `mainnet/` and
`testnet/` directories. Public appearance preferences use `ui/appearance.json`.
Settings → Wallet displays the running profile's directory and `vault.lwv` name.

Close all older wallet processes before upgrading. On application startup, an
existing `localwallet` root is renamed to `cryptowallet` in the same configuration
directory. The atomic no-replace operation moves the complete directory,
including current/previous ciphertext, encrypted journals and credentials,
interrupted encrypted writes and public preferences. It does not read/decrypt
vaults, copy secrets, change passwords, rederive keys or migrate the vault schema.
File inodes, contents and permissions remain intact. Installation and diagnostic
commands such as `--version` never migrate or open wallet data.

Migration uses pinned no-follow directory handles, verifies owner-only wallet
directories and obtains both profiles' existing single-writer locks. Concurrent
new-version migrations serialize on the configuration directory. A busy writer,
unsafe directory/lock or missing storage access stops startup. Symlinked paths
are not followed; nonregular, hardlinked or nonprivate writer locks reject.
No old-data failure falls back to creating an empty replacement wallet. The
configuration parent and any group with write access remain trusted, as with
the existing vault traversal; wallet roots and profiles stay owner-only.

If both `localwallet` and `cryptowallet` exist, the application refuses to select,
merge or replace either. Keep both intact, close every wallet process and preserve
separate private copies before resolving which root belongs at `cryptowallet`.
Do not delete a directory based on an empty balance or move individual encrypted
generations between wallets. Use separate OS users or private `XDG_CONFIG_HOME`
directories when independently inspecting preserved profiles. Do not run older
binaries after migrating: they do not know the new directory and can recreate
their former location. Phrase restoration into a fresh private configuration is
available separately; it cannot reconstruct installation journals or credentials.

The Go module name, IPC identities, encryption domains, derivation profiles and
`vault.lwv` filename retain their existing values. Renaming those would be a
different compatibility/security change. See [vault format](VAULT_FORMAT.md),
[recovery](RECOVERY.md) and [current evidence](STATUS.md).

Relevant checks:

```sh
./scripts/with-go.sh go test -race ./internal/profilepath ./internal/walletcore ./internal/nativewallet ./internal/webui ./cmd/cryptowallet
make test-recovery-portable
make test-wails-wallet
```

All migration tests use disposable configuration directories and synthetic
secrets. The headless recovery campaign additionally renames its own generated
profile to the legacy layout, runs the actual portable application, checks exact
ciphertext/preferences before unlock and compares all 24 descriptors at account
indices 0, 1 and 127. Separate fresh-copy phrase restoration remains required.
