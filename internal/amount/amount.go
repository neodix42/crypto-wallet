// Package amount provides canonical bounded integer and decimal arithmetic.
package amount

import (
	"errors"
	"math/big"
	"strings"
)

var ErrInvalid = errors.New("invalid amount or precision")

func Atomic(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 78 || (len(s) > 1 && s[0] == '0') {
		return nil, ErrInvalid
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, ErrInvalid
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.BitLen() > 256 {
		return nil, ErrInvalid
	}
	return n, nil
}
func Parse(s string, decimals uint8) (string, error) {
	if len(s) == 0 || len(s) > 158 || decimals > 77 {
		return "", ErrInvalid
	}
	p := strings.Split(s, ".")
	if len(p) > 2 {
		return "", ErrInvalid
	}
	if _, e := Atomic(p[0]); e != nil {
		return "", e
	}
	fraction := ""
	if len(p) == 2 {
		fraction = p[1]
		if fraction == "" || len(fraction) > int(decimals) {
			return "", ErrInvalid
		}
	}
	for _, c := range fraction {
		if c < '0' || c > '9' {
			return "", ErrInvalid
		}
	}
	out := strings.TrimLeft(p[0]+fraction+strings.Repeat("0", int(decimals)-len(fraction)), "0")
	if out == "" {
		out = "0"
	}
	if _, e := Atomic(out); e != nil {
		return "", e
	}
	return out, nil
}
func Format(s string, d uint8) (string, error) {
	n, e := Atomic(s)
	if e != nil || d > 77 {
		return "", ErrInvalid
	}
	s = n.String()
	if d == 0 {
		return s, nil
	}
	if len(s) <= int(d) {
		s = strings.Repeat("0", int(d)-len(s)+1) + s
	}
	i := len(s) - int(d)
	return s[:i] + "." + s[i:], nil
}
func Max(balance, fee string) (string, error) {
	b, e := Atomic(balance)
	if e != nil {
		return "", e
	}
	f, e := Atomic(fee)
	if e != nil || b.Cmp(f) < 0 {
		return "", ErrInvalid
	}
	return new(big.Int).Sub(b, f).String(), nil
}
