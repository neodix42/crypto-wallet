# Address book

The main-window **Address book** tab sits immediately after **Settings**. Each
contact belongs to an owned wallet account and its exact network. Ethereum and
Base contacts remain separate even when their owned addresses match; deterministic
account indices also have separate books. All eight active mainnet networks and
the existing testnet/local profiles use their registered address validators.

Select **Wallet / network**, enter **Name** and **Address**, and choose **Save
address**. The network is assigned from that wallet; it is never editable contact
metadata. A TON contact can include an optional comment of up to 120 UTF-8 bytes.
Names are limited to 80 bytes and addresses to 256. Control characters, invisible
formatting and multiline metadata are rejected. Limits are 256 entries per wallet
and 2,048 per vault. A case-insensitive duplicate name or duplicate exact
destination/comment in the same wallet is rejected. **Edit** changes an existing
contact explicitly; **Delete** removes it.

In a wallet's Send form, the book icon opens that account's contacts; the save
icon opens a contact form prefilled with its recipient and, for TON, comment.
**Use address** fills the same wallet's recipient/comment, preserves its selected
asset, clears any stale prepared-payment preview and returns to Send. It does not
prepare, approve, sign or broadcast anything. Review the complete actual recipient,
network, asset, amount and fee in the usual private approval before paying. Names
are labels, not ownership verification.

Contacts are an optional member of the existing authenticated encrypted `vault.lwv`,
alongside wallet metadata. Existing vaults without contacts remain readable. Saving
or deleting requires an unlocked, recovery-confirmed wallet; no additional app
password is requested for public contact metadata. Signer validation checks owned
account, exact network, chain syntax and limits before persistence. An unsuccessful
durable write locks the signer rather than returning an uncommitted success.

The ordinary UI sees only public contacts for the selected wallet. Changing that
selection clears the prior list/composer; ordinary tab navigation preserves its
draft. Lock and account/session replacement clear cached contacts and drafts.
Delayed or foreign-scope replies cannot repopulate the book. Contact edits never
modify an immutable prepared payment or its journal/reservations. Choosing a contact
only invalidates the local preview and requires a new exact payment preparation.

The recovery phrase restores keys, not contacts, token watch policies, RPC
credentials or journals. A fresh installation must recreate its address book.
Keep the original encrypted profile safe if this metadata is needed. No plaintext
contact export, remote synchronization or address lookup service is introduced.

## Verification checklist

| State | Evidence and limits |
| --- | --- |
| PASS, backend unit/protocol | All eight active mainnets and 11 testnet/local descriptors, chain syntax, account-index/network separation, duplicate/size limits, old-vault compatibility, encrypted reload, hostile decoded plaintext, persistence-failure lock, immutable prepared-payment preservation and strict IPC/epoch rejection. `reports/addressbook-backend-tests.log`, `reports/addressbook-backend-race.log`. |
| PASS, controller/race | Exact scoped save/list/edit/delete, TON comment, asset-preserving selection, draft/ongoing-read navigation, changed selection/session/lock invalidation, foreign/duplicate/oversized response rejection and no implicit payment call. `reports/addressbook-controller-race.log`. |
| PASS, current units/build | `make test-ci`, all 59 frontend component tests, 82 final GUI helper tests, current backend/controller race checks and portable build/package pass. |
| PASS, actual desktop | Four targeted and all 24 complete uninstrumented Wails steps pass with stable sources. Real Save/Load icons persist a named recipient, Use fills Send, clearing then Load/Use restores the complete address, and no payment is prepared or approved. The complete run repeats this after fresh-profile phrase restore. `reports/wails-addressbook-tokens.json`, `reports/wails-wallet.json`. |
| PASS, recovery and runtime | Final executable passes seven headless recovery/migration checks with three installations/24 descriptors, eight private-geth payment checks, actual WebKit security/inspector checks and ten installation checks. Exact candidate/scope are recorded in `STATUS.md`; these are not funded/public-network acceptance. |
| NOT_RUN | Cross-machine encrypted-profile migration, full clean OS/Wayland matrix, independent review and funded public-network/Bybit acceptance. No real user secrets or mainnet funds used. |
