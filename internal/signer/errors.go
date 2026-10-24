package signer

import "errors"

// These errors deliberately contain only fixed, public validation information.
// The IPC boundary may map their identities to an explicit public allowlist;
// arbitrary filesystem, cryptographic and native errors must stay private.
var (
	ErrPasswordPolicy   = errors.New("app password must contain 12–4096 UTF-8 bytes")
	ErrPasswordMismatch = errors.New("app password confirmation does not match")
	ErrInvalidPhrase    = errors.New("enter a valid English BIP39 recovery phrase with its checksum")
)
