# Automatic Bybit deposit confirmation

The reported 6 GRAM deposit was delivered successfully. Public TON transaction
BOCs link signed external message
`956c289e323fb73723362df1bc26db154f720a30bc9eda1cf10cbf016581fb89`
to sender transaction
`9aabe0765796346d0ce447f63a4ec565ab41daec90ac77316e4c21bb891722d1`
and recipient transaction
`5ab9780b09037d425c9d8a4b43d17ae39582f7768ddc05c2fbc3efbdb610b974`.
The source action and matching recipient credit both succeeded. The native
adapter already reconciles this transfer; no execution checks needed relaxing.
Public fixture provenance is retained in
`internal/chains/ton/testdata/bybit-native-deposit-public.json`.

The old wizard required a manual credit refresh and used only the recipient
transaction identity. Bybit can index either transaction of this linked TON
payment. Its region-specific `travelRuleStatus` field is also absent from valid
responses in non-compliance areas, while the old parser required it. The user's
private Bybit response and encrypted profile were not inspected, so these are
verified implementation defects rather than a claim to have identified that
account's exact private response.

## Behavior

After the initial exact deposit approval/password, the wallet signs and sends
the original operation once. While Exchange is open and unlocked, a quiet check
runs at most once every five seconds, with at most one such request in flight.
It reconciles the original recorded chain operation, never sends it again, and
never signs, cancels or automatically moves exchange funds. Lock, session loss,
provider changes, workflow changes and foreground actions invalidate stale
results. Polling does not extend the unlock timeout.

On verified on-chain delivery, the signer atomically advances its current
workflow to step 2 and preserves concurrent draft edits and later operations.
The wallet shows a single informational confirmation and the deposited asset,
amount, checked transaction identity and Funding/Unified total and available
balances. Exact Bybit credit continues checking on this page. Unavailable reads
are shown as pending/unavailable rather than zero or successful credit.

The normal submitted wizard deposit is omitted from the automatic pending
confirmation inbox, including after fresh unlock. Deliberate recovery of an
unsigned or signed-but-not-submitted original remains available; all original
encrypted records, signatures and duplicate-send protection remain intact.

Funding transfer requires the exact successful normal Bybit deposit record,
matching verified transaction, chain, coin, destination and memo. A supplied
source address must also match. Explicit pending/denied review states still
block it. Missing mandatory fields, conflicting records, excessive amounts,
unknown balances and cursor loops do not authorize a transfer. A balance by
itself never identifies this deposit. The API does not define whether its
reported amount is gross or net of `depositFee`. Both are displayed separately.
The transfer budget uses the smaller of original/reported amounts, less a
nonempty reported fee, and is also capped by safe Funding availability. This
can leave a remainder if the API amount was already net. Empty fees remain
labelled unreported, not asserted zero. Each subsequent economic action retains
its own approval.

## Evidence checklist

| State | Evidence |
|---|---|
| PASS | Read-only live default-provider reconciliation of the reported transfer; public BOC regression and altered source action, recipient execution, credit, message linkage, amount and memo cases. No private profile or secret used. |
| PASS | TON/model race tests and vet; actual pinned canonical USDT TVM execution for both TON wallet profiles. |
| PASS | Focused native controller tests for polling, lifecycle invalidation, nonmodal retries, exact credit gating, fee-adjusted defaults and startup inbox filtering. |
| PASS | 158 unchanged React checks. `reports/deposit-confirmation-frontend.log`. |
| PASS | Complete final `make test-ci`, affected full Go race suite, vet and rebuilt portable executable. `reports/deposit-confirmation-ci-final.log`, `reports/deposit-confirmation-race-final.log`, `reports/deposit-confirmation-vet-final.log`, `reports/deposit-confirmation-build-final.log`. |
| PASS | Final `make test-security-ci`; all 81 mutants in ten campaigns killed, every campaign source-stable. `reports/deposit-confirmation-security-final.log`. |
| PASS | Six actual final-binary X11 Appearance/Bybit-cancellation/Exchange navigation/disconnected-wizard checks; source-stable, binary SHA256 `e87402897d11b054b207ebf54d27393075c19cfde1debea79ba0f598134c64ec`. `reports/deposit-confirmation-final-appearance.json`. |
| NOT_RUN | Hosted exact-source full desktop/recovery/private-geth and aggregate fuzz/integration campaigns; publication and publisher verification. Results will be recorded after push. |
| NOT_RUN | This user's private Bybit credit, account restrictions and funded Funding/Unified transfer. Development never opens the real profile, imports real credentials or sends mainnet funds. |

Earlier development failures are retained separately: two inbox mocks needed to
accept the new local navigation read, and the new TON coordinator fixture used
the obsolete TON display symbol instead of the current GRAM exchange symbol.
Both are fixed with the production guards intact. The first full local desktop
campaign began before the final fee-budget change, so it cannot certify the
final candidate; its own stability gate remains authoritative. These attempts
are distinct from final passing results above.

Bybit contract:
[deposit records](https://bybit-exchange.github.io/docs/v5/asset/deposit/deposit-record).
On-chain success and Bybit custody credit remain different observations.
