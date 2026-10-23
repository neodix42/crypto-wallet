package hypercore

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
)

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func zeroInteger(v *big.Int) {
	if v == nil {
		return
	}
	bits := v.Bits()
	for i := range bits {
		bits[i] = 0
	}
	v.SetInt64(0)
}
func destroyKey(k *ecdsa.PrivateKey) {
	if k != nil {
		zeroInteger(k.D)
	}
}

// BIP32 CKDpriv, with geth's reviewed secp256k1 implementation. Invalid children
// fail explicitly rather than silently relabeling an index in a recovery descriptor.
func deriveKey(seed []byte, index uint32) (*ecdsa.PrivateKey, error) {
	if len(seed) != 64 || index >= 1<<31 {
		return nil, errors.New("invalid seed length or account index")
	}
	m := hmac.New(sha512.New, []byte("Bitcoin seed"))
	m.Write(seed)
	state := m.Sum(nil)
	defer wipe(state)
	order := crypto.S256().Params().N
	scalar := new(big.Int).SetBytes(state[:32])
	defer zeroInteger(scalar)
	if scalar.Sign() == 0 || scalar.Cmp(order) >= 0 {
		return nil, errors.New("invalid BIP32 master key")
	}
	chain := append([]byte(nil), state[32:]...)
	defer wipe(chain)
	for _, child := range []uint32{44 | 1<<31, 60 | 1<<31, 0 | 1<<31, 0, index} {
		data := make([]byte, 37)
		if child >= 1<<31 {
			scalar.FillBytes(data[1:33])
		} else {
			raw := scalar.FillBytes(make([]byte, 32))
			key, e := crypto.ToECDSA(raw)
			wipe(raw)
			if e != nil {
				return nil, errors.New("BIP32 key failure")
			}
			copy(data, crypto.CompressPubkey(&key.PublicKey))
			destroyKey(key)
		}
		binary.BigEndian.PutUint32(data[33:], child)
		mac := hmac.New(sha512.New, chain)
		mac.Write(data)
		sum := mac.Sum(nil)
		wipe(data)
		left := new(big.Int).SetBytes(sum[:32])
		if left.Cmp(order) >= 0 {
			zeroInteger(left)
			wipe(sum)
			return nil, errors.New("invalid BIP32 child; account index not relabeled")
		}
		scalar.Add(scalar, left)
		scalar.Mod(scalar, order)
		zeroInteger(left)
		if scalar.Sign() == 0 {
			wipe(sum)
			return nil, errors.New("invalid BIP32 child; account index not relabeled")
		}
		copy(chain, sum[32:])
		wipe(sum)
	}
	raw := scalar.FillBytes(make([]byte, 32))
	defer wipe(raw)
	return crypto.ToECDSA(raw)
}
