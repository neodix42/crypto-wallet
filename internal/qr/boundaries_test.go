package qr

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestTP061ImageResourceAndTruncationBoundaries(t *testing.T) {
	for _, b := range [][]byte{nil, make([]byte, (5<<20)+1)} {
		if text, err := Decode(b); err == nil || text != "" {
			t.Fatal("empty or oversized image accepted")
		}
	}
	var encoded bytes.Buffer
	blank := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			blank.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	if err := png.Encode(&encoded, blank); err != nil {
		t.Fatal(err)
	}
	if text, err := Decode(encoded.Bytes()); err == nil || text != "" {
		t.Fatal("blank image invented a payment request")
	}
	// IHDR alone has valid bounded dimensions but no complete pixel stream.
	truncated := encoded.Bytes()[:33]
	if _, _, err := image.DecodeConfig(bytes.NewReader(truncated)); err != nil {
		t.Fatal("truncation fixture did not retain valid dimensions", err)
	}
	if text, err := Decode(truncated); err == nil || text != "" {
		t.Fatal("truncated pixel stream accepted after dimension validation")
	}
	encoded.Reset()
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	if text, err := Decode(encoded.Bytes()); err == nil || text != "" {
		t.Fatal("out-of-bounds dimensions accepted")
	}
}
