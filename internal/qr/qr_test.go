package qr

import (
	"github.com/skip2/go-qrcode"
	"testing"
)

func TestTP061IndependentQRDecoder(t *testing.T) {
	for _, address := range []string{"0x9858EfFD232B4033E47d90003D41EC34EcaEda94", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123", "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ", "0:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		for _, size := range []int{256, 512, 1024} {
			png, e := qrcode.Encode(address, qrcode.Medium, size)
			if e != nil {
				t.Fatal(e)
			}
			got, e := Decode(png)
			if e != nil || got != address {
				t.Fatal("independent QR roundtrip", e)
			}
		}
	}
	for _, address := range []string{"https://evil.invalid", "near:account?amount=1", "hello\ncommand", "ethereum:0x123?chain=1", "ethereum:0x123", "near:account", "0:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", ""} {
		png, e := qrcode.Encode(address, qrcode.Medium, 512)
		if e == nil {
			if _, e = Decode(png); e == nil {
				t.Fatal("URI/injection accepted")
			}
		}
	}
	if _, e := Decode([]byte("bad")); e == nil {
		t.Fatal("image accepted")
	}
}
func FuzzTP012QR(f *testing.F) {
	f.Add([]byte("invalid PNG"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = Decode(b) })
}
