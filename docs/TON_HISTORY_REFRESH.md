# TON history and separate wallet IDs

TON history belongs to an exact network and receive address. Wallet V5R1
subwallet 0 and subwallet 42 have different StateInit cells and addresses even
when they use the same public key. A new default does not move funds or history
between them.

The reported empty address was checked using public data only. TON Center
reported an uninitialized account, zero balance, no transaction head and an
empty archival history. Independent TonAPI mainnet and testnet queries also
reported no activity. A production-Go comparison using the earlier deployed
wallet's public code/data showed that the supplied address is its separate
subwallet-42 identity; the earlier active account uses subwallet 0. No user
storage or recovery material was opened. Identifying addresses and captured
responses remain in ignored local reports.

The final production-Go read found four checked transaction BOCs containing
three native transfers on the original account, and none on subwallet 42.
Both pages remain incomplete because the separate token account-chain query
does not establish complete historical absence. These are provider
observations, not independent consensus proofs.

Open the existing original TON wallet if it is listed. Otherwise use
**Settings → Wallet → TON recovery profiles**, select **Original subwallet 0
(legacy address)** and the original account index, then **Add / recover TON
profile**. Check the recovered receive address before using it. This adds the
original identity; it does not replace the new wallet, move funds or submit a
transaction. The phrase and public key alone do not make two wallet IDs the
same account.

An empty initial provider page now triggers a fresh account-state check. If
the account has a recorded transaction head, the adapter requests that exact
logical time and hash once and checks the returned BOC against both the head
identity and account. An omitted, malformed or contradictory head is an error,
not an empty history. A zero-head uninitialized account receives explicit
provider-scoped guidance. It remains `complete:false`: an RPC observation is
not a consensus proof of no historical activity. JSON `null` is rejected as an
invalid transaction array. Normal nonempty pages, bounded continuations,
canonical USDT checks and actual on-chain comments keep their existing checks.

The wallet's empty TON history explains that other wallet IDs have separate
addresses and points to the recovery controls. History remains read-only;
there are no transaction review or confirmation controls in its table. Native
GRAM and canonical USDT observations belong to the whole wallet, regardless
of the asset selected for Send.

The state/head relationship follows the official TON API definitions for
[account information](https://docs.ton.org/api/v2/rpc/get-address-information)
and [archival transaction requests](https://docs.ton.org/api/v2/rpc/get-transactions).
Provider availability and completeness remain trust limits.

| State | Evidence |
|---|---|
| PASS, public diagnosis | TON Center identity/state/archival response, independent TonAPI checks and production-Go original/subwallet-42 comparison reproduce the empty result. `reports/ton-history-refresh/`, `reports/ton-history-refresh-independent/`. |
| BLOCKED, additional index | TON Center v3 returned HTTP 403. This is an unavailable check, not evidence of empty history. |
| PASS, focused regressions | Seventeen empty/head/null/stale/contradictory-provider cases and same-key profile separation pass; full TON/core races and vet, seven actual adapter/BOC-to-core paging cases, typed GRAM/USDT refresh and fourteen recovery-hint visibility cases pass. `reports/ton-history-refresh/{backend-race,ton-full-race}.log`, `reports/ton-history-core-{transport,ton-race,ton-vet}.log`, `reports/ton-history-controller-race.log`. |
| PASS, actual local contracts | Existing native payment/deployment and canonical USDT TVM execution passes for both V5R1 profiles and network domains. `reports/ton-history-actual-tvm.log`. |
| PASS, independent negative controls | Two compiling semantic mutations remove exact-head identity or unused-account state binding. Unmodified controls pass; each mutation fails the expected runtime assertion, with stable sources. Build failures or panics are not counted. `reports/ton-history-negative-controls.json`. |
| PASS, build/install | Portable Go build and managed local installation pass. `reports/ton-history-build-install.log`. |
| FAIL, superseded diagnostic placement | The first full local command collected an ignored probe `.go` file as a package. All wallet suites passed, but that command failed. The diagnostic source now has a non-package `.go.txt` suffix; package discovery passes. The failed attempt is retained in `reports/ton-history-test-ci.log`. |
| PASS, corrected full local command | Complete `make test-ci` passes current Go/frontend/helpers and stress after the probe-source correction. `reports/ton-history-test-ci-final.log`. |
| PASS, hosted desktop | Exact source `2431aa75259e521c2b48b45931293a49a78bf798` passes the Wails job in [run 41227873277](https://github.com/neodix42/crypto-wallet/actions/runs/41227873277): 29 graphical checks, 4 TON checks, 7 recovery steps and 9 actual private-geth steps. Artifact 13052294039 matches SHA256 `10322480350f8e1f639769c2b02720f86515da9d3a280fe8b4f3b6373669118e`; runtime binary binding and all 1453 private-geth source hashes match. Full/TON/geth sources are stable. `reports/ton-history-hosted-gui/summary.json`. The exact successful-empty RPC/recovery-hint branch is covered by typed controller tests; the actual graphical TON workflow is offline and does not certify that branch against public RPC. |
| PASS, complete hosted checks | Both jobs in [run 41227873277](https://github.com/neodix42/crypto-wallet/actions/runs/41227873277), attempt 1, succeed for the exact source above: units/frontend/helpers, 81 semantic mutants across 10 campaigns, full races/stress, interoperability, 13 actual local integration gates, 25 executed fuzz targets, dependency, 7 recovery and crash-canary checks. Artifact 13053585961 matches SHA256 `3dd612d17357743c7d611718be8ba50cf07242ab328ddd125ca0954ec64d8182`; all 30 JSON reports pass and all 1587 reported source files match, with no declared unstable snapshot. `reports/ton-history-hosted-checks/41227873277/verification.json`. |
| PASS, publication | Normal [release 0.1.33](https://github.com/neodix42/crypto-wallet/releases/tag/0.1.33), [build 41227873335](https://github.com/neodix42/crypto-wallet/actions/runs/41227873335), tag and binary version match the exact source. Independent publisher/source/provenance and published-byte verification pass. One portable archive contains exactly the executable, user installer and two icons; archive SHA256 `7b65efb499924518a6afa5791edf678a2708b62656d5c350831382922568c267`. `reports/ton-history-release/summary.json`. |
| NOT_RUN, external acceptance | Funded public transfers, complete independent archival proofs, physical-machine/Wayland acceptance and independent security review. No public transaction was signed or sent. |
