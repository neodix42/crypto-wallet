package chainutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"localwallet/internal/ipc"
)

type privateFailureTransport func(*http.Request) (*http.Response, error)

func (f privateFailureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRPCTransportCauseRetainsTypedReasonWithoutLeakingItsText(t *testing.T) {
	const canary = "CANARY-private-api-key-url-name-context"
	for _, cause := range []error{&net.DNSError{Err: canary, Name: canary}, context.Canceled, context.DeadlineExceeded} {
		client := NewClient()
		client.HTTP.Transport = privateFailureTransport(func(*http.Request) (*http.Response, error) { return nil, cause })
		err := client.Do(context.Background(), "GET", "https://provider.invalid/private?apiKey="+canary, "", nil, nil)
		var typed *TransportError
		if err == nil || !errors.As(err, &typed) || !errors.Is(err, cause) || strings.Contains(err.Error(), canary) || err.Error() != "chain RPC unavailable" {
			t.Fatal("transport error leaked or lost typed cause", err)
		}
		expected := "NETWORK_UNAVAILABLE"
		if errors.Is(cause, context.Canceled) {
			expected = "REQUEST_CANCELLED"
		}
		if errors.Is(cause, context.DeadlineExceeded) {
			expected = "NETWORK_TIMEOUT"
		}
		if ipc.ClassifyErrorCode(err) != expected {
			t.Fatal("typed transport cause lost", ipc.ClassifyErrorCode(err), expected)
		}
	}
}
