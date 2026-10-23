package signer

import "errors"

// These errors deliberately contain only fixed, public validation information.
// The IPC boundary may map their identities to an explicit public allowlist;
// arbitrary filesystem, cryptographic and native errors must stay private.
var (
	ErrPasswordPolicy     = errors.New("app password must contain 12–4096 UTF-8 bytes")
	ErrPasswordMismatch   = errors.New("app password confirmation does not match")
	ErrInvalidPhrase      = errors.New("enter a valid English BIP39 recovery phrase with its checksum")
	ErrBackupDestination  = errors.New("choose a writable folder owned by your user without group or other write access")
	ErrBackupExists       = errors.New("backup filename already exists; choose a new filename")
	ErrBackupWrite        = errors.New("backup could not be written; check space and permissions, then choose a new filename")
	ErrBackupVerification = errors.New("backup could not be verified; keep the wallet and choose a new backup filename")
	ErrRestoreSource      = errors.New("choose an owner-only encrypted backup in a safe folder owned by your user")
)
