package walletsigner

import (
	"errors"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/signer"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
)

// Only these categories leave the secret-bearing process. Never forward an
// error's text, native input, selected pathname or filesystem/provider detail.
func requestErrorCode(err error) string {
	if code := ipc.PublicErrorCode(err); code != "" && code != "REQUEST_FAILED_STATE_UNKNOWN" {
		return code
	}
	for _, entry := range []struct {
		err  error
		code string
	}{
		{signer.ErrPasswordPolicy, "PASSWORD_POLICY"},
		{ton.ErrRateLimited, "TON_RPC_RATE_LIMITED"},
		{uiprompt.ErrInputTooLong, "INPUT_TOO_LONG"},
		{signer.ErrPasswordMismatch, "PASSWORD_MISMATCH"},
		{signer.ErrInvalidPhrase, "INVALID_MNEMONIC"},
		{uiprompt.ErrCancelled, "REQUEST_CANCELLED"},
		{signer.ErrAccountRecoveryInvalidRange, "ACCOUNT_RECOVERY_INVALID_RANGE"},
		{signer.ErrAccountRecoveryLimit, "ACCOUNT_RECOVERY_LIMIT"},
		{signer.ErrAccountRecoveryDerivation, "ACCOUNT_RECOVERY_DERIVATION"},
		{signer.ErrAccountRecoveryStorage, "ACCOUNT_RECOVERY_STORAGE"},
		{signer.ErrAccountRecoveryStale, "ACCOUNT_RECOVERY_STALE"},
		{signer.ErrAccountRecoveryCancelled, "REQUEST_CANCELLED"},
		{signer.ErrAccountRecoveryLocked, "WALLET_LOCKED"},
		{signer.ErrAddressBookInvalidInput, "ADDRESS_BOOK_INVALID_INPUT"},
		{signer.ErrAddressBookInvalidAddress, "ADDRESS_BOOK_INVALID_ADDRESS"},
		{signer.ErrAddressBookDuplicateName, "ADDRESS_BOOK_DUPLICATE_NAME"},
		{signer.ErrAddressBookDuplicateRecipient, "ADDRESS_BOOK_DUPLICATE_RECIPIENT"},
		{signer.ErrAddressBookLimit, "ADDRESS_BOOK_LIMIT"},
		{signer.ErrAddressBookStale, "ADDRESS_BOOK_STALE"},
		{signer.ErrAddressBookUnavailable, "ADDRESS_BOOK_UNAVAILABLE"},
		{signer.ErrAddressBookWriteFailed, "ADDRESS_BOOK_WRITE_FAILED"},
		{vault.ErrUnlock, "AUTHENTICATION_FAILED"},
		{vault.ErrFormat, "STORAGE_CORRUPT"},
		{vault.ErrFile, "STORAGE_UNAVAILABLE"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	if code := ipc.ClassifyErrorCode(err); code != "" && code != "REQUEST_FAILED_STATE_UNKNOWN" {
		return code
	}
	return "REQUEST_DENIED"
}
