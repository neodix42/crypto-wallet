// Package vault is exclusively linked into the secret-bearing process.
package vault

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var ErrFormat = errors.New("unsupported or malformed encrypted vault")
var ErrUnlock = errors.New("unable to authenticate vault")

const HeaderSize = 72
const MaxSize = 16 << 20

type Parameters struct {
	Memory uint32
	Passes uint32
	Lanes  uint8
}

var Default = Parameters{256 * 1024, 3, 4}
var kdfLock sync.Mutex

func (p Parameters) valid() bool {
	return p.Memory >= 64*1024 && p.Memory <= 512*1024 && p.Memory%(1024) == 0 && p.Passes >= 3 && p.Passes <= 6 && p.Lanes == 4
}
func PasswordValid(p []byte) bool { return len(p) >= 12 && len(p) <= 4096 && utf8.Valid(p) }
func Seal(plaintext, password []byte, p Parameters) ([]byte, error) {
	return seal(rand.Reader, plaintext, password, p)
}
func seal(r io.Reader, plain, password []byte, p Parameters) ([]byte, error) {
	if !p.valid() || !PasswordValid(password) || len(plain) == 0 || len(plain) > MaxSize {
		return nil, ErrFormat
	}
	h := make([]byte, HeaderSize)
	copy(h, []byte("LWVLT001"))
	binary.BigEndian.PutUint16(h[8:10], 1)
	h[10] = 1
	h[11] = 1
	binary.BigEndian.PutUint32(h[12:16], p.Memory)
	binary.BigEndian.PutUint32(h[16:20], p.Passes)
	h[20] = p.Lanes
	if _, e := io.ReadFull(r, h[24:64]); e != nil {
		return nil, errors.New("vault randomness failed")
	}
	binary.BigEndian.PutUint64(h[64:72], uint64(len(plain)+chacha20poly1305.Overhead))
	kdfLock.Lock()
	key := argon2.IDKey(password, h[24:40], p.Passes, p.Memory, p.Lanes, 32)
	defer clear(key)
	kdfLock.Unlock()
	a, e := chacha20poly1305.NewX(key)
	if e != nil {
		return nil, ErrFormat
	}
	return a.Seal(h, h[40:64], plain, h), nil
}
func Open(encrypted, password []byte) ([]byte, error) {
	if len(encrypted) < HeaderSize+16 || len(encrypted) > HeaderSize+MaxSize+16 || len(password) > 4096 {
		return nil, ErrFormat
	}
	h := encrypted[:HeaderSize]
	p := Parameters{binary.BigEndian.Uint32(h[12:16]), binary.BigEndian.Uint32(h[16:20]), h[20]}
	if string(h[:8]) != "LWVLT001" || binary.BigEndian.Uint16(h[8:10]) != 1 || h[10] != 1 || h[11] != 1 || h[21] != 0 || h[22] != 0 || h[23] != 0 || !p.valid() || binary.BigEndian.Uint64(h[64:72]) != uint64(len(encrypted)-HeaderSize) {
		return nil, ErrFormat
	}
	kdfLock.Lock()
	key := argon2.IDKey(password, h[24:40], p.Passes, p.Memory, p.Lanes, 32)
	defer clear(key)
	kdfLock.Unlock()
	a, e := chacha20poly1305.NewX(key)
	if e != nil {
		return nil, ErrFormat
	}
	out, e := a.Open(nil, h[40:64], encrypted[HeaderSize:], h)
	if e != nil {
		return nil, ErrUnlock
	}
	return out, nil
}
