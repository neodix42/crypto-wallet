> Superseded interface policy: the current wallet uses Electron dialogs for all input, phrase recording, receive and approval. Go generation, encrypted persistence, word/password checks and signing remain. See [ELECTRON_INTERFACE.md](ELECTRON_INTERFACE.md) for the current trust boundary and tests; native GTK evidence below is historical.

# Native phrase setup and independent-installation recovery

This owner-authorized policy replaces encrypted portable-backup creation, verification and file restoration. After native app-password entry, Go generates a random English BIP39 phrase, encrypts the root durably, displays the phrase only in its native window, and asks the user to confirm recorded words. The phrase display is nonselectable and has no clipboard, save or QR action. Confirmation checks transcription; software cannot prove that a physical offline copy was stored safely.

Incorrect words or cancellation leave the same encrypted wallet pending. Finish recovery phrase setup requires native app-password reauthentication. Receiving, copying receive addresses, payment preparation and signing remain denied in Go until confirmation. Existing v1 vaults preserve their keys and metadata; the old encrypted backup flag does not grant this new authorization. Account addition, password rotation and ordinary metadata changes do not change the root or invalidate its recorded phrase. A confirmed current generation has no repeated phrase-display endpoint; an attacker rolling back the whole directory to an older pending generation is outside any anti-rollback guarantee.

Restore from recovery phrase accepts the recorded words through native input in a fresh profile. The app password encrypts that installation and does not derive blockchain keys. The same phrase and immutable profiles therefore recover the same canonical wallets with either the same or a different app password. An original optional BIP39 passphrase is additionally required when one was used. Additional canonical account indices are recovered explicitly through the existing bounded range control. A phrase does not contain named/rotated associations, Decred scan counters, RPC credentials or the local payment journal; recovery must check those separately.

## Evidence checklist

| Check | Status | Evidence and limits |
|---|---|---|
| Native phrase display, masked recorded-word challenge, cancellation/incorrect words, cleared widget text and unchanged clipboard | PASS | Focused real GTK 1024×768 proof plus all 14 mandatory native tests; optional five-minute typing test skipped in aggregate |
| Desktop methods, pending/confirmed states, phrase restoration entry and absence of Electron secret fields | PASS | 68 component tests, zero skips; all five JS files have 100% statements, renderer 97.98% branches |
| Durable pending root, legacy migration, final authorization gates and same/different-password canonical recovery | PASS | Current source-stable Go tests, callback/persistence regressions and all-profile signing/receive gates |
| Independent copied applications, empty HOME/config, generated phrase and all networks/indices restored | PASS | `make test-recovery`: six checks, three installations, all nine mainnet networks at indices0/1/127 (27 descriptors), same/different app passwords and restart |
| Installed portable app phrase setup, restore, restart and wallet interactions | PASS | 20 checks against the final installed portable X11 artifact; no debug port or sandbox bypass |
| Full Go/native coverage floors, race and critical recovery mutants | PASS | All 22 package floors; full Go race, bounded stress, 13 short fuzz campaigns, 12/12 semantic mutants killed; earlier backup-policy reports remain historical |
| Separate physical machines, full clean supported OS/Wayland matrix, 24-hour workload and independent external review | NOT_RUN | Isolated directories/processes on one host do not establish these gates |
| Automated mainnet payments | NOT_RUN | Never attempted; only public/disposable test secrets and read-only public observations |

`make test-recovery` creates independent installation directories containing copies of the shipped Go applications. Each has a new HOME/config directory and new processes. It creates a disposable random phrase in the first native window, keeps its test OCR and inputs in memory, and restores the other copy without copying wallet files or using a machine keyring. It compares all nine mainnet network wallets at indices 0, 1 and 127, restarts locked, and repeats recovery with a different app password. It checks tested password/phrase canaries in public IPC, process logs and persisted files. No phrase pixels or words are retained in screenshots, report files, clipboard, command arguments or environment variables. This is an independent-installation simulation, not a claim that separate physical hardware ran.

The [recovery guide](RECOVERY.md), [threat model](THREAT_MODEL.md) and [vault format](VAULT_FORMAT.md) describe the resulting behavior and limits. [STATUS.md](STATUS.md) retains the broader release gates.

The retained [summary](evidence/phrase-onboarding.json), [copied-app report](evidence/phrase-recovery.json), [installed report](evidence/packaged-recovery.json) and [coverage review](COVERAGE.md) bind these results to the delivered source and binary/UI hashes. During harness development, native OCR punctuation/paint handling and public-button targeting failed safely; the harness was corrected and final complete drills reran. Native secret pixels were never written as failure diagnostics.
