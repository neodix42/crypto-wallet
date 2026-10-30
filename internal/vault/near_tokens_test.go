package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/decred/base58"
	"localwallet/internal/chains/near"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

func publicVaultNEP141Token(index int, network string) model.NEP141Token {
	contract := fmt.Sprintf("token%d.testnet", index)
	return model.NEP141Token{Network: network, Asset: model.Asset{ID: network + ":nep141:" + contract,
		Symbol: "PUBLIC", Decimals: 6, Contract: contract}, CodeHash: base58.Encode(bytes.Repeat([]byte{0x42}, 32))}
}

func nearTokenVaultFixture(t *testing.T) *Data {
	t.Helper()
	data := synthetic()
	seed, err := derivation.Seed(data.Entropy, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	account, err := near.New().Derive(seed, 0, "near-testnet")
	if err != nil {
		t.Fatal(err)
	}
	data.Accounts = append(data.Accounts, account)
	data.NEP141Tokens = []model.NEP141Token{publicVaultNEP141Token(0, "near-testnet"), publicVaultNEP141Token(1, "near-testnet")}
	data.NEP141Tokens[1].Asset.Decimals = 18
	return data
}

func TestNEP141PoliciesSurviveEncryptedFileRoundtripWithoutChangingRecoveryIdentity(t *testing.T) {
	data := nearTokenVaultFixture(t)
	defer data.Destroy()
	raw, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	ciphertext, err := Seal(raw, testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range data.NEP141Tokens {
		if bytes.Contains(ciphertext, []byte(token.Asset.Contract)) || bytes.Contains(ciphertext, []byte(token.CodeHash)) {
			t.Fatal("watch policy metadata appeared outside authenticated encryption")
		}
	}
	files, err := NewFiles(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err := files.Write("vault.lwv", ciphertext, false); err != nil {
		t.Fatal(err)
	}
	stored, err := files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(stored, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	restored, err := Decode(opened)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if !reflect.DeepEqual(restored.NEP141Tokens, data.NEP141Tokens) || VerifyRecovery(data, restored) != nil {
		t.Fatal("encrypted token metadata or canonical recovery identity changed")
	}
	restored.Destroy()
	if restored.NEP141Tokens != nil {
		t.Fatal("retired vault retained token watch metadata")
	}
}

func TestNEP141LegacyVaultWithoutOptionalPolicyFieldRemainsReadable(t *testing.T) {
	data := synthetic()
	defer data.Destroy()
	raw, err := data.Encode()
	if err != nil || bytes.Contains(raw, []byte(`"nep141Tokens"`)) {
		t.Fatal("empty watch policy became a mandatory vault field")
	}
	defer clear(raw)
	restored, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if restored.NEP141Tokens != nil || VerifyRecovery(data, restored) != nil {
		t.Fatal("legacy optional metadata changed existing keys or recovery semantics")
	}
}

func TestNEP141VaultRejectsDuplicateMalformedCrossProfileAndUnboundedPolicies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Data)
	}{
		{"duplicate", func(d *Data) { d.NEP141Tokens = append(d.NEP141Tokens, d.NEP141Tokens[0]) }},
		{"cross profile", func(d *Data) { d.NEP141Tokens[0] = publicVaultNEP141Token(0, "near-mainnet") }},
		{"foreign chain", func(d *Data) { d.NEP141Tokens[0] = publicVaultNEP141Token(0, "hyperevm-testnet") }},
		{"missing asset identity", func(d *Data) { d.NEP141Tokens[0].Asset.ID = "" }},
		{"foreign asset identity", func(d *Data) { d.NEP141Tokens[0].Asset.ID = "near-testnet:native" }},
		{"missing contract", func(d *Data) { d.NEP141Tokens[0].Asset.Contract = "" }},
		{"malformed hash", func(d *Data) { d.NEP141Tokens[0].CodeHash = "0x1234" }},
		{"unbounded precision", func(d *Data) { d.NEP141Tokens[0].Asset.Decimals = 78 }},
		{"untrusted symbol markup", func(d *Data) { d.NEP141Tokens[0].Asset.Symbol = "<script>" }},
		{"too many policies", func(d *Data) {
			d.NEP141Tokens = nil
			for i := 0; i < 33; i++ {
				d.NEP141Tokens = append(d.NEP141Tokens, publicVaultNEP141Token(i, "near-testnet"))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := nearTokenVaultFixture(t)
			defer data.Destroy()
			tc.mutate(data)
			if _, err := data.Encode(); err == nil {
				t.Fatal("invalid token policy entered the authenticated vault schema")
			}
		})
	}
	data := nearTokenVaultFixture(t)
	defer data.Destroy()
	data.NEP141Tokens = nil
	for i := 0; i < 32; i++ {
		data.NEP141Tokens = append(data.NEP141Tokens, publicVaultNEP141Token(i, "near-testnet"))
	}
	if _, err := data.Encode(); err != nil {
		t.Fatal("maximum bounded distinct policy set was rejected", err)
	}
}

func TestNEP141VaultDecoderRejectsAmbiguousNestedAuthorizationMetadata(t *testing.T) {
	data := nearTokenVaultFixture(t)
	defer data.Destroy()
	raw, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	for _, tc := range []struct{ before, after string }{
		{`"nep141Tokens":[`, `"nep141Tokens":[],"nep141Tokens":[`},
		{`"decimals":6`, `"decimals":null`},
		{`"decimals":6,`, ``},
		{`"decimals":6`, `"decimals":"6"`},
		{`"decimals":6`, `"decimals":-1`},
		{`"decimals":6`, `"decimals":6,"decimals":18`},
		{`"asset":{`, `"asset":{"unexpected":true,`},
		{`"asset":{`, `"asset":null,"asset":{`},
		{`"codeHash":`, `"unknown":true,"codeHash":`},
	} {
		altered := strings.Replace(string(raw), tc.before, tc.after, 1)
		if altered == string(raw) {
			t.Fatal("negative fixture did not change its intended metadata")
		}
		if restored, err := Decode([]byte(altered)); err == nil || restored != nil {
			t.Fatal("ambiguous/null/missing/foreign nested policy field was accepted")
		}
	}
}

func TestNEP141RecoveryVerificationIncludesEveryPolicyField(t *testing.T) {
	data := nearTokenVaultFixture(t)
	defer data.Destroy()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	for _, mutate := range []func(*Data){
		func(d *Data) { d.NEP141Tokens = nil },
		func(d *Data) { d.NEP141Tokens[0].Asset.Symbol = "OTHER" },
		func(d *Data) { d.NEP141Tokens[0].Asset.Decimals = 0 },
		func(d *Data) { d.NEP141Tokens[0].CodeHash = base58.Encode(bytes.Repeat([]byte{0x43}, 32)) },
		func(d *Data) { d.NEP141Tokens[0] = publicVaultNEP141Token(2, "near-testnet") },
		func(d *Data) { d.NEP141Tokens[0] = publicVaultNEP141Token(0, "near-mainnet") },
	} {
		restored, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		mutate(restored)
		if VerifyRecovery(data, restored) == nil {
			t.Fatal("changed token policy was ignored by recovery verification")
		}
		restored.Destroy()
	}
}

func TestNEP141VaultEnvironmentBindsMainnetTestnetAndLocalnetPolicies(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		for _, network := range []string{"near-mainnet", "near-testnet", "near-localnet"} {
			t.Run(environment+"/"+network, func(t *testing.T) {
				data := synthetic()
				defer data.Destroy()
				data.Environment = environment
				data.NEP141Tokens = []model.NEP141Token{publicVaultNEP141Token(0, network)}
				raw, err := data.Encode()
				valid := (network == "near-mainnet") == (environment == "mainnet")
				if !valid {
					if err == nil {
						t.Fatal("cross-environment token policy accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer clear(raw)
				encrypted, err := Seal(raw, testPassword, testParams)
				if err != nil {
					t.Fatal(err)
				}
				opened, err := Open(encrypted, testPassword)
				if err != nil {
					t.Fatal(err)
				}
				defer clear(opened)
				restored, err := Decode(opened)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Destroy()
				if !reflect.DeepEqual(restored.NEP141Tokens, data.NEP141Tokens) {
					t.Fatal("encrypted profile-specific token policy changed")
				}
			})
		}
	}
}
