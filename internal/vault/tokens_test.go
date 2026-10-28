package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

func publicVaultToken(index int, network string) model.ERC20Token {
	contract := common.HexToAddress(fmt.Sprintf("0x%040x", index+100)).Hex()
	return model.ERC20Token{Network: network, Asset: model.Asset{ID: network + ":erc20:" + contract,
		Symbol: "PUBLIC", Decimals: 6, Contract: contract}, CodeHash: "0x" + strings.Repeat("a", 64)}
}

func tokenVaultFixture(t *testing.T) *Data {
	t.Helper()
	data := synthetic()
	seed, err := derivation.Seed(data.Entropy, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	account, err := evm.New().Derive(seed, 0, "ethereum-sepolia")
	if err != nil {
		t.Fatal(err)
	}
	data.Accounts = append(data.Accounts, account)
	data.ERC20Tokens = []model.ERC20Token{publicVaultToken(0, "ethereum-sepolia"), publicVaultToken(1, "ethereum-sepolia")}
	data.ERC20Tokens[1].Asset.Decimals = 18
	return data
}

func TestERC20PoliciesSurviveEncryptedFileRoundtripWithoutChangingRecoveryIdentity(t *testing.T) {
	data := tokenVaultFixture(t)
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
	for _, token := range data.ERC20Tokens {
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
	if !reflect.DeepEqual(restored.ERC20Tokens, data.ERC20Tokens) || VerifyRecovery(data, restored) != nil {
		t.Fatal("encrypted token metadata or canonical recovery identity changed")
	}
	restored.Destroy()
	if restored.ERC20Tokens != nil {
		t.Fatal("retired vault retained token watch metadata")
	}
}

func TestERC20LegacyVaultWithoutOptionalPolicyFieldRemainsReadable(t *testing.T) {
	data := synthetic()
	defer data.Destroy()
	raw, err := data.Encode()
	if err != nil || bytes.Contains(raw, []byte(`"erc20Tokens"`)) {
		t.Fatal("empty watch policy became a mandatory vault field")
	}
	defer clear(raw)
	restored, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if restored.ERC20Tokens != nil || VerifyRecovery(data, restored) != nil {
		t.Fatal("legacy optional metadata changed existing keys or recovery semantics")
	}
}

func TestERC20VaultRejectsDuplicateMalformedCrossProfileAndUnboundedPolicies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Data)
	}{
		{"duplicate", func(d *Data) { d.ERC20Tokens = append(d.ERC20Tokens, d.ERC20Tokens[0]) }},
		{"cross profile", func(d *Data) { d.ERC20Tokens[0] = publicVaultToken(0, "ethereum-mainnet") }},
		{"foreign chain", func(d *Data) { d.ERC20Tokens[0] = publicVaultToken(0, "base-sepolia") }},
		{"missing asset identity", func(d *Data) { d.ERC20Tokens[0].Asset.ID = "" }},
		{"foreign asset identity", func(d *Data) { d.ERC20Tokens[0].Asset.ID = "ethereum-sepolia:native" }},
		{"missing contract", func(d *Data) { d.ERC20Tokens[0].Asset.Contract = "" }},
		{"malformed hash", func(d *Data) { d.ERC20Tokens[0].CodeHash = "0x1234" }},
		{"unbounded precision", func(d *Data) { d.ERC20Tokens[0].Asset.Decimals = 78 }},
		{"untrusted symbol markup", func(d *Data) { d.ERC20Tokens[0].Asset.Symbol = "<script>" }},
		{"too many policies", func(d *Data) {
			d.ERC20Tokens = nil
			for i := 0; i < 33; i++ {
				d.ERC20Tokens = append(d.ERC20Tokens, publicVaultToken(i, "ethereum-sepolia"))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tokenVaultFixture(t)
			defer data.Destroy()
			tc.mutate(data)
			if _, err := data.Encode(); err == nil {
				t.Fatal("invalid token policy entered the authenticated vault schema")
			}
		})
	}
	data := tokenVaultFixture(t)
	defer data.Destroy()
	data.ERC20Tokens = nil
	for i := 0; i < 32; i++ {
		data.ERC20Tokens = append(data.ERC20Tokens, publicVaultToken(i, "ethereum-sepolia"))
	}
	if _, err := data.Encode(); err != nil {
		t.Fatal("maximum bounded distinct policy set was rejected", err)
	}
}

func TestERC20VaultDecoderRejectsAmbiguousNestedAuthorizationMetadata(t *testing.T) {
	data := tokenVaultFixture(t)
	defer data.Destroy()
	raw, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	for _, tc := range []struct{ before, after string }{
		{`"erc20Tokens":[`, `"erc20Tokens":[],"erc20Tokens":[`},
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

func TestERC20RecoveryVerificationIncludesEveryPolicyField(t *testing.T) {
	data := tokenVaultFixture(t)
	defer data.Destroy()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	for _, mutate := range []func(*Data){
		func(d *Data) { d.ERC20Tokens = nil },
		func(d *Data) { d.ERC20Tokens[0].Asset.Symbol = "OTHER" },
		func(d *Data) { d.ERC20Tokens[0].Asset.Decimals = 0 },
		func(d *Data) { d.ERC20Tokens[0].CodeHash = "0x" + strings.Repeat("b", 64) },
		func(d *Data) { d.ERC20Tokens[0] = publicVaultToken(2, "ethereum-sepolia") },
		func(d *Data) { d.ERC20Tokens[0] = publicVaultToken(0, "ethereum-mainnet") },
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
