// Package chainutil implements bounded transport and canonical public-input parsing.
package chainutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid chain data")

// Client never follows redirects, bounds response bytes and applies a finite timeout.
type Client struct{ HTTP *http.Client }

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func Endpoint(s string) error {
	u, e := url.Parse(s)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return ErrInvalid
	}
	if u.Scheme == "https" {
		return nil
	}
	h := u.Hostname()
	ip := net.ParseIP(h)
	if u.Scheme == "http" && (h == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("RPC requires HTTPS or an explicit loopback node")
}

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string {
	return "chain RPC rejected request: HTTP " + strconv.Itoa(e.Status)
}
func (c *Client) Do(ctx context.Context, method, endpoint, contentType string, body []byte, out any) error {
	if e := Endpoint(strings.Split(endpoint, "?")[0]); e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return ErrInvalid
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return errors.New("chain RPC unavailable")
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if e != nil || len(b) > 2<<20 {
		return ErrInvalid
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	if e = CheckJSON(b); e != nil {
		return e
	}
	if e = json.Unmarshal(b, out); e != nil {
		return ErrInvalid
	}
	return nil
}

// CheckJSON rejects duplicate members, excessive nesting, and trailing data before parsing typed JSON.
func CheckJSON(b []byte) error {
	if len(b) > 2<<20 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := check(d, 0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrInvalid
	}
	return nil
}
func check(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrInvalid
	}
	t, e := d.Token()
	if e != nil {
		return ErrInvalid
	}
	s, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch s {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return ErrInvalid
			}
			ks, ok := k.(string)
			if !ok || seen[ks] {
				return ErrInvalid
			}
			seen[ks] = true
			if e = check(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = check(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return ErrInvalid
	}
	_, e = d.Token()
	if e != nil {
		return ErrInvalid
	}
	return nil
}
func DecodeStrict(b []byte, out any) error {
	if e := CheckJSON(b); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return ErrInvalid
	}
	return nil
}
func Big(s string, bits int) (*big.Int, error) {
	if len(s) == 0 || len(s) > 80 || (len(s) > 1 && s[0] == '0') {
		return nil, ErrInvalid
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil, ErrInvalid
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.BitLen() > bits {
		return nil, ErrInvalid
	}
	return n, nil
}
func Uint(s string, bits int) (uint64, error) {
	n, e := Big(s, bits)
	if e != nil {
		return 0, e
	}
	return n.Uint64(), nil
}
