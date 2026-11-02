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
| PASS, native/controller | Full native/prompt/bridge races, group ownership/utility/close/paging tests, distinct network counts, nineteen asset precision/receive checks, immediate validation updates and exact action guards. `reports/account-layout-final-native-race.log`, `reports/account-layout-native-controls.log`. |
| PASS, validation controls | All eight active networks and nineteen defaults, checksum/friendly-address flags, imported precision, malformed inputs, unknown assets and stale/foreign drafts. Two compiling semantic controls kill removed early preparation and recipient checks using designated runtime assertions. `reports/account-send-validation-negative-controls.json`. |
| PASS, frontend/contract | 145 frontend tests, project TypeScript and actual Go JSON binding pass. Empty optional-field compatibility, nested multi-account tabs, retained utility context, icon placement, zero-visible-row groups and invalid raw input feedback are covered. `reports/account-groups-ui/`, `reports/account-layout-wire-contract-final.log`. |
| PASS, aggregate/build/helpers | Complete `make test-ci`, portable build and managed installation pass. All 200 desktop helpers pass. `reports/account-layout-aggregate-final-v4.log`, `reports/account-layout-build-final-v4.log`, `reports/account-layout-helpers-final-v7.log`. |
| HISTORICAL CHECKPOINT, earlier focused layout | Seven uninstrumented app checks pass with stable sources on final binary `016cd36927149cba4feb4ac20d94024a06f46ebffe89b815416360aba0726f2a`: header/count/table refresh, parent/child groups with distinct addresses, ETH/Base/TON action rows, network/numeric rejection and invalid-field Max alignment, header Lock and Enter unlock. `reports/account-layout-target-v4.json`. |
| NOT_RUN, current aggregate/build/GUI | The extended account-menu/Exchange candidate needs final aggregate/build and actual updated workflows. Earlier binary/checkpoints do not pass this gate. |
| PASS, encrypted presentation backend | Full model/vault/signer/core/walletsigner races and vet pass. Actual signed UNKNOWN artifacts survive label/archive updates and encrypted restart; next index, bounded/scoped metadata, cancelled/stale confirmations and typed dispatch are tested. `reports/account-presentation-backend-checklist.json`. |
| PASS, economic input controls | Full native/prompt/bridge races plus four separately compiling negative controls pass with stable sources. Sell/Buy USDT/USDC rules, eligible assets, zero-API rejection, ownership and exact venue bounds are covered. `reports/account-extended-final-native-race.log`, `reports/account-input-validation-negative-controls-final.json`. |
| NOT_RUN, hosted CI/publication | Exact-source hosted tests and the next release have not run. |
| NOT_RUN, external gates | Funded public transfers, full installed physical OS/Wayland matrix and independent review. No real profile, user secrets or automatic mainnet funds are used. |

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
