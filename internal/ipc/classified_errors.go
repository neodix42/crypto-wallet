package ipc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"reflect"
)

// ClassifyErrorCode recognizes structural error types, never their messages.
// PublicErrorCode remains the stricter explicitly authored-category decoder.
// Error context, paths, network addresses, credentials and bodies stay private.
func ClassifyErrorCode(err error) string {
	if err == nil {
		return ""
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return "REQUEST_FAILED_STATE_UNKNOWN"
		}
	}
	if code := PublicErrorCode(err); code != "REQUEST_FAILED_STATE_UNKNOWN" {
		return code
	}
	if errors.Is(err, context.Canceled) {
		return "REQUEST_CANCELLED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "NETWORK_TIMEOUT"
	}
	var file *os.PathError
	var link *os.LinkError
	if errors.As(err, &file) && file != nil || errors.As(err, &link) && link != nil {
		if errors.Is(err, os.ErrPermission) {
			return "STORAGE_PERMISSION_DENIED"
		}
		return "STORAGE_UNAVAILABLE"
	}
	if errors.Is(err, os.ErrPermission) {
		return "ACCESS_DENIED"
	}
	var tlsFailure *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certificate x509.CertificateInvalidError
	if errors.As(err, &tlsFailure) && tlsFailure != nil || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &certificate) {
		return "NETWORK_TLS_FAILED"
	}
	var failure net.Error
	if errors.As(err, &failure) && failure != nil {
		if failure.Timeout() {
			return "NETWORK_TIMEOUT"
		}
		return "NETWORK_UNAVAILABLE"
	}
	return "REQUEST_FAILED_STATE_UNKNOWN"
}

// HTTPStatusErrorCode is used only with a trusted transport's numeric status,
// not a parsed provider message. No failed request is automatically retried.
func HTTPStatusErrorCode(status int) string {
	switch {
	case status == 401:
		return "PROVIDER_AUTHENTICATION_FAILED"
	case status == 403:
		return "PROVIDER_ACCESS_DENIED"
	case status == 408 || status == 504:
		return "NETWORK_TIMEOUT"
	case status == 429:
		return "PROVIDER_RATE_LIMITED"
	case status >= 500 && status <= 599:
		return "PROVIDER_UNAVAILABLE"
	case status >= 300 && status <= 399:
		return "PROVIDER_REDIRECT_DENIED"
	case status >= 400 && status <= 499:
		return "PROVIDER_REQUEST_REJECTED"
	default:
		return "REQUEST_FAILED_STATE_UNKNOWN"
	}
}
