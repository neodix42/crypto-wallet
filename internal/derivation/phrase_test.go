package derivation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	bip39 "github.com/tyler-smith/go-bip39"
)

// Compare the mutable encoder against the pinned independent BIP39 package for
// all five supported entropy lengths, including byte patterns crossing every
// dictionary/checksum boundary. Only public deterministic fixtures are used.
func TestNativePhraseMatchesBIP39AllLengths(t *testing.T) {
	for _, size := range []int{16, 20, 24, 28, 32} {
		for pattern := 0; pattern < 256; pattern++ {
			entropy := make([]byte, size)
			if pattern == 255 {
				for i := range entropy {
					entropy[i] = 255
				}
			} else if pattern != 0 {
				var counter [4]byte
				binary.BigEndian.PutUint32(counter[:], uint32(pattern))
				digest := sha256.Sum256(counter[:])
				copy(entropy, digest[:])
			}
			original := bytes.Clone(entropy)
			got, err := Phrase(entropy)
			if err != nil {
				t.Fatal("canonical phrase rejected")
			}
			want, err := bip39.NewMnemonic(entropy)
			if err != nil || !bytes.Equal(got, []byte(want)) {
				t.Fatalf("BIP39 encoder mismatch: size=%d fixture=%d", size, pattern)
			}
			decoded, err := Import(got)
			if err != nil || !bytes.Equal(decoded, entropy) {
				t.Fatal("canonical phrase import mismatch")
			}
			if !bytes.Equal(entropy, original) {
				t.Fatal("phrase encoder mutated entropy")
			}
			clear(got)
			clear(decoded)
			clear(entropy)
			clear(original)
		}
	}
	for _, size := range []int{0, 1, 15, 17, 19, 21, 23, 25, 27, 29, 31, 33, 4096} {
		if got, err := Phrase(make([]byte, size)); err != ErrInput || got != nil {
			t.Fatal("invalid entropy accepted")
		}
	}
}
