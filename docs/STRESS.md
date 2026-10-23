# Stress and soak evidence

Measured on Ubuntu 26.04 amd64, Go 1.27.3, AMD Ryzen 9 5950X 16-core CPU (32 logical CPUs). Every credential, phrase, seed and approval in this harness is public synthetic test material. No public endpoint or live funds are used.

The bounded profile runs production code for 100,000 malformed/bounded IPC requests and 10,000 address/amount/unsupported-URI/duplicate-metadata cases spanning 12 registered testnet profiles. It also runs 1,000 scheduling scenarios with 16 competing workers, real EVM transaction encoding/signing/signature verification, a bounded fake RPC ledger, lost acceptance responses, reopened journal identities and final reconciliation. The fake node independently decodes the transaction, enforces nonce/source/chain/value/gas and records one credit per signed identity. These tests establish scheduling and rejection behavior; actual consensus acceptance is covered by the independent integration report.

Two additional cycles use the actual signer Engine: import the public BIP39 fixture, persist encrypted state, deny a signature while locked, reopen vault files into a fresh Engine, cancel the unapproved operation, authorize and sign a new payment, lose its broadcast response, reopen the encrypted signed journal, reconcile the original bytes, configure and scan bounded Decred discovery, advance a receive address, add an account group, scan growing EVM history and authenticate an encrypted backup. The test UI supplies only the explicit synthetic approvals; native GTK and OS process crash tests are separate gates. Argon2id remains the production 256 MiB/3-pass/4-lane profile.

Budgets are declared in source before the measured runs: ordinary RSS/HWM 1 GiB, post-GC heap 512 MiB, at most 8 residual goroutines, Go runtime memory limit 768 MiB, parser p99<=10 ms, scheduling p99<=1 s, KDF seal/reopen p99<=15 s and complete signer lifecycle p99<=60 s. Linux `/proc/self/status` supplies actual VmRSS/VmHWM measurements every 20 ms. The report retains per-cycle settled RSS/heap/goroutine/journal counts, latency counts/distributions, exact workload counts and scenario seeds.

| Current executed gate | Actual result |
| --- | --- |
| Bounded stress ordinary run | PASS, 21.73 s; RSS/HWM 568,549,376 bytes (542.2 MiB), final heap 4.1 MB, goroutines 2->2 |
| IPC/scheduling/KDF/lifecycle p99 | 501 us / 3975 us / 602951 us / 5912723 us; each below its declared budget |
| Fake ledger reconciliation | 1000 scheduling payments plus 2 production signer payments; exactly matched journals; 0 unresolved |
| Race instrumentation | PASS, 58.02 s; no race report; RSS/HWM 2,304,491,520 bytes against a separately declared 3 GiB instrumentation budget |
| 95-second soak smoke | Execution PASS, 96.57 s elapsed; 4 cycles, 8 encrypted restarts, 4 verified backups/discovery/history scans; RSS/HWM 800,788,480 bytes, goroutines 2->2; final journal/ledger/approval counts match |
| Required 24-hour campaign | **NOT_RUN**. The short run does not satisfy this gate. |

The race detector exceeded the ordinary 1 GiB RSS target: an earlier instrumented run measured 2,018,676,736 bytes. Tightening only the Go heap limit to 384 MiB did not constrain the race runtime's memory. That failed measurement remains in `reports/stress-race-reference-budget.json`. The ordinary resource target remains 1 GiB; the instrumented profile explicitly reports 3 GiB, while retaining the same production KDF and state/latency checks. A passing race-resource profile is not evidence of ordinary production RSS.

Replay from the repository root:

```sh
make test-stress
go test -race -count=1 ./tests/stress -run TestTP071BoundedStress -timeout=8m
go test -count=1 -v ./tests/stress -run TestTP072Soak -timeout=3m -args -soak=95s
make test-soak
python3 scripts/integration.py
```

Generated results are `reports/stress.json`, `stress-race.json`, `soak.json` and `integration.json`. A short soak records `executionStatus:PASS` with `status:NOT_RUN` and `campaign24hStatus:NOT_RUN`. The 24-hour runner uses a fixed 30-second scenario cadence, keeps the actual encrypted journal and fake history growing within production bounds, and performs final reconciliation of every signed identity. Its scenario mix includes RPC 503 outages, 429 rate limits, lock/unlock, fresh Engine instances on reopened files, receive/account discovery and growing history. It contains no automatic mainnet or public-testnet activity. A 24-hour passing result, production desktop/display stress and public-testnet activity remain separate unexecuted release gates.
