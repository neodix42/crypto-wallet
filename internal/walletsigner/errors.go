package walletsigner

import (
	"errors"
	"localwallet/internal/signer"
	"localwallet/internal/uiprompt"
)

// Only these categories leave the secret-bearing process. Never forward an
// error's text, native input, selected pathname or filesystem/provider detail.
func requestErrorCode(err error) string {
	for _, entry := range []struct {
		err  error
		code string
	}{
		{signer.ErrPasswordPolicy, "PASSWORD_POLICY"},
		{uiprompt.ErrInputTooLong, "INPUT_TOO_LONG"},
		{signer.ErrPasswordMismatch, "PASSWORD_MISMATCH"},
		{signer.ErrInvalidPhrase, "INVALID_MNEMONIC"},
		{uiprompt.ErrCancelled, "REQUEST_CANCELLED"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return "REQUEST_DENIED"
}

var failureGuidance = map[string]string{
	"PASSWORD_POLICY":   "Use an app password of 12–4096 UTF-8 bytes. The password protects this installation and is handled locally.",
	"INPUT_TOO_LONG":    "An input exceeds 4096 UTF-8 bytes. Shorten it in the input window.",
	"PASSWORD_MISMATCH": "The two new password entries do not match. Enter the same app password in both password fields.",
	"INVALID_MNEMONIC":  "Enter a valid English BIP39 recovery phrase with 12, 15, 18, 21 or 24 words. Arbitrary text is not a recovery phrase. Its checksum must also be valid.",
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
