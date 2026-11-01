# Arbitrum history and wallet navigation

The reported Arbitrum failure is reproduced using only the supplied public
address. The configured Blockscout legacy and v2 indexes return HTTP 403; the
default official RPC does not supply the Alchemy address-transfer index.
Routescan does not provide this Arbitrum chain. An unavailable index must not
be converted into an empty history or a recent global-block scan.

Arbitrum mainnet now has a fixed-host public Arbiscan fallback for candidate
transaction hashes, with separate native/token streams and bounded pages.
Go never renders that HTML, follows its links/scripts, reads its displayed
amounts or adopts token metadata. Requests disclose only the public address,
page and page-size values. Redirects and cookies are disabled; RPC credentials
and URLs are not copied into these requests. Changed/challenge/error tables,
unbound rows, invalid hashes, excess bodies/rows and repeated pages fail closed.

Each candidate still passes the existing configured-chain identity,
transaction, canonical block, receipt, owned effect and approved token-runtime/
precision checks. Cross-stream duplicates cannot create extra transfers.
The cursor retains the original owner, network, index source and checked RPC
head. Public index ordering can change and is not a frozen archival snapshot;
candidate coverage remains incomplete, and changed/above-head observations
require refresh. Exhaustion never independently proves that no older funds or
transactions existed. Other networks, local nodes, signing and token policies
retain their existing behavior.

The production-Go read-only probe loads three checked owned transactions:
native ETH, USDT0 and USDC. Untracked token candidates are omitted. The supplied
address and downloaded provider responses remain in ignored diagnostic reports;
no user vault, secret, signature or public-chain payment is used.

## Current controls

- Exchange, Address book and Settings are accessible icon buttons at the
  header's top right. Version/environment sit beside the brand on the left.
  Account and wallet tabs occupy their own separate row.
- Wallets and Receive dialogs use Copy address icons with accessible names,
  keyboard operation and tooltips. Public address copying still calls the
  native clipboard immediately; it does not request wallet approval.
- Transaction history has Refresh and Info icons beside its heading. Info
  discloses the full provider scope and limitations on demand. The table
  remains read-only; unavailable/paused states remain explicit.
- Track new token opens a plain-text contract selector on existing supported
  ERC-20 and NEP-141 networks. Cancellation, lock, close, changed account or
  stale replies cannot import a policy. Go metadata validation, exact policy
  consent and current-password approval remain required. Password, phrase and
  exchange credential dialogs remain masked/unchanged.
- Settings → Networks selects an owned active network and opens its existing
  trusted-RPC configuration/approval dialog. RPC settings are encrypted and
  shared by all wallet accounts on that selected network. Credentials are not
  exported to the public settings page. Opening Settings from a wallet selects
  its network. Wallet pages no longer contain Configure trusted RPC.

## Evidence checklist

| State | Evidence and scope |
|---|---|
| PASS, diagnosis and production public read | Default index failures and public Arbiscan candidates are reproduced. The unchanged transaction/effect verification path supplies checked ETH, USDT0 and USDC observations. `reports/arbitrum-history-ui/public-production-check.json`. No user storage or spending. |
| PASS, native dialogs/controllers | Complete native/prompt/bridge race suites pass. Tests cover exact contract-only routing, cancellation, malformed/stale/locked/closed/navigated replies, current wallet/network binding, icon-only wallet copy identity, hidden inline token forms/RPC controls, Info-only scope and owned-network RPC selection. `reports/arbitrum-history-ui-native-race-final.log`. |
| PASS, frontend | 121 component/protocol tests and project TypeScript check pass, including separate header navigation, accessible icon actions, escaped Info disclosure, and plain public contract input while secret inputs stay masked. `reports/wallet-header-token-dialog-frontend-tests.log`, `reports/wallet-header-token-dialog-typecheck.log`. |
| PASS, prompt fuzz | The new publicInput seed participates in bounded protocol fuzz. `reports/wallet-public-input-fuzz.log`. |
| PASS, parser and checked effects | Full EVM race suite, 61,223 bounded parser fuzz mutations, two compiling semantic negative controls, captured native/token empty schemas and four missing-row regressions pass on stable final sources. The public production probe returns three checked transactions. `reports/arbitrum-history-ui/backend-final-checklist.json`. |
| PASS, complete local aggregate/build | Final `make test-ci`, portable build and managed installation pass. The updated desktop helper suite passes 189 checks. `reports/arbitrum-wallet-layout-final-aggregate.log`, `reports/arbitrum-wallet-layout-final-build.log`, `reports/icon-harness-helpers-final-v6.log`. |
| PASS, actual graphical workflows | All 29 full generated-root/recovery app checks and seven clipboard workflows pass on the final binary with stable sources. Focused native token popup (10), main-row copy (3) and header/history/settings layout (11) checks also pass on that same binary. `reports/wails-wallet-icons-full-v5.json`, `reports/wails-wallet-icons-clipboard-v5.json`, `reports/wails-wallet-icons-{near-popup,main-row,history-layout}-v4.json`. Five final TON-only checks also pass with stable sources: `reports/wails-ton-icons-final.json`. |
| NOT_RUN, hosted acceptance/publication | Exact-source hosted CI and publication are pending. |
| NOT_RUN, external gates | Funded public sends, actual funded graphical approvals/pending operations, complete installed OS/Wayland and physical-machine matrix, and independent review. This is scoped implementation evidence, not production/audit certification. |

The first native run failed because two older assertions still required the
removed inline token form. The first new close test incorrectly required an
already closed app to clear its terminal busy flag. Updated tests preserve
their identity/lifecycle assertions; neither initial failure is counted as
final acceptance.

The initial parser silently skipped four malformed or unidentified nonempty rows;
a retained expected-failure overlay demonstrates that gap. The final parser
rejects these rows and explicitly accepts only the captured native/token empty
schemas. An intermediate strict parser incorrectly rejected the native empty
page's secondary hint; final compatibility regressions and the live read probe
pass. These earlier failures do not count as final acceptance.

Actual graphical development attempts also exposed test-driver assumptions:
selected icon outlines, Settings prose mistaken for a subtab, overview headers
mistaken for wallet markers, a 34-pixel copy-button assumption, decorative OCR
fragments, a Receive title/field substring ambiguity, and a hovered native copy tooltip still open during the strict single-window check. Each correction has
helper regressions and preserves exact native clipboard checks without glyph
substitution. Concurrent GUI attempts collided on display allocation; final
runs are sequential. Stale-binary and failed attempts remain diagnostic records,
not passing candidate acceptance. The TON-only recovery scenario now reveals the address with ordinary bounded scrolling before measuring its icon; a clipped icon still fails. Focused/full reports use their recorded stable driver versions and the same final application binary; later driver-only changes do not alter that binary.
