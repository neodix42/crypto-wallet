package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"localwallet/internal/model"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ValidateEndpoint permits TLS or explicitly configured loopback development nodes.
func ValidateEndpoint(s string) error {
	u, e := url.Parse(s)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("invalid RPC endpoint")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("RPC endpoint requires TLS or a loopback node")
}

// StrictJSON rejects duplicate object members, unknown fields, trailing data, and
// excessive nesting. It is shared by the public HyperCore adapter.
func StrictJSON(b []byte, out any) error {
	if len(b) > 2<<20 {
		return errors.New("JSON size limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return errors.New("JSON nesting limit")
		}
		t, e := d.Token()
		if e != nil {
			return errors.New("invalid JSON")
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return errors.New("invalid JSON")
					}
					s, ok := key.(string)
					if !ok || seen[s] {
						return errors.New("duplicate JSON member")
					}
					seen[s] = true
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
				t, e = d.Token()
				if e != nil || t != json.Delim('}') {
					return errors.New("invalid JSON")
				}
			case '[':
				for d.More() {
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
				t, e = d.Token()
				if e != nil || t != json.Delim(']') {
					return errors.New("invalid JSON")
				}
			default:
				return errors.New("invalid JSON delimiter")
			}
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if e := d.Decode(out); e != nil {
		return errors.New("invalid JSON schema")
	}
	return nil
}

func (a *Adapter) rpc(ctx context.Context, n model.Network, method string, params any, out any) error {
	endpoint := n.RPC
	if x, ok := a.endpoints[n.ID]; ok {
		endpoint = x
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return err
	}
	body, _ := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", 1, method, params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("RPC request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	c := a.HTTPClient
	if c == nil {
		return errors.New("RPC client unavailable")
	}
	res, err := c.Do(req)
	if err != nil {
		return errors.New("RPC unavailable; submission may be ambiguous")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("RPC HTTP status %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil {
		return errors.New("RPC read failed")
	}
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data,omitempty"`
		} `json:"error"`
	}
	if err = StrictJSON(b, &env); err != nil {
		return err
	}
	if env.JSONRPC != "2.0" || env.ID != 1 {
		return errors.New("RPC response binding failed")
	}
	if env.Error != nil {
		return fmt.Errorf("RPC rejected request (%d)", env.Error.Code)
	}
	if len(env.Result) == 0 {
		return errors.New("RPC missing result")
	}
	// RPC objects have version-specific optional fields; duplicate/depth checks are
	// still enforced while individual methods validate all consumed fields.
	var checked any
	if err = StrictJSON(env.Result, &checked); err != nil {
		return err
	}
	if err = json.Unmarshal(env.Result, out); err != nil {
		return errors.New("invalid RPC result")
	}
	return nil
}
func (a *Adapter) checkChain(ctx context.Context, n model.Network) error {
	var s string
	if e := a.rpc(ctx, n, "eth_chainId", []any{}, &s); e != nil {
		return e
	}
	v, e := hexInteger(s)
	if e != nil || v.String() != n.ChainID {
		return errors.New("RPC chain identity mismatch")
	}
	return nil
}

func cleanHex(s string) bool { return strings.HasPrefix(s, "0x") && len(s)%2 == 0 }
