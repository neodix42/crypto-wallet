package main

import (
	"errors"
	"localwallet/internal/secureui"
	"localwallet/internal/signer"
)

// Only these categories leave the secret-bearing process. Never forward an
// error's text, native input, selected pathname or filesystem/provider detail.
func requestErrorCode(err error) string {
	for _, entry := range []struct {
		err  error
		code string
	}{
		{signer.ErrPasswordPolicy, "PASSWORD_POLICY"},
		{secureui.ErrInputTooLong, "INPUT_TOO_LONG"},
		{signer.ErrPasswordMismatch, "PASSWORD_MISMATCH"},
		{signer.ErrInvalidPhrase, "INVALID_MNEMONIC"},
		{signer.ErrBackupDestination, "BACKUP_DESTINATION_UNSAFE"},
		{signer.ErrBackupExists, "BACKUP_EXISTS"},
		{signer.ErrBackupWrite, "BACKUP_WRITE_FAILED"},
		{signer.ErrBackupVerification, "BACKUP_VERIFY_FAILED"},
		{signer.ErrRestoreSource, "RESTORE_SOURCE_UNSAFE"},
		{secureui.ErrCancelled, "REQUEST_CANCELLED"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return "REQUEST_DENIED"
}

var failureGuidance = map[string]string{
	"PASSWORD_POLICY":           "Use an app password of 12–4096 UTF-8 bytes. The password is entered only in this native window and is never displayed or sent to the main interface.",
	"INPUT_TOO_LONG":            "An input exceeds 4096 UTF-8 bytes. Shorten it in the native window.",
	"PASSWORD_MISMATCH":         "The two new password entries do not match. Enter the same app password in both native fields.",
	"INVALID_MNEMONIC":          "Enter a valid English BIP39 recovery phrase with 12, 15, 18, 21 or 24 words. Arbitrary text is not a recovery phrase. Its checksum must also be valid.",
	"BACKUP_DESTINATION_UNSAFE": "Choose an existing folder you own that other users cannot modify. The backup file will be encrypted and readable only by you. Shared writable folders and symbolic links are not accepted.",
	"BACKUP_EXISTS":             "Choose a new backup filename. Existing files are never overwritten. Your current wallet has not been replaced.",
	"BACKUP_WRITE_FAILED":       "The encrypted backup could not be saved durably. Check folder access and free disk space, then choose a new filename. Keep any file from the failed attempt until you verify a replacement.",
	"BACKUP_VERIFY_FAILED":      "The backup could not be reopened and verified. Do not rely on it for recovery. Keep your current wallet and create another backup in a safe folder.",
	"RESTORE_SOURCE_UNSAFE":     "The selected backup could not be read safely. Use a regular owner-only backup file in an existing folder you own. Shared writable folders and symbolic links are not accepted.",
}

func notifyFailure(ui signer.UI, err error) {
	message := failureGuidance[requestErrorCode(err)]
	if message == "" {
		return
	}
	if native, ok := ui.(interface{ Notice(string, string) }); ok {
		native.Notice("Wallet request failed", message)
	}
}
