package derivation

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestTP020TP021IndependentAllLengthsAndPassphrases(t *testing.T) {
	raw, e := os.ReadFile("../../tests/oracles/bip39/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Vectors []struct{ ID, Entropy, Phrase, Passphrase, Seed string }
	}
	if json.Unmarshal(raw, &f) != nil || len(f.Vectors) != 25 {
		t.Fatal("oracle fixtures")
	}
	for _, v := range f.Vectors {
		entropy, e := Import([]byte(v.Phrase))
		want, _ := hex.DecodeString(v.Entropy)
		if e != nil || !bytes.Equal(entropy, want) {
			t.Fatalf("fixture %s entropy", v.ID)
		}
		seed, e := Seed(entropy, []byte(v.Passphrase))
		want, _ = hex.DecodeString(v.Seed)
		if e != nil || !bytes.Equal(seed, want) {
			t.Fatalf("fixture %s seed", v.ID)
		}
		clear(seed)
	}
}
