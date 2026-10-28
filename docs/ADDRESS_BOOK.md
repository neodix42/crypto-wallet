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
destination/comment in the same wallet is rejected. Retrying the exact same
create request returns its already saved contact without another encrypted
write or a duplicate row. **Edit** changes an existing contact explicitly;
**Delete** removes it.

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

Save uses the signer's durable acknowledgement; a subsequent list request is
not required to establish success. Invalid addresses, conflicting names or
destinations, and size limits produce specific status messages while preserving
the draft and unchanged contacts. A changed session requires unlocking again;
a failed encrypted write locks the wallet and asks the user to check storage
access and free space. An uncertain acknowledgement requires Refresh before
retrying. No submitted contact or underlying filesystem/provider error is added
to public error messages. Background screen updates acknowledge public field
edits without replacing newer typing with an older prefix. Save success, Clear
and Edit explicitly replace the composer, including fast saves whose busy and
completed views arrive together; rejected input keeps its correctable draft.

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
| PASS, backend unit/protocol | All eight active mainnets and 11 testnet/local descriptors, chain syntax, account-index/network separation, duplicate/size limits, old-vault compatibility, encrypted reload, exact durable retries, hostile decoded plaintext, persistence-failure lock, immutable prepared-payment preservation, strict IPC/epoch rejection and fixed private-safe error categories. `reports/addressbook-save-all-backend-tests.log`, `reports/addressbook-save-diagnostics-race.log`. |
| PASS, controller/race | Exact scoped save/list/edit/delete, durable acknowledgements without a second read, actionable rejections preserving drafts/contacts, explicit same-value composer reset, TON comment, asset-preserving selection, changed selection/session/lock invalidation, forged/oversized response rejection and no implicit payment call. `reports/addressbook-save-controller-race.log`. |
| PASS, current units/build | `make test-ci`, all 66 frontend component tests, current GUI helper tests, current backend/controller race checks and portable build/package pass. Delayed older field echoes and coalesced fast-save snapshots have explicit regressions. |
| PASS, targeted actual desktop | Five source-stable uninstrumented steps prove fresh/repeated Save clears its form, retains one selectable entry, shows exact invalid/duplicate feedback without hiding contacts, and restores the full recipient through Load/Use after actual restart/unlock. `reports/wails-addressbook-save.json`. Earlier failures and independent observer corrections are recorded in `STATUS.md`. |
| PASS, complete actual desktop | All 24 source-stable uninstrumented steps pass, including generated-root setup, fresh independent phrase restore with a different password, all eight matching addresses, and repeated Save/retry/rejection/Load/Use checks in that restored profile. `reports/wails-wallet.json`. |
| PASS, recovery and runtime | Final executable passes seven headless recovery/migration checks with three installations/24 descriptors, eight private-geth payment checks, actual WebKit security/inspector checks and ten installation checks. Exact candidate/scope are recorded in `STATUS.md`; these are not funded/public-network acceptance. |
| NOT_RUN | Cross-machine encrypted-profile migration, full clean OS/Wayland matrix, independent review and funded public-network/Bybit acceptance. No real user secrets or mainnet funds used. |
