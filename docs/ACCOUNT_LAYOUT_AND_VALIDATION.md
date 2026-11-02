# Account navigation and Send validation

Account tabs contain only deterministic account groups. Each open wallet has an
explicit parent group and appears on that group's separate wallet subtab row.
Closing an active wallet returns to its own group; closing an inactive wallet
keeps the current page. Utility pages retain the selected parent. Previous/Next
accounts preserve access beyond eight visible groups without a group dropdown.

The header shows the current group's distinct network count, Exchange, Address
book, Lock wallet and Settings. Version/environment stay left. Refresh wallet
state is an icon in the table header. The portfolio displays `Balance: 25.00$`
without a tilde; the underlying value remains the observed USDT subtotal with
pricing/missing-asset scope on hover, not a new fiat conversion or firm quote.
Unknown and partial observations keep their explicit states.

Send, Receive, tracking/discovery/status and Hide controls share one nonwrapping
wallet action row. Narrow screens scroll that row. Existing confirmation,
recovery and busy guards stay intact. NEAR Staking retains its separate staking
controls rather than exposing an ordinary Send toggle without a transfer form. History's redundant action-pause sentence
and the empty Send form's fee-helper sentence are removed; loading, failure,
fee estimates, reserves, errors and incomplete-history information remain.

Go validates recipients with the existing exact network adapters and positive
amounts with the selected asset's bounded decimal parser. UI fields show local
errors; invalid drafts cannot request a quote or prepare a payment. Max requires
a compatible recipient, but can fill an empty amount. Amount fields use decimal
input hints and validity patterns, keep invalid raw text for correction and
never silently reinterpret exponents, separators or malformed pastes. Final
coordinator/signer validation and explicit fee/approval/password gates remain.
Compatible syntax cannot prove ownership, account existence or funding; several
networks deliberately share address representations.

The Go/TypeScript contract now carries bounded parent IDs and entry-only error/
decimal-hint metadata. Shared fixtures bind actual Go JSON to frontend decoding,
including serialized empty strings. No secret or transaction authority is added.

## Additional account and Exchange controls

The account-tab `+` creates the next index after the latest stored account,
including hidden/archived and retired-network indices. The signer computes and
confirms this index; the UI cannot reuse a deleted hole. Right-click and
ContextMenu/Shift+F10 menus rename or delete exact parent/child tab targets.
Names and deleted-group markers are encrypted metadata separate from immutable
account descriptors. Delete confirms a local archive; keys, funds, original
signed bytes and operation records remain. Show hidden wallets exposes archived
entries for restoration; an empty Accounts shell preserves `+` and Settings.

Rename/archive confirmations do not request another password. Funds-moving
reauthentication is unchanged. Metadata exports remain page-scoped, bounded and
revision-bound. Label edits cannot change derivation, addresses or plan digests.

Amount, recipient and generic field/action rows put accessible error text below
all controls. Rejected oversized UTF-8/NUL edits keep the previous value visibly
intact, emit no field RPC and show fixed value-free footer feedback; they cannot
leave the displayed recipient different from Go's retained draft.

Exchange amount feedback uses exact positive decimals, network asset precision,
and fresh provider/wallet/asset/side/USDT-or-USDC venue rules. Buy quote precision
is distinct from on-chain token precision. Invalid/stale requests cannot start
economic API work. Metadata refresh remains available. Actual API limits, fees,
fresh approval and custody rules remain enforced by the unchanged core. KuCoin's
existing spot restriction and mainnet-only exchange policy remain.

## Evidence checklist

