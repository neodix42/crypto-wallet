# Funding availability and rejected Bybit deposit records

The supplied screen distinguishes two observations: the original 6 GRAM chain
payment is confirmed, and Bybit reports 5.99998566 GRAM safely available in
Funding. The separate deposit-record verification failed. The wizard requires
that record before enabling its Funding transfer; it must not treat an exchange
balance as proof of this exact deposit.

The old `PROVIDER_INVALID_RESPONSE` code covered both parser errors and exact
identity mismatches. It cannot identify the rejected field from the supplied
screen. The actual private response was not inspected. No API key, API secret,
app password, recovery phrase or real wallet profile is requested or read.

## Changes

After credit verification, the default transfer amount is the smaller of its
conservative deposit budget and the verified Funding availability, rounded down
to wallet precision. Thus a 6 GRAM deposit with 5.99998566 GRAM available suggests
5.99998566 GRAM, preserving a smaller valid user amount. An original recorded
transfer is unchanged. Unknown credit still disables the button; changing a
draft amount never prepares, signs or sends anything.

The Bybit parser now treats the unique deposit ID as an opaque bounded string.
Its previous ASCII identifier restriction was not part of the documented API
contract. Nonempty IDs with punctuation/Unicode are accepted; empty, oversized,
control-containing or duplicate IDs remain rejected. This fixes a demonstrated
schema incompatibility, not a claim about which field this account returned.

Failures now identify one of 17 fixed record-field categories or ten fixed
matching categories, such as `DEPOSIT_RECORD_TYPE`, `DEPOSIT_RECORD_ID`,
`CREDIT_TRANSACTION`, `CREDIT_ADDRESS` or `CREDIT_MEMO`. The existing read code
prefix remains `EXCHANGE_READ_DEPOSIT_CREDIT__`. Core status and explicit-action
error dialogs give a fixed human-readable reason. Raw provider bodies, headers,
credentials and field values never become errors. Unknown categories are
sanitized; other endpoints cannot acquire credit-specific diagnostics.

Transaction, coin, network, destination, memo, amount, reported-fee and status
checks remain. Documented optional Travel Rule fields retain their existing
handling. JSON duplicate/case-shadow fields cannot replace canonical credit
status or identity. Pagination and duplicate-record checks stay bounded. No
balance/time heuristic, replacement deposit, automatic Funding transfer or
new signing authority is introduced.

Public BOCs show that the destination transaction has no outgoing messages and
charges 14,342 nanoGRAM: 6 minus 0.000014342 is 5.999985658. This is consistent
with the eight-decimal Funding display, but does not prove Bybit's private
credit record or explain which field failed. The forwarding hypothesis was
checked and rejected; no speculative trace relaxation was added.

Primary sources:
[Bybit deposit-record schema](https://bybit-exchange.github.io/docs/v5/asset/deposit/deposit-record),
[Bybit TON transaction-hash guidance](https://www.bybit.com/en-GB/help-center/article/How-to-Retrieve-Your-Transaction-Details-from-Various-Blockchain-Explorers?category=4c624888cddd3260eb).
The existing public fixture and provenance remain in
`internal/chains/ton/testdata/bybit-native-deposit-public.json`.

## Evidence checklist

| State | Scope |
|---|---|
| PASS | Complete Bybit client tests/races/vet, documented response example, opaque IDs, malformed fields, mandatory status/memo/identity, duplicate/case-shadow refusals and private-value canaries. `reports/bybit-deposit-response-diagnostics-tests.log`, `reports/bybit-deposit-response-diagnostics-race.log`, `reports/bybit-deposit-response-diagnostics-vet.log`. |
| PASS | Native/IPC/client focused suite; actual supplied GRAM amounts, exact rounding and valid user choices, credit gating and specific safe reasons. `reports/bybit-credit-diagnostics-focused.log`. |
| PASS | Coordinator tests for all ten matching failures and 17 parser categories, exact reason text, no new deposit or Funding POST, unknown-category sanitization. Core race suite. `reports/bybit-credit-core-diagnostics-final.log`, `reports/bybit-credit-core-race.log`. |
| PASS | Final portable Go build, affected aggregate races/vet, all five client semantic mutants killed with stable sources. `reports/bybit-credit-diagnostics-build.log`, `reports/bybit-credit-diagnostics-race.log`, `reports/bybit-credit-diagnostics-vet.log`, `reports/bybit-credit-diagnostics-client-mutations.log`. |
| PASS | Six actual stable-source X11 Appearance/Exchange-navigation/disconnected-wizard checks on final binary SHA256 `9cb4e08f543744e4443a5805b426f729c018967a1a6d74bcee9fc5bfbc8932ee`. `reports/bybit-credit-diagnostics-appearance.json`. These do not invent a connected credit response or Funding debit. |
| PASS, exact-source hosted desktop | [Tests 41601762568](https://github.com/neodix42/crypto-wallet/actions/runs/41601762568), job 125801935504, source `fc85271195b5f52fdfca818e6f719998fef8cccb`: 38 full graphical workflows, five TON, seven recovery and nine actual private-geth checks. Artifact 13214804818 digest `a139186d17300736bf468766ee22d19f7d0a2b69a687d4a32a544469869f065d`; all nine report/source/binary bindings verify. `reports/bybit-credit-diagnostics-hosted-gui/summary.json`. |
| PASS, exact-source hosted backend | Same run/source, job 125801935615: all 30 JSON gates, full races/stress, 81 mutants in ten campaigns, 26 measured fuzz targets, 13 actual local integrations and 24 restored descriptors across eight networks. Artifact 13216162374 SHA256 `1bed15f324681ce43999fb45abf3c02fb650c661ddee62c9a531699fe6b6ec27`; 1,681 exact Git-source hashes match, zero mismatches. `reports/bybit-credit-diagnostics-hosted-checks/verification.json`. |
| PASS, publication | [Build/release 41601762413](https://github.com/neodix42/crypto-wallet/actions/runs/41601762413) and normal [release 0.1.46](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.46) match the exact source/tag/version and authenticated publisher. The sole portable archive contains cryptowallet, install-user.sh and PNG/SVG icons; SHA256 `c0d0450217a6d8032659baedf348f907bac0364e6878f1bcf2a319ab36adc39e`. `reports/bybit-credit-diagnostics-release/summary.json`. |
| PASS, downloaded executable | Six actual stable-source X11 checks on the authenticated 0.1.46 executable, SHA256 `f3d3f1053548f286dbf8d7caf29a47a821ca04811df3cef9873d4237688e8645`. `reports/bybit-credit-diagnostics-published.json`. |
| FAIL, superseded development run | The first focused coordinator run preceded the new IPC allowlist, so new field categories were correctly sanitized to the generic safe code and diagnostic assertions failed. After the exact allowlist was added, the final tests and whole hosted suite pass; unknown fields and unrelated stages remain rejected. `reports/bybit-credit-core-diagnostics.log`, `reports/bybit-credit-core-diagnostics-final.log`. |
| BLOCKED | Exact private-response cause: awaiting the non-secret Bybit transaction ID/credited amount, deposit-record row or the updated field-specific reason. The supplied generic code contains no such detail. |
| NOT_RUN | Private authenticated Bybit acceptance or funded transfers. No real wallet storage, keys or automatic mainnet funds are used in development. |
