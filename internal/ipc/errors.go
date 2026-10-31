package ipc

import "errors"

// RequestError carries only an allowlisted public category. It never contains
// native input, a selected filename, an OS error or provider response text.
type RequestError struct{ code string }

func (e *RequestError) Error() string { return e.code }

func SafeErrorCode(code string) string {
	switch code {
	case "PASSWORD_POLICY", "INPUT_TOO_LONG", "PASSWORD_MISMATCH", "INVALID_MNEMONIC",
		"BACKUP_DESTINATION_UNSAFE", "BACKUP_EXISTS", "BACKUP_WRITE_FAILED", "BACKUP_VERIFY_FAILED",
		"RESTORE_SOURCE_UNSAFE", "REQUEST_CANCELLED",
		"ADDRESS_BOOK_INVALID_INPUT", "ADDRESS_BOOK_INVALID_ADDRESS", "ADDRESS_BOOK_DUPLICATE_NAME",
		"ADDRESS_BOOK_DUPLICATE_RECIPIENT", "ADDRESS_BOOK_LIMIT", "ADDRESS_BOOK_STALE",
		"ADDRESS_BOOK_UNAVAILABLE", "ADDRESS_BOOK_WRITE_FAILED":
		return code
	case "TON_RPC_RATE_LIMITED", "TON_DEPLOYMENT_UNAVAILABLE", "TON_DEPLOYMENT_RECORD_FAILED", "TON_DEPLOYMENT_INVALID_PLAN":
		return code
	default:
		return "REQUEST_FAILED_STATE_UNKNOWN"
	}
}

func NewRequestError(code string) error { return &RequestError{SafeErrorCode(code)} }

func PublicErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var failure *RequestError
	if errors.As(err, &failure) && failure != nil {
		return SafeErrorCode(failure.code)
	}
	return "REQUEST_FAILED_STATE_UNKNOWN"
}
