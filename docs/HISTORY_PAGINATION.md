# Bounded portfolio and history pages

The local implementation now supports bounded account lists, local payment-journal pages, HyperCore history and TON archival history. Decred synchronization and public testnet acceptance were excluded from this work. These local passes do not establish production readiness or third-party provider completeness.

## Account and journal storage boundaries

The signer keeps its complete authenticated vault and reservations. Its internal `Engine.Snapshot()` and `Pending()` semantics remain available to Go code; production wire snapshots use `SnapshotPage()` and omit the journal entirely. A page carries at most 128 accounts and 256 KiB, plus the total account/operation counts and a decimal vault revision. A private position binds kind, revision, offset and applicable account/asset filter. A changed generation invalidates the old position.

`account` retrieves one public descriptor, and `operation` retrieves one exact archived operation over the inherited Go-only channel. Sending/reconciliation no longer enumerates every signed operation merely to find one payment. The public journal path always removes the signed artifact and never frees a reservation. The retained legacy private `pending` operation fails explicitly when its response exceeds the private frame budget; the live wallet uses the typed single-operation path instead.

The coordinator replaces every private position with a random, single-use 128-bit capability. It binds that capability to the exact IPC session, coordinator epoch, page kind and account/asset query. Capabilities expire after five minutes and disappear on lock, restart or RPC reconfiguration. Refresh invalidates older continuations for the same public query. Private positions are neither portfolio parameters nor URLs. Explicit JSON null, non-string values, invalid UTF-8, NUL and strings exceeding 4,096 bytes are rejected for the new page request fields.

The portfolio gathers one revision of all account pages before displaying it, checking counts and duplicate identities. A failed or changed page produces an unavailable state, without installing a partial portfolio or authorizing creation over the existing vault. Account gathering is bounded by 10,000 records, 16 MiB and 256 page requests. The Settings journal requests the selected account/asset only, with a **Load older payments** button. It clears continuations on selection, refresh and lock, and rejects stale or duplicate rows. The display is bounded by 10,000 records and 16 MiB. No journal scan reconstructs missing payments from a restored phrase.

## Chain history

`model.HistoryPager` is optional. Pager adapters supply positions only to Go core. Public `chainHistory` returns `{entries,nextCursor,complete,scope}` with at most 128 records and 256 KiB per response, leaving room inside the 1 MiB framed IPC limit. Legacy adapters still supply one explicitly bounded snapshot; core divides it into deliverable pages and does not turn snapshot exhaustion into a claim of complete blockchain history.

A chain scan rejects invalid/duplicate identities and repeated adapter positions. It retains at most 10,000 identity checks and 4 MiB per scan, with at most 16 contexts and 16 MiB combined retained data. The portfolio limits accumulated chain observations to 10,000 records and 4 MiB. A bound, provider error, malformed continuation or missing data remains an explicit error/uncertainty; it does not become a successful empty balance or failed payment. Exhausting a disclosed scope never independently proves finality or absence of funds.

The existing Decred report shape and synchronization are retained, with a public response-size check. There is no Decred archival pagination or incremental synchronization claim in this change.

## TON archival behavior

The adapter requests archival TON Center-compatible transaction BOCs in pages of at most 100. The official API documents newest-first `lt`/`hash` continuation and archival requests. The implementation derives the next exact identity from the oldest BOC's `PrevTxLT` and `PrevTxHash`, avoiding a repeated boundary transaction. It independently rehashes every BOC, checks the account, exact requested first identity, strictly descending linked logical times, duplicates, gaps and the terminal zero link. Provider metadata and URLs never select an address or continuation authority. See the [official API documentation](https://docs-next.ton.org/api/v2/transactions/get-transactions) and [TON Center API source](https://github.com/toncenter/ton-http-api), checked 2026-10-24.

A short page with a nonzero previous link still has a continuation. An empty initial result has `complete:false`: no account-chain head was established. An empty continuation that omits an expected linked transaction is an error. `complete:true` means the supplied locally linked account history reached a terminal zero link within this trusted provider's scope; it is not consensus authentication. Native-message/credit history remains `OBSERVED`, with separate execution and recipient-payment checks. Per-page interpretation retains the 256 messages per transaction and 1,000 outgoing messages per page bounds; jettons and other currencies remain outside the native history interpretation.

Reconciliation separately searches up to 32 linked pages (at most 3,200 transactions) for both the exact signed external body and its exact outgoing message at the recipient. It retains the existing successful-source-action, exact spend-effect, recipient-credit and bounce checks. A matching old transaction can now reconcile beyond the former latest-100 window. A missing trace, archival inconsistency or exhausted search remains UNKNOWN/SUBMITTED as appropriate; there is no automatic new signature, nonce, resubmission or payment. Searching still older unresolved payments through a persisted incremental reconciliation checkpoint is not implemented by this bounded per-attempt search.

HyperCore's separate time-window, page commitment and equal-timestamp handling are documented in [EVM/Core evidence](EVM_CORE.md).

## Local evidence

| Status | Evidence |
| --- | --- |
| PASS | Public Go snapshot/journal tests page 10,000 synthetic account descriptors and 401 local journal records without truncation, artifact disclosure or reservation changes. |
| PASS | Actual signer dispatch unlocks an encrypted vault exceeding 1 MiB, with 300 independently derived public account descriptors and 300 synthetic archived operations; every response stays bounded and the original late signed artifact remains available through typed lookup. No network transaction is made. |
| PASS | Coordinator tests page 1,000 public accounts and 400 journal records, reject cross-session/kind/filter/revision, expired, refreshed and replayed capabilities, and preserve late-operation reconciliation. |
| PASS | TON fixture tests traverse 205 linked historical BOCs and reconcile exact source/recipient observations located after 103/107 transactions. Added chain nodes are synthetic extensions of retained emulator traces, not new consensus-acceptance evidence. |
| PASS | Negative cases cover cursor tampering, missing linked transactions, wrong networks, ordering, duplicates, false termination, provider outages, saturated/bounded searches, forbidden signed public artifacts and malformed/null page fields. |
| PASS | Frontend tests exercise complete-generation account gathering, stale/locked/changed/duplicate/truncated/oversized pages, local and chain Load older behavior, context changes, inert rendering and failure feedback. |
| NOT_RUN | Public funded testnet acceptance, real third-party provider archival completeness and identified wallet-application interoperability. |
| NOT_RUN | Complete installed Ubuntu X11/Wayland acceptance and separate physical-machine recovery for this change. |
| NOT_RUN | Persisted arbitrarily old unresolved-payment reconciliation and an exhaustive archival-recovery claim. Current limits are explicit and keep uncertain outcomes reserved. |

Focused command logs are under `reports/history-*`; final candidate-wide source hashes, coverage, race/security evidence and remaining gates are recorded by the release checklist. Earlier focused coverage logs are development evidence and are not a substitute for the final source-frozen candidate report.
