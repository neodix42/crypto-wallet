// Package derivation implements the fixed BIP39 and hardened SLIP-0010 profiles.
// It must only be imported into secret-bearing executables.
package derivation

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	bip39 "github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/unicode/norm"
)

var ErrInput = errors.New("invalid recovery input")

const MaxIndex uint32 = 1000000

func Wipe(b []byte)                      { clear(b) }
func Generate(words int) ([]byte, error) { return generate(rand.Reader, words) }
func generate(r io.Reader, words int) ([]byte, error) {
	n := 0
	switch words {
	case 12:
		n = 16
	case 24:
		n = 32
	default:
		return nil, ErrInput
	}
	entropy := make([]byte, n)
	if _, e := io.ReadFull(r, entropy); e != nil {
		Wipe(entropy)
		return nil, errors.New("entropy source failed")
	}
	return entropy, nil
}

// Import enforces English BIP39, NFKD and single ASCII space separation.
// Whitespace other than ASCII separators is rejected rather than repaired.
func Import(phrase []byte) ([]byte, error) {
	if len(phrase) > 2048 {
		return nil, ErrInput
	}
	s := norm.NFKD.String(string(phrase))
	parts := strings.Split(s, " ")
	switch len(parts) {
	case 12, 15, 18, 21, 24:
	default:
		return nil, ErrInput
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, "\n\r\t") {
			return nil, ErrInput
		}
	}
	e, err := bip39.EntropyFromMnemonic(s)
	if err != nil {
		return nil, ErrInput
	}
	return e, nil
}
func Seed(entropy, passphrase []byte) ([]byte, error) {
	switch len(entropy) {
	case 16, 20, 24, 28, 32:
	default:
		return nil, ErrInput
	}
	if len(passphrase) > 4096 || !utf8.Valid(passphrase) {
		return nil, ErrInput
	}
	phrase, e := bip39.NewMnemonic(entropy)
	if e != nil {
		return nil, ErrInput
	}
	m := []byte(norm.NFKD.String(phrase))
	defer Wipe(m)
	salt := append([]byte("mnemonic"), norm.NFKD.Bytes(passphrase)...)
	defer Wipe(salt)
	return pbkdf2.Key(m, salt, 2048, 64, sha512.New), nil
}

// Ed25519 performs SLIP-0010 hardening internally; callers pass profile indices.
func Ed25519(seed []byte, path []uint32) (ed25519.PrivateKey, error) {
	if len(seed) != 64 || len(path) == 0 || len(path) > 8 {
		return nil, ErrInput
	}
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(seed)
	i := mac.Sum(nil)
	defer func() { Wipe(i) }()
	for _, p := range path {
		if p >= 0x80000000 {
			return nil, ErrInput
		}
		data := make([]byte, 37)
		copy(data[1:33], i[:32])
		binary.BigEndian.PutUint32(data[33:], p|0x80000000)
		mac = hmac.New(sha512.New, i[32:])
		mac.Write(data)
		next := mac.Sum(nil)
		Wipe(data)
		Wipe(i)
		i = next
	}
	return ed25519.NewKeyFromSeed(i[:32]), nil
}
