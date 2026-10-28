package evm

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
	"math/big"
	"strings"
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

func (a *Adapter) Derive(seed []byte, index uint32, net string) (model.Account, error) {
	if _, e := network(net); e != nil {
		return model.Account{}, e
	}
	key, e := deriveKey(seed, index)
	if e != nil {
		return model.Account{}, e
	}
	defer destroyKey(key)
	return model.Account{ID: fmt.Sprintf("evm:%d:%s", index, net), Family: "evm", Profile: Profile, Index: index, Network: net, Address: crypto.PubkeyToAddress(key.PublicKey).Hex(), PublicKey: "0x" + hex.EncodeToString(crypto.FromECDSAPub(&key.PublicKey))}, nil
}

func (a *Adapter) ValidateAddress(net, s string) error {
	if _, e := network(net); e != nil {
		return e
	}
	if len(s) != 42 || !strings.HasPrefix(s, "0x") || !common.IsHexAddress(s) {
		return errors.New("invalid EVM address")
	}
	body := s[2:]
	if body != strings.ToLower(body) && body != strings.ToUpper(body) && common.HexToAddress(s).Hex() != s {
		return errors.New("invalid EIP-55 checksum")
	}
	if common.HexToAddress(s) == (common.Address{}) {
		return errors.New("zero address is not a payment recipient")
	}
	return nil
}

func (a *Adapter) validateAccount(ac model.Account) (model.Network, error) {
	n, e := a.network(ac.Network)
	if e != nil {
		return n, e
	}
	if ac.Profile != Profile || ac.Family != "evm" || ac.Index >= 1<<31 || ac.ID != fmt.Sprintf("evm:%d:%s", ac.Index, ac.Network) || len(ac.Parameters) != 0 {
		return n, errors.New("invalid EVM recovery descriptor")
	}
	if e = a.ValidateAddress(ac.Network, ac.Address); e != nil {
		return n, e
	}
	pub, e := hex.DecodeString(strings.TrimPrefix(ac.PublicKey, "0x"))
	if e != nil || len(pub) != 65 || !strings.HasPrefix(ac.PublicKey, "0x") {
		return n, errors.New("invalid EVM public key")
	}
	key, e := crypto.UnmarshalPubkey(pub)
	if e != nil || crypto.PubkeyToAddress(*key).Hex() != ac.Address {
		return n, errors.New("account key/address mismatch")
	}
	return n, nil
}
