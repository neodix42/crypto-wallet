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
		"RESTORE_SOURCE_UNSAFE", "REQUEST_CANCELLED":
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
