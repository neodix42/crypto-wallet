package chainutil

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTP012CanonicalBoundedParsers(t *testing.T) {
	for _, s := range []string{"", "-1", "+1", "01", "1.0", "1e2", strings.Repeat("9", 81), "18446744073709551616"} {
		if _, e := Uint(s, 64); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"0", "1", "18446744073709551615"} {
		if _, e := Uint(s, 64); e != nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `[] []`, `[`, `{"a":1}garbage`, strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34), strings.Repeat(" ", 2<<20+1)} {
		if CheckJSON([]byte(s)) == nil {
			t.Fatal("accepted malformed input")
		}
	}
	for _, s := range []string{`{"a":1}`, `[]`, `null`, `[true,"x",1]`} {
		if CheckJSON([]byte(s)) != nil {
			t.Fatal(s)
		}
	}
	var q struct {
		A int `json:"a"`
	}
	if DecodeStrict([]byte(`{"unknown":1}`), &q) == nil {
		t.Fatal("unknown member")
	}
	if DecodeStrict([]byte(`{"a":1}`), &q) != nil || q.A != 1 {
		t.Fatal("strict decode")
	}
}
func TestTP041BoundedTransport(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	for _, mode := range []string{"ok", "redirect", "error", "oversized", "duplicate", "badJSON"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "ok":
					_ = json.NewEncoder(w).Encode(map[string]int{"n": 1})
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				case "error":
					w.WriteHeader(500)
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", 2<<20+1)))
				case "duplicate":
					_, _ = w.Write([]byte(`{"n":1,"n":2}`))
				case "badJSON":
					_, _ = w.Write([]byte(`bad`))
				}
			}))
			defer srv.Close()
			var out map[string]int
			e := NewClient().Do(context.Background(), "GET", srv.URL, "", nil, &out)
			if mode == "ok" {
				if e != nil || out["n"] != 1 {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("invalid response accepted")
			}
			if mode == "error" {
				var he *HTTPError
				if !errors.As(e, &he) || he.Status != 500 {
					t.Fatal(e)
				}
			}
		})
	}
	for _, s := range []string{"http://remote.example", "https://user:pass@example.com", "file:///etc/passwd", "http://127.0.0.1#frag", "http://localhost?q=x"} {
		if Endpoint(s) == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"https://example.org", "http://127.0.0.1:8080", "http://[::1]:3030", "http://localhost"} {
		if Endpoint(s) != nil {
			t.Fatal(s)
		}
	}
}
func TestTP071MalformedStress(t *testing.T) {
	for i := 0; i < 10000; i++ {
		input := []byte(strings.Repeat("[", i%35) + `{"a":1,"a":2}` + strings.Repeat("]", i%35))
		if CheckJSON(input) == nil {
			t.Fatal("duplicate accepted")
		}
		_, _ = Uint(strings.Repeat("9", i%82), 64)
	}
}
func FuzzJSON(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":1,"a":2}`, `[]`, "null", "{"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 2<<20 {
			return
		}
		_ = CheckJSON([]byte(s))
	})
}
func TestTP012TransportErrorPaths(t *testing.T) {
	c := NewClient()
	if c.Do(context.Background(), "GET", "http://remote.invalid", "", nil, nil) == nil {
		t.Fatal("unsafe endpoint")
	}
	if c.Do(context.Background(), "bad\nmethod", "http://127.0.0.1:1", "", nil, nil) == nil {
		t.Fatal("invalid method")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Do(ctx, "GET", "http://127.0.0.1:1", "", nil, nil) == nil {
		t.Fatal("cancel")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer srv.Close()
	if e := c.Do(context.Background(), "POST", srv.URL, "application/json", []byte(`{}`), nil); e != nil {
		t.Fatal(e)
	}
	var q map[string]int
	if c.Do(context.Background(), "GET", srv.URL, "", nil, &q) == nil {
		t.Fatal("wrong response type")
	}
	var v struct{}
	if DecodeStrict([]byte(`{`), &v) == nil {
		t.Fatal("invalid strict JSON")
	}
	for _, s := range []string{`{"a"`, `}`, `{"a":1,`, `[1}`, `{"a":1]`} {
		if CheckJSON([]byte(s)) == nil {
			t.Fatal("malformed object")
		}
	}
	if (&HTTPError{Status: 500}).Error() != "chain RPC rejected request: HTTP 500" {
		t.Fatal("sanitized error")
	}
}
