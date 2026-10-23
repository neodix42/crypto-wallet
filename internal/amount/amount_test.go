package amount

import (
	"strings"
	"testing"
)

func TestTP052ExactAmounts(t *testing.T) {
	for _, c := range []struct {
		s    string
		d    uint8
		want string
	}{{"1", 18, "1000000000000000000"}, {"0.00000001", 8, "1"}, {"12345678901234567890.12345678", 8, "1234567890123456789012345678"}, {"0", 0, "0"}} {
		got, e := Parse(c.s, c.d)
		if e != nil || got != c.want {
			t.Fatalf("precision %s: %v", c.s, e)
		}
		if _, e = Format(got, c.d); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"", "01", "-1", "+1", "1e3", " 1", "1.", ".1", "1.001", "0x1", "NaN", "1..2", strings.Repeat("9", 159)} {
		if _, e := Parse(s, 2); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"00", "-1", strings.Repeat("9", 78), strings.Repeat("1", 79)} {
		if _, e := Atomic(s); e == nil {
			t.Fatal("accepted invalid atomic")
		}
	}
	if s, e := Max("10", "3"); e != nil || s != "7" {
		t.Fatal(s, e)
	}
	for _, c := range [][2]string{{"1", "2"}, {"x", "1"}, {"1", "x"}} {
		if _, e := Max(c[0], c[1]); e == nil {
			t.Fatal("underflow accepted")
		}
	}
	if _, e := Parse("1", 78); e == nil {
		t.Fatal("excess decimals")
	}
	if _, e := Format("x", 18); e == nil {
		t.Fatal("bad format")
	}
	if _, e := Format("1", 78); e == nil {
		t.Fatal("precision")
	}
	if got, _ := Format("1", 8); got != "0.00000001" {
		t.Fatal(got)
	}
	if got, _ := Format("1", 0); got != "1" {
		t.Fatal(got)
	}
}
func FuzzTP012Amount(f *testing.F) {
	for _, s := range []string{"0", "1.0001", "-1", "1e10", "0x1"} {
		f.Add(s, uint8(18))
	}
	f.Fuzz(func(t *testing.T, s string, d uint8) {
		a, e := Parse(s, d)
		if e == nil {
			if _, e := Atomic(a); e != nil {
				t.Fatal(e)
			}
		}
	})
}
