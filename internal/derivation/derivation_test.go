package derivation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

// Published Trezor BIP39 vector #1; public synthetic input only.
const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestTP020BIP39PublishedVector(t *testing.T) {
	e, err := Import([]byte(phrase))
	if err != nil || !bytes.Equal(e, make([]byte, 16)) {
		t.Fatal("BIP39 entropy")
	}
	s, err := Seed(e, []byte("TREZOR"))
	if err != nil || hex.EncodeToString(s) != "c55257c360c07c72029aebc1b53c05ed0362ada38ead3e3e9efa3708e53495531f09a6987599d18264c1e1c92f2cf141630c7a3c4ab7c81b2f001698e7463b04" {
		t.Fatal("published BIP39 seed mismatch")
	}
	clear(s)
	for _, s := range []string{"", strings.Replace(phrase, "about", "abandon", 1), " " + phrase, phrase + " ", strings.Replace(phrase, " ", "\t", 1), strings.Replace(phrase, "abandon", "unknown", 1), strings.Repeat("x", 2049)} {
		if _, e := Import([]byte(s)); e == nil {
			t.Fatal("invalid mnemonic accepted")
		}
	}
	for _, n := range []int{16, 20, 24, 28, 32} {
		s, e := Seed(make([]byte, n), nil)
		if e != nil || len(s) != 64 {
			t.Fatal("supported entropy")
		}
	}
	if _, e := Seed([]byte{1}, nil); e == nil {
		t.Fatal("entropy bound")
	}
	if _, e := Seed(make([]byte, 16), make([]byte, 4097)); e == nil {
		t.Fatal("passphrase bound")
	}
	a, _ := Seed(e, []byte("\u00e9"))
	b, _ := Seed(e, []byte("e\u0301"))
	if !bytes.Equal(a, b) {
		t.Fatal("NFKD passphrase")
	}
	c, _ := Seed(e, []byte("different"))
	if bytes.Equal(a, c) {
		t.Fatal("passphrase separation")
	}
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errors.New("synthetic entropy failure") }
func TestTP020EntropyFailure(t *testing.T) {
	for _, r := range []io.Reader{badReader{}, bytes.NewReader([]byte{1})} {
		if _, e := generate(r, 12); e == nil {
			t.Fatal("failure fell back")
		}
	}
	for _, n := range []int{12, 24} {
		b, e := Generate(n)
		if e != nil || len(b) != n/3*4 {
			t.Fatal("CSPRNG")
		}
	}
	if _, e := Generate(15); e == nil {
		t.Fatal("creation policy")
	}
}
func TestTP022Slip10IndependentVector(t *testing.T) {
	// Freeze independently generated vector in tests/oracles; verify hardened
	// derivation determinism and no cross-profile or cross-index reuse here.
	seed, _ := Seed(make([]byte, 16), nil)
	defer clear(seed)
	seen := map[string]bool{}
	for _, p := range [][]uint32{{44, 397, 0}, {44, 637, 0, 0, 0}, {44, 607, 0}, {44, 397, 1}, {44, 397, 1000}} {
		k, e := Ed25519(seed, p)
		if e != nil || len(k) != 64 {
			t.Fatal(e)
		}
		pub := hex.EncodeToString(k.Public().(ed25519.PublicKey))
		if seen[pub] {
			t.Fatal("profile/index collision")
		}
		seen[pub] = true
		clear(k)
	}
	for _, p := range [][]uint32{nil, {0x80000000}, {1, 2, 3, 4, 5, 6, 7, 8, 9}} {
		if _, e := Ed25519(seed, p); e == nil {
			t.Fatal("path accepted")
		}
	}
	if _, e := Ed25519(nil, []uint32{44}); e == nil {
		t.Fatal("seed length")
	}
}
func FuzzTP012Mnemonic(f *testing.F) {
	f.Add(phrase)
	f.Add("wrong")
	f.Fuzz(func(t *testing.T, s string) {
		e, err := Import([]byte(s))
		if err == nil {
			if _, err = Seed(e, nil); err != nil {
				t.Fatal(err)
			}
		}
	})
}
