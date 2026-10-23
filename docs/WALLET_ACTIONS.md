# Wallet actions, Settings journal and overview balances

The previous interface disabled Send and Receive whenever a mainnet backup was unverified. That state occurs after creation and after changes to recovery metadata, including adding an account. The Go security checks were intentional; disabling entry buttons made their recovery path difficult to find. The original frontend regression explicitly expected both buttons to be disabled, and the packaged workflow verified the QR only after creating a backup. Those tests covered the gate but missed the unusable entry experience.

Send now opens its form while preparation and signing still require a verified mainnet backup. Receive offers explicit backup guidance and the native encrypted-backup action before opening the authoritative Go QR window. Cancelling, failing, navigating away or locking must not resume the abandoned flow. Passwords, phrases and signing approvals stay in the native Go surface.

The Local payment journal belongs in Settings, with independent wallet and asset selectors. Inspecting another journal must not switch the active wallet, rewrite a send draft or release its reservation. Cancellation remains limited to the original unsigned PREPARED operation; reconciliation uses the original identity.

The account overview displays each wallet's native-currency balance. Base and Horizen show ETH, HyperEVM and HyperCore show their separate HYPE domains, and the other rows show their registered native assets. Amounts use decimal strings and registry precision. Missing, malformed, incomplete and failed observations remain unknown or unavailable. Observations are public-provider data, not consensus proofs. Queries expose public addresses to configured providers, run once per visible descriptor with one background query at a time, and stop scheduling on lock. Native actions pause background work until their metadata refresh finishes. A query already running can delay a queued native action up to its 30-second deadline; the public Send/Receive workflow opens immediately. Automatic retries are not implied.

## Evidence checklist

| Check | Status | Evidence |
|---|---|---|
| Original disabled-button cause and required backup policy | PASS | Existing renderer gate and Go Receive/Prepare/signing checks inspected; no user vault opened |
| Go mainnet backup, receive and recovery regressions | PASS | `go test ./internal/signer ./cmd/wallet-core -run 'Mainnet\|Backup\|Receive' -count=1`; `reports/wallet-actions-backend.log` |
| Clickable Send/Receive with safe backup continuation | PASS | 65 frontend tests and 27 installed-app checks; native backup success opens only the original unchanged wallet; cancellation, failure, navigation and lock covered |
| Journal in Settings with independent context and stale-reply guards | PASS | Frontend context/filter/cancellation/reconciliation tests and actual installed Settings panel; unrelated payment drafts stay unchanged |
| Exact native row balances, bounded scheduling, failures and lock invalidation | PASS | All nine mainnet registry shapes with exact public component fixtures; installed Ethereum row showed an actual ETH observation. Unknown/error/duplicate data, superseded refreshes, metadata/provider changes and lock covered |
| Source-stable frontend coverage and packaged runtime checks | PASS | All five shipped JS files: 100% statements; four critical files: 100% branches; renderer: 97.97% branches. 65 tests, zero skips; final portable runtime passed 27 checks and actual ASAR bytes match source |
| Full clean OS/Wayland/lifecycle matrix, public funded acceptance, 24-hour soak and independent review | NOT_RUN | Prior local/component evidence does not establish these release gates |
| Automated mainnet payments | NOT_RUN | Never attempted; only disposable generated and public fixtures used |

The [source/artifact-bound report](evidence/wallet-actions.json) records commands, hashes, current observations and explicit limits. `./wallet` is rebuilt and installed. The actual displayed native QR was decoded independently from screenshot pixels and checked against the public fixture; its Copy button returned the same exact address. Tested process logs and profile files contained no checked password/phrase canaries, Mesa loader warnings or ordinary IPC-handler stack traces. The private X11 and virtual Wayland startup probes also passed within their startup-only scope.

Two exploratory packaged attempts are retained rather than counted as successes. `reports/wallet-actions-packaged-ocr-attempt.{json,log}` stopped after five checks when OCR read “verify” as “verity”; strict complete-label recognition at the primary-button contrast threshold fixed the driver. `reports/wallet-actions-pre-metadata-attempt.{json,log}` stopped after twenty-four checks on the preceding 64-test artifact while waiting for Account 2. The final implementation holds account metadata refresh inside the native scheduling pause, and the driver waits for the actual tab within a bounded 45 seconds instead of assuming two seconds. A fresh run against the final 65-test artifact passed all twenty-seven checks. No production debug or automation bypass was added.

All 137 Go source/test/module hashes and both retained coverage-profile hashes still match the preceding Go/native coverage run; no Go source was added or removed. Its 22 package floors and native/race results retain their original report scope, while the targeted backend command above was rerun for this UI change. The native C/GTK code is unchanged.

The earlier table/tab campaign retains its own source and artifact binding in [WALLET_LAYOUT.md](WALLET_LAYOUT.md). Required release gates remain recorded in [STATUS.md](STATUS.md).
