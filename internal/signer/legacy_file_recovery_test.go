// Historical file-recovery hardening exercises remain test-only. The product
// exposes phrase recovery and does not compile these removed file workflows.
package signer

import (
	"errors"
	"localwallet/internal/journal"
	"localwallet/internal/vault"
	"path/filepath"
)

var (
	ErrBackupDestination  = errors.New("choose a writable folder owned by your user without group or other write access")
	ErrBackupExists       = errors.New("backup filename already exists; choose a new filename")
	ErrBackupWrite        = errors.New("backup could not be written; check space and permissions, then choose a new filename")
	ErrBackupVerification = errors.New("backup could not be verified; keep the wallet and choose a new backup filename")
	ErrRestoreSource      = errors.New("choose an owner-only encrypted backup in a safe folder owned by your user")
)

func (e *Engine) Backup() error {
	if e.data == nil {
		return ErrDenied
	}
	path, err := e.UI.File("Save encrypted portable backup", true)
	if err != nil {
		return err
	}
	f, err := vault.NewBackupFiles(filepath.Dir(path), true)
	if err != nil {
		return ErrBackupDestination
	}
	defer f.Close()
	plain, err := e.data.Encode()
	if err != nil {
		return err
	}
	defer clear(plain)
	b, err := vault.Seal(plain, e.password, e.parameters)
	if err != nil {
		return err
	}
	if err = f.Write(filepath.Base(path), b); err != nil {
		if errors.Is(err, vault.ErrExists) {
			return ErrBackupExists
		}
		return ErrBackupWrite
	}
	r, err := f.Read(filepath.Base(path))
	if err != nil {
		return ErrBackupVerification
	}
	opened, err := vault.Open(r, e.password)
	if err != nil {
		return ErrBackupVerification
	}
	defer clear(opened)
	d, err := vault.Decode(opened)
	if err != nil {
		return ErrBackupVerification
	}
	defer d.Destroy()
	if err = vault.VerifyRecovery(e.data, d); err != nil {
		return ErrBackupVerification
	}
	if e.Environment != "" && d.Environment != e.Environment {
		d.Destroy()
		return ErrDenied
	}
	if err = e.validateAccounts(d); err != nil {
		return ErrBackupVerification
	}
	e.data.RecoveryConfirmed = true
	return e.persist()
}
func (e *Engine) Restore() error {
	if e.data != nil {
		return ErrDenied
	}
	if exists, state := e.walletPresence(); exists || state != "missing" {
		return ErrDenied
	}
	path, err := e.UI.File("Open encrypted backup — restores into an empty profile", false)
	if err != nil {
		return err
	}
	f, err := vault.NewBackupFiles(filepath.Dir(path), false)
	if err != nil {
		return ErrRestoreSource
	}
	defer f.Close()
	b, err := f.Read(filepath.Base(path))
	if err != nil {
		return ErrRestoreSource
	}
	in, err := e.UI.Input("Restore encrypted portable backup", "Password that encrypted this backup")
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != 1 {
		return ErrDenied
	}
	plain, err := vault.Open(b, in[0])
	if err != nil {
		return vault.ErrUnlock
	}
	defer clear(plain)
	d, err := vault.Decode(plain)
	if err != nil {
		return err
	}
	if e.Environment != "" && d.Environment != e.Environment {
		d.Destroy()
		return ErrDenied
	}
	if err = e.validateAccounts(d); err != nil {
		d.Destroy()
		return err
	}
	d.RecoveryConfirmed = true
	journal.Recover(d.Operations)
	e.initializing = true
	defer func() { e.initializing = false }()
	e.data = d
	e.password = append([]byte(nil), in[0]...)
	return e.persist()
}
