// Independent fixture generator: direct calls to the pinned official Decred
// libraries. Test-only public deterministic seeds; no wallet production API.
package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"golang.org/x/crypto/pbkdf2"
)

type row struct {
	Network   string `json:"network"`
	Index     uint32 `json:"index"`
	Address   string `json:"address"`
	PublicKey string `json:"publicKey"`
	XPub      string `json:"xpub"`
}

func derive(seed []byte, index uint32, p *chaincfg.Params, coin uint32, standard bool) (string, string, string) {
	k, e := hdkeychain.NewMaster(seed, p)
	if e != nil {
		panic(e)
	}
	defer func() { k.Zero() }()
	var xpub string
	for i, n := range []uint32{44 + 0x80000000, coin + 0x80000000, index + 0x80000000, 0, 0} {
		var c *hdkeychain.ExtendedKey
		if standard {
			c, e = k.ChildBIP32Std(n)
		} else {
			c, e = k.Child(n)
		}
		if e != nil {
			panic(e)
		}
		k.Zero()
		k = c
		if i == 2 {
			xpub = k.Neuter().String()
		}
	}
	pub := k.SerializedPubKey()
	a, e := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(stdaddr.Hash160(pub), p)
	if e != nil {
		panic(e)
	}
	return a.String(), hex.EncodeToString(pub), xpub
}
func main() {
	seed := pbkdf2.Key([]byte("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"), []byte("mnemonic"), 2048, 64, sha512.New)
	rows := []row{}
	for _, n := range []struct {
		id   string
		p    *chaincfg.Params
		coin uint32
	}{{"decred-mainnet", chaincfg.MainNetParams(), 42}, {"decred-testnet3", chaincfg.TestNet3Params(), 1}, {"decred-simnet", chaincfg.SimNetParams(), 1}} {
		for _, index := range []uint32{0, 1, 17} {
			a, pub, x := derive(seed, index, n.p, n.coin, false)
			rows = append(rows, row{n.id, index, a, pub, x})
		}
	}
	var divergence map[string]string
	for i := 0; i < 10000; i++ {
		passphrase := fmt.Sprintf("public synthetic Decred leading-zero fixture %d", i)
		s := pbkdf2.Key([]byte("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"), []byte("mnemonic"+passphrase), 2048, 64, sha512.New)
		legacy, pub, _ := derive(s, 0, chaincfg.MainNetParams(), 42, false)
		standard, _, _ := derive(s, 0, chaincfg.MainNetParams(), 42, true)
		if legacy != standard {
			divergence = map[string]string{"seed": hex.EncodeToString(s), "passphrase": passphrase, "childAddress": legacy, "standardAddress": standard, "publicKey": pub, "path": "m/44'/42'/0'/0/0"}
			break
		}
	}
	if divergence == nil {
		panic("no genuine Child leading-zero divergence found")
	}
	out := map[string]any{"oracle": "official hdkeychain/v3 3.1.3, chaincfg/v3 3.3.0, txscript/v4 4.1.2; direct reference generator", "seed": hex.EncodeToString(seed), "accounts": rows, "leadingZero": divergence}
	b, _ := json.MarshalIndent(out, "", "  ")
	b = append(b, '\n')
	if len(os.Args) == 2 && os.Args[1] == "--generate" {
		if e := os.WriteFile("tests/oracles/decred/fixtures.json", b, 0644); e != nil {
			panic(e)
		}
		fmt.Println("generated nine Decred fixtures and genuine full-seed Child divergence")
		return
	}
	want, e := os.ReadFile("tests/oracles/decred/fixtures.json")
	if e != nil || string(want) != string(b) {
		panic("Decred independent fixture mismatch")
	}
	fmt.Println("PASS: nine Decred official-library fixtures and genuine Child/strict-BIP32 divergence")
}
