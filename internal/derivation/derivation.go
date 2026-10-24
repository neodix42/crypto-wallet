// Package derivation implements the fixed BIP39 and hardened SLIP-0010 profiles.
// It must only be imported into secret-bearing executables.
package derivation

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	bip39 "github.com/tyler-smith/go-bip39"
	"github.com/tyler-smith/go-bip39/wordlists"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/unicode/norm"
)

var ErrInput = errors.New("invalid recovery input")

// Phrase is used only by the native Go recovery-setup window. Callers must wipe
// the returned mutable buffer after the window closes and never send it to IPC.
func Phrase(entropy []byte) ([]byte, error) {
	switch len(entropy) {
	case 16, 20, 24, 28, 32:
	default:
		return nil, ErrInput
	}
	// BIP39 appends ENT/32 checksum bits and splits the bitstream into
	// 11-bit indices. Only public dictionary words become strings; the
	// secret sequence is assembled directly in an owned mutable buffer.
	checksum := sha256.Sum256(entropy)
	defer clear(checksum[:])
	entropyBits := len(entropy) * 8
	totalBits := entropyBits + len(entropy)/4
	phrase := make([]byte, 0, totalBits/11*9-1)
	for offset := 0; offset < totalBits; offset += 11 {
		index := 0
		for bit := offset; bit < offset+11; bit++ {
			var value byte
			if bit < entropyBits {
				value = (entropy[bit/8] >> (7 - bit%8)) & 1
			} else {
				position := bit - entropyBits
				value = (checksum[position/8] >> (7 - position%8)) & 1
			}
			index = index<<1 | int(value)
		}
		if offset != 0 {
			phrase = append(phrase, ' ')
		}
		phrase = append(phrase, wordlists.English[index]...)
	}
	return phrase, nil
}

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
	m, e := Phrase(entropy)
	if e != nil {
		return nil, ErrInput
	}
	defer Wipe(m)
	// Append owns the normalized salt and avoids an additional immutable
	// phrase/passphrase string or a temporary normalized secret buffer.
	salt := norm.NFKD.Append([]byte("mnemonic"), passphrase...)
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
