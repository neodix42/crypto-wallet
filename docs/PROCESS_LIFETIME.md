# Linux child process lifetime evidence

> Historical portable-recovery row: its eight checks used the former encrypted-file backup workflow and apply to that recorded artifact. The child-lifetime mechanism is separate from recovery policy. Current recovery uses native phrase recording/import; see [PHRASE_RECOVERY.md](PHRASE_RECOVERY.md) for new artifact-bound results.


Linux parent-death signals follow the OS thread that creates a child. A Go goroutine can move to another thread, and its original thread can end while the Go process remains alive. The [Go syscall documentation](https://pkg.go.dev/syscall@go1.27.3#SysProcAttr) and [Linux manual](https://man7.org/linux/man-pages/man2/PR_SET_PDEATHSIG.2const.html) describe this behavior.

The launcher now pins its thread before starting the public coordinator and keeps it pinned through `Exec`. The supervisor gives each signer a dedicated locked creator thread, retained until the signer is killed and reaped. Startup failures close the pipes; explicit lock and failure teardown wait for child reaping and owner completion. Parent-death `SIGKILL`, process groups, executable manifests and the existing network restrictions remain enabled. Public `--version` still returns without opening a profile or starting a child.

| Actual check | Result and scope |
|---|---|
| Different-thread creation/exec control | PASS: all three public `/bin/cat` controls died after exec; three same-thread controls survived |
| Production launcher boundary | PASS: 32 repeated launches preserve fixed inherited pipes and sandbox restrictions |
| Signer creator-thread retirement | PASS: the caller thread ends, the actual manifest-checked child remains alive and completes another framed request |
| Explicit signer stop/restart | PASS: eight generations release every owner and reap every child |
| Abrupt parent process death | PASS: an isolated subreaper requires actual child `SIGKILL`; the public peer deliberately waits on EOF, preventing pipe closure from passing this test |
| Rebuilt portable recovery | PASS: all eight generated/imported wallet, backup, restart, restore and canary steps; exact executable/UI hashes are recorded |

The reproducible commands are `./scripts/with-go.sh go test -race -count=1 ./cmd/wallet-launcher ./internal/supervisor` and `./scripts/with-toolchain.sh python3 scripts/packaged-recovery.py`. Compact results are retained in [launcher-thread-proof.json](evidence/launcher-thread-proof.json), [supervisor-lifecycle.json](evidence/supervisor-lifecycle.json) and [packaged-recovery.json](evidence/packaged-recovery.json).

An earlier packaged recovery attempt failed after five passing steps because its restart screen could not obtain wallet status. That **FAIL** is retained in [pdeath-pre-fix-recovery.json](evidence/pdeath-pre-fix-recovery.json). The isolated Linux defect is confirmed, but no thread trace was captured from that particular failed attempt, so its exact attribution remains unproven. The rebuilt result does not erase the earlier failure or establish the complete OS/Wayland/suspend matrix. Public network acceptance, the 24-hour soak and independent security review remain separate release gates.
