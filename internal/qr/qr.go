// Package qr parses only local bounded public QR images. V1 accepts plain
// addresses; payment URI schemes are rejected until separately implemented.
package qr

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/makiuchi-d/gozxing"
	decoder "github.com/makiuchi-d/gozxing/qrcode"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode"
)

func Decode(b []byte) (string, error) {
	if len(b) == 0 || len(b) > 5<<20 {
		return "", errors.New("QR image size rejected")
	}
	c, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || c.Width < 1 || c.Height < 1 || c.Width > 4096 || c.Height > 4096 || int64(c.Width)*int64(c.Height) > 16<<20 {
		return "", errors.New("QR dimensions rejected")
	}
	im, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return "", errors.New("QR image rejected")
	}
	bitmap, e := gozxing.NewBinaryBitmapFromImage(im)
	if e != nil {
		return "", errors.New("QR image rejected")
	}
	r, e := decoder.NewQRCodeReader().Decode(bitmap, nil)
	if e != nil {
		return "", errors.New("QR unreadable")
	}
	s := r.GetText()
	if len(s) == 0 || len(s) > 128 || strings.ContainsAny(s, "/?#@\\") || strings.Contains(s, "://") {
		return "", errors.New("unsupported QR payload")
	}
	if strings.Contains(s, ":") {
		// Only TON's raw workchain:hash address uses a colon in v1. No URI
		// scheme is accepted, even when it has no query parameters.
		parts := strings.Split(s, ":")
		if len(parts) != 2 || (parts[0] != "0" && parts[0] != "-1") || len(parts[1]) != 64 {
			return "", errors.New("unsupported QR payload")
		}
		if _, err := hex.DecodeString(parts[1]); err != nil {
			return "", errors.New("unsupported QR payload")
		}
	}
	for _, c := range s {
		if unicode.IsControl(c) || unicode.IsSpace(c) {
			return "", errors.New("unsupported QR payload")
		}
	}
	return s, nil
}
