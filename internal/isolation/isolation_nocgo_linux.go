//go:build !cgo || portable

// Package isolation provides memory protection without sandbox dependencies.
package isolation

import (
	"errors"

	"golang.org/x/sys/unix"
)

// ProtectMemory retains the same fail-closed dump protection in a portable
// executable without libseccomp. It installs no syscall or network filter.
func ProtectMemory() error {
	limit := unix.Rlimit{}
	if unix.Setrlimit(unix.RLIMIT_CORE, &limit) != nil || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return errors.New("wallet memory protection unavailable")
	}
	dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil || dumpable != 0 || unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
		return errors.New("wallet memory protection unavailable")
	}
	return nil
}

// Install never pretends that a memory-only build installed the legacy libseccomp
// filter. Only the explicit portable entry point uses ProtectMemory instead.
func Install() error { return errors.New("signer isolation unavailable") }
