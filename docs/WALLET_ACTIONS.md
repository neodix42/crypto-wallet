# Wallet actions, Settings journal and overview balances

The owner now requires Electron for every dialog. Go generates the BIP39 root, verifies recorded words and restored phrases, encrypts local storage and validates/signs transaction effects. A dedicated sandboxed modal displays the generated phrase and accepts passwords/challenge inputs through a strictly bound private transport. The portfolio page has no secret-bearing methods. Electron is now inside the secret/approval trust boundary; this replaces the earlier native-only policy, not the remaining cryptographic or transaction safeguards. See [FRONTEND_SIGNER_TESTS.md](FRONTEND_SIGNER_TESTS.md).

Setup chooses an app password, records the phrase and confirms selected words. Acknowledgment alone cannot enable the wallet. Wrong words/cancellation preserve the encrypted pending root. A fresh copy can restore canonical wallets with the phrase, a specified local password and any original optional BIP39 passphrase. The local password does not derive wallet addresses. Encrypted portable backup creation, verification and file restoration remain removed.

Send opens its form even while setup is pending, offering **Finish recovery phrase setup** with preparation gated. Receive offers setup before its Go-validated QR/address dialog. Continuation is bound to the unchanged wallet/view/session; cancellation, incomplete confirmation, navigation, tab closing and lock never revive abandoned intent. Confirmed eligibility persists through password changes and account metadata refreshes. Prompt input/phrase DOM is cleared on submission, cancellation, hiding and unload; main destroys its temporary session on completion or interruption. JavaScript/runtime copies cannot be guaranteed erased.

The Local payment journal belongs in Settings with independent wallet/asset selectors. Inspecting another journal cannot change the active wallet, overwrite a Send draft or release a reservation. Cancellation stays limited to the original unsigned PREPARED operation; reconciliation retains the original identity. Stale responses/errors are discarded when context changes.

The overview shows native-currency balances using exact atomic strings: ETH for Base/Horizen, separate HYPE observations for HyperEVM/Core and each network's relevant native asset. Unknown/malformed/incomplete/failed observations never become invented zeroes. Queries disclose public addresses, run once per visible descriptor/provider configuration and use one background request at a time. Prompt actions pause new background work through metadata refresh; an already running request may delay the queued action up to its backend deadline. Lock stops scheduling. No automatic payment retry follows.

Receive/copy/save use the same authoritative public descriptor supplied by Go. The isolated dialog displays its network, full address and local QR. Copy deliberately copies only that exact address; Done does not copy. Copying before Cancel leaves that public clipboard action in place. File selection uses Electron's chooser in main; the prompt renderer cannot submit arbitrary filesystem paths. The normal portfolio preload cannot access the private prompt API.

## Current evidence checklist

| Check | Status | Evidence and scope |
| --- | --- | --- |
| Removed encrypted-file flow and public secret payloads | PASS in component tests | Exact portfolio allowlist/schemas; no backup/restore/reveal/export/promptReply methods; separate prompt-only bridge |
| Pending setup and confirmed eligibility | PASS in component tests | Both profiles, Go-owned word verification, explicit continuation, stale/cancel/lock/navigation guards, unchanged Send draft and retained confirmation |
| Private prompt transport/lifecycle | PASS in component tests | Current modal/frame/origin/ID binding, one-time delivery, malformed/replay/concurrency rejection, title replacement, locked-status/crash/navigation/timeout/EOF clearing and trusted activity |
| Settings journal and native row balances | PASS in component tests | Independent identities, exact precision, bounded queue, supersession, provider/descriptor invalidation and lock cleanup |
| Shipped frontend coverage | PASS | 95 tests, zero failures/skips; eight JS files 100% statements; main 98.4% branches, renderer 97.98%, six other files 100%. Prompt UI enforces critical 95%/90% floors; `reports/frontend-coverage.json` |
| Fresh-install phrase restoration | PASS in headless protocol integration | Six checks across three independent copies with separate HOME/config/processes; no original vault/keyring copied; 27 exact descriptors at indices 0/1/127 across nine mainnet networks match with same/different app passwords; locked restart and tested ordinary-IPC/log/file canaries; `reports/phrase-recovery.json` |
| Current installed Electron dialogs | PASS on final hardened artifact | 20 actual-X11 checks on the uninstrumented portable runtime, including default 12-word setup and fresh-profile restore, wrong-word/pending/resume, independently decoded QR/exact address copy, provider-observed balance, Settings journal, Send/form/tab behavior and restart/profile isolation. Five phrase lengths have prompt component coverage; installed sending and external physical/OS/audit gates remain NOT_RUN |
| Current Go coverage/adversarial checks | PASS within final source-bound scope | 23 measured package floors/149 stable hashes, including 22 shipped packages and retained unshipped GTK regression code. Explicit skips and remaining blocks in [COVERAGE.md](COVERAGE.md); final full race passes; 14 critical mutants killed and 14 bounded fuzz targets pass on unchanged targets before the separately tested memory helper |
| Former GTK implementation | PASS as retained legacy regression only | 14 short tests, optional five-minute case skipped; not linked into the production signer and not current interface evidence |
| Separate physical machines, full supported OS/Wayland/lifecycle matrix, public funded acceptance, 24-hour soak and independent review | NOT_RUN | Current same-host tests do not establish these gates |
| Automated mainnet payments | NOT_RUN | Never attempted; disposable/public fixtures only |

The final headless and installed drills use the same hardened Go binary manifest. [FRONTEND_SIGNER_TESTS.md](FRONTEND_SIGNER_TESTS.md) lists the complete binary/ASAR hashes. Earlier title/OCR driver failures remain separately recorded; final passing runs do not relabel them.

## Historical diagnosis and results

Older tests expected disabled Send/Receive buttons before encrypted-backup verification and opened Receive only after backup. They missed an unusable entry experience. The wallet-actions campaign fixed entry controls, moved the journal to Settings and added native balances; its [retained report](evidence/wallet-actions.json) passed 65 frontend tests and 27 packaged checks on its own artifact. The later native phrase campaign passed 68 frontend tests and 20 packaged checks. Those policies/artifacts do not certify the new Electron secret-bearing interface.

Historical exploratory OCR/file-selection/account-tab failures remain failures. Subsequent passes apply only to their exact recorded sources/artifacts. [WALLET_LAYOUT.md](WALLET_LAYOUT.md) retains the layout scope; [STATUS.md](STATUS.md) records outstanding production gates.