| State | Scope |
|---|---|
| PASS, controller/protocol | Full affected native/prompt/bridge/core races, exact parent context, close/paging, eight network address validators, nineteen defaults and metadata prompt routing. Actual coordinator/signer subprocess tests retain encrypted names, original signed UNKNOWN bytes and reservations through restart. `reports/account-recovery-error-native-final-race.log`, `reports/account-recovery-actual-process-race.log`. |
| PASS, encrypted presentation/recovery | Full final signer/walletsigner races and vet; staged derivation/storage failures, exact cancellation, bounded/scoped metadata and next-index rules. `reports/account-recovery-backend-checklist.json`. |
| PASS, input controls | Four separately compiling semantic controls pass baseline and fail their designated runtime assertions after removing Send preparation/address or Exchange pre-API/quote bindings. Final 949-file snapshot is stable. `reports/account-input-validation-negative-controls-final.json`. |
| PASS, frontend/contract | 153 frontend tests, TypeScript and shared Go JSON contract: nested ownership, pointer/keyboard menus, input precision/alignment, oversized-input retention and complete error dialogs. `reports/account-recovery-frontend-final.log`. |
| PASS, aggregate/build/helpers | Complete `make test-ci`, portable build/managed installation and 214 graphical-helper tests. `reports/account-recovery-final-unit-v5.log`, `reports/account-recovery-cancel-build.log`, `reports/account-recovery-cancel-helpers.log`. |
| PASS, actual extended layout | Eleven uninstrumented checks pass with stable sources on binary `b0bd995677c29cb0a4ca460397e648da98444025852fbd6afaf5d0d411187eb9`: header/table layout, distinct parent/child identities, invalid Send/Exchange inputs, field/action alignment, + next-index creation, pointer/keyboard rename, cancelled/accepted archive, encrypted restart, hidden restoration, index nonreuse and header Lock/Enter unlock. `reports/account-recovery-layout-v2.json`. This scoped campaign predates the later typed exchange-cancellation correction, which has separate full-flow evidence below. |
| PASS, final local full GUI | All 35 generated-root/fresh-copy/recovery graphical checks pass with stable sources on binary `6aa6ee8d74d1c31d030b1d600fa19e14c60c8357ed97ab553a01a9a9ed322243`, including completed typed Bybit cancellation. `reports/account-recovery-full-v3.json`. The five earlier local TON checks on `b0bd9956…` remain separate; final exact-source TON acceptance is hosted below. |
| PASS, exact-source hosted CI | Both jobs in [run 41424837883](https://github.com/neodix42/crypto-wallet/actions/runs/41424837883) succeed for `aea16d899ee233a5dc3d56570c0cecc7e5d4e198`: 35 full graphical, five TON, seven recovery and nine actual private-geth steps; complete units/frontend/helpers, races/stress, interoperability, 81 semantic mutants/ten campaigns, 13 actual local integrations, 26 fuzz targets, USDT/dependency/recovery/crash checks. Both artifacts' digests, producers and source/binary bindings verify. Dedicated eleven-step account-layout/menu acceptance remains separately scoped local evidence. `reports/account-recovery-hosted-gui-v5/summary.json`, `reports/account-recovery-hosted-checks-v5/verification.json`. |
| PASS, publication and downloaded app | Normal [release 0.1.41](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.41) matches that exact source, publisher, tag and app version; its four-file inventory and published bytes verify. Seven actual recovery/error checks pass on the authenticated downloaded executable. `reports/account-recovery-release-final-v5/summary.json`, `reports/account-recovery-published-v5.json`. |
| NOT_RUN, external gates | Funded public transfers, live exchanges, full installed physical OS/Wayland matrix and independent review. No real profile, user secrets or automatic mainnet funds are used. |

The initial native assertion still expected Max enabled for an empty recipient;
it is updated to require validation while preserving all asset/receive checks.
The first semantic-control test used a worker-goroutine fatal assertion; that
rejected harness result is retained separately. Final controls capture calls
atomically, drain owned work and require the designated runtime failure.
The first Go JSON fixture binding normalized nil slices through cloneView and
did not match the generator; the corrected test binds the actual encoding.
These development failures are not final passing evidence.

The first new graphical target failed after header checks because its Receive
locator also matched Save receive QR. The next passed header, nesting, actions
and invalid-input/Max alignment, then failed a Settings locator whose old height
bound excluded the new subtab row. Public diagnostics show the complete controls;
final helpers use exact word context and a unique complete Settings tab row.
These earlier source-stable checkpoints are retained, not final acceptance.

Validation feedback initially placed Max below its input. The corrected Amount
row retains accessible error association but renders the single error beneath
both controls; actual input/Max bottom edges match. The final candidate also
preserves the preexisting separate NEAR Staking view. These changes have separate
final build, aggregate and affected race evidence before graphical acceptance.

The first full workflow's NEAR locator also matched the word Wallet in the
moved Hide wallet action. Public inspection confirms the real Wallet/Staking
row; final helpers require the unique complete pair with ambiguity checks. The
focused seven-check layout report remains bound to the same application binary;
the later correction affects only that NEAR-specific test driver.

The following full run correctly opened Staking, but the same words in its
`NEAR Wallet · Staking` heading made the new pair locator ambiguous. Exact known
heading context is now excluded, while duplicate real navigation rows still
fail. Five focused actual staking checks pass with stable sources on the final
application binary. This correction changes only the test driver.

The first extended context-menu measurement mistook the wallet-subtab divider
four pixels above the menu for a second top border. Numeric candidate outlines
and a guarded public-fixture image establish the cause. The final helper requires
both complete vertical sides below rounded corners; a regression supplies the
extra divider explicitly. The rejected run also records a driver edit during
execution (`sourceStable: false`), so it is not acceptance. The final eleven-step
campaign is source-stable and changes no product UI for this driver correction.
