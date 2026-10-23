package evm

import "testing"

// Best-effort wiping is tested through aliases to the original scalar backing
// words. This makes no claim about copies made by libraries, runtime or OS.
func TestTP040MutableScalarErasure(t *testing.T) {
	key, e := deriveKey(syntheticSeed(), 127)
	if e != nil {
		t.Fatal(e)
	}
	words := key.D.Bits()
	if len(words) == 0 {
		t.Fatal("missing test scalar")
	}
	destroyKey(key)
	for _, word := range words {
		if word != 0 {
			t.Fatal("original scalar backing words were not cleared")
		}
	}
	if key.D.Sign() != 0 {
		t.Fatal("key scalar remains nonzero")
	}
	zeroInteger(nil)
	destroyKey(nil)
}
