package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
)

func importedTokenFixture() model.ERC20Token {
	contract := common.HexToAddress("0x1234567890123456789012345678901234567890").Hex()
	return model.ERC20Token{Network: "ethereum-sepolia", Asset: model.Asset{ID: "ethereum-sepolia:erc20:" + contract, Symbol: "TEST", Decimals: 6, Contract: contract}, CodeHash: crypto.Keccak256Hash([]byte{0x60, 0x01}).Hex()}
}

func tokenSymbolABI(symbol string) string {
	b := make([]byte, 96)
	b[31], b[63] = 32, byte(len(symbol))
	copy(b[64:], symbol)
	return "0x" + hex.EncodeToString(b)
}

func importedNode(t *testing.T) (*nodeFixture, model.Account) {
	t.Helper()
	f := fakeNode(t, "ethereum-sepolia")
	f.intercept = func(method string, args []json.RawMessage) (any, bool) {
		if method == "eth_getCode" {
			var address string
			_ = json.Unmarshal(args[0], &address)
			if address == importedTokenFixture().Asset.Contract {
				return "0x6001", true
			}
		}
		if method == "eth_call" {
			var call map[string]string
			_ = json.Unmarshal(args[0], &call)
			switch call["data"] {
			case callData("symbol()"):
				return tokenSymbolABI("TEST"), true
			case callData("decimals()"):
				return "0x" + hex.EncodeToString(word(big.NewInt(6))), true
			}
		}
		return nil, false
	}
	account, _ := f.a.Derive(syntheticSeed(), 0, "ethereum-sepolia")
	return f, account
}

func TestImportedTokenPolicyKeepsNetworkContractPrecisionAndLimits(t *testing.T) {
	base := importedTokenFixture()
	if ValidateTokenPolicy(base) != nil || !IsPinnedAsset("ethereum-mainnet", USDTAsset) || IsPinnedAsset(base.Network, base.Asset.ID) || IsPinnedAsset("foreign", "native") {
		t.Fatal("pinned/imported identity classification changed")
	}
	for _, mutate := range []func(*model.ERC20Token){
		func(t *model.ERC20Token) { t.Network = "base-mainnet" },
		func(t *model.ERC20Token) { t.Asset.ID = "TEST" },
		func(t *model.ERC20Token) { t.Asset.Contract = "0x" + strings.Repeat("0", 40) },
		func(t *model.ERC20Token) { t.Asset.Contract = "0xinvalid" },
		func(t *model.ERC20Token) { t.Asset.Decimals = 78 },
		func(t *model.ERC20Token) { t.Asset.Symbol = "" },
		func(t *model.ERC20Token) { t.Asset.Symbol = strings.Repeat("A", 25) },
		func(t *model.ERC20Token) { t.Asset.Symbol = "<script>" },
		func(t *model.ERC20Token) { t.CodeHash = "0x1234" },
		func(t *model.ERC20Token) { t.CodeHash = strings.ToUpper(t.CodeHash) },
		func(t *model.ERC20Token) {
			t.Network = "ethereum-mainnet"
			t.Asset.ID = USDTAsset
			t.Asset.Contract = USDTContract
		},
	} {
		bad := base
		mutate(&bad)
		if ValidateTokenPolicy(bad) == nil {
			t.Fatal("unsupported token policy accepted")
		}
	}
	a := New()
	policies := []model.ERC20Token{base}
	updated, err := a.WithTokens(policies)
	if err != nil {
		t.Fatal(err)
	}
	policies[0].Asset.Symbol = "MUTATED"
	n, _ := updated.network(base.Network)
	original, _ := a.network(base.Network)
	if len(n.Assets) != 2 || n.Assets[1] != base.Asset || len(original.Assets) != 1 {
		t.Fatal("token import mutated an in-flight adapter or aliased caller metadata")
	}
	for _, tokens := range [][]model.ERC20Token{{base, base}, make([]model.ERC20Token, 33), {{}}} {
		if _, err := a.WithTokens(tokens); err == nil {
			t.Fatal("duplicate, oversized or invalid token list accepted")
		}
	}
	a.endpoints["unknown"] = "https://example.invalid"
	if _, err := a.WithTokens(nil); err == nil {
		t.Fatal("invalid endpoint policy bypassed during adapter rebuild")
	}
}

func TestImportMetadataRejectsMissingOrAmbiguousABIAndNeverGuessesPrecision(t *testing.T) {
	f, account := importedNode(t)
	token, err := f.a.InspectToken(context.Background(), account, importedTokenFixture().Asset.Contract)
	if err != nil || token != importedTokenFixture() {
		t.Fatal("checked one-block metadata did not reproduce exact policy", err)
	}
	tracked, _ := f.a.WithTokens([]model.ERC20Token{token})
	if _, err := tracked.InspectToken(context.Background(), account, token.Asset.Contract); err == nil {
		t.Fatal("duplicate token import accepted")
	}
	for _, network := range []string{"base-sepolia", "ethereum-sepolia"} {
		wrong, _ := f.a.Derive(syntheticSeed(), 0, network)
		if network == "ethereum-sepolia" {
			wrong.Profile = "unapproved"
		}
		if _, err := f.a.InspectToken(context.Background(), wrong, token.Asset.Contract); err == nil {
			t.Fatal("unsupported network or unowned descriptor imported a token")
		}
	}
	for _, contract := range []string{"invalid", "0x" + strings.Repeat("0", 40)} {
		if _, err := f.a.InspectToken(context.Background(), account, contract); err == nil {
			t.Fatal("invalid token address bypassed import checks")
		}
	}
	for _, mode := range []string{"symbol-response-type", "symbol-invalid", "runtime-hex"} {
		f, account := importedNode(t)
		original := f.intercept
		f.intercept = func(method string, args []json.RawMessage) (any, bool) {
			if mode == "runtime-hex" && method == "eth_getCode" {
				return "0xzz", true
			}
			if method == "eth_call" {
				var call map[string]string
				_ = json.Unmarshal(args[0], &call)
				if call["data"] == callData("symbol()") {
					if mode == "symbol-response-type" {
						return map[string]string{"symbol": "untyped"}, true
					}
					if mode == "symbol-invalid" {
						return "0x", true
					}
				}
			}
			return original(method, args)
		}
		if _, err := f.a.InspectToken(context.Background(), account, token.Asset.Contract); err == nil {
			t.Fatal("invalid RPC metadata/runtime encoding imported a policy")
		}
	}
	for _, method := range []string{"eth_chainId", "eth_getCode", "eth_call"} {
		f, account := importedNode(t)
		f.rpcError = method
		if _, err := f.a.InspectToken(context.Background(), account, token.Asset.Contract); err == nil {
			t.Fatal("failed metadata request became an approved policy")
		}
	}
	for _, code := range []string{"0x", "0x0", "0xzz"} {
		f, account := importedNode(t)
		f.intercept = nil
		f.override["eth_getCode"] = code
		if _, err := f.a.InspectToken(context.Background(), account, token.Asset.Contract); err == nil {
			t.Fatal("missing/malformed contract code imported")
		}
	}
	for _, precision := range []any{nil, "0x", "0x01", "0x" + hex.EncodeToString(word(big.NewInt(78))), "0x" + strings.Repeat("f", 64)} {
		f, account := importedNode(t)
		original := f.intercept
		f.intercept = func(method string, args []json.RawMessage) (any, bool) {
			if method == "eth_call" {
				var call map[string]string
				_ = json.Unmarshal(args[0], &call)
				if call["data"] == callData("decimals()") {
					return precision, true
				}
			}
			return original(method, args)
		}
		if _, err := f.a.InspectToken(context.Background(), account, token.Asset.Contract); err == nil {
			t.Fatal("missing/malformed/unsupported precision imported")
		}
	}
	for _, value := range []string{"0x", "0xzz", tokenSymbolABI("<unsafe>"), tokenSymbolABI(""), "0x" + strings.Repeat("0", 320)} {
		if _, err := decodeTokenSymbol(value); err == nil {
			t.Fatal("ambiguous/unsafe symbol accepted")
		}
	}
	valid, _ := hex.DecodeString(tokenSymbolABI("TEST")[2:])
	for _, index := range []int{0, 31, 32, 63, 95} {
		bad := append([]byte(nil), valid...)
		bad[index] = 255
		if _, err := decodeTokenSymbol("0x" + hex.EncodeToString(bad)); err == nil {
			t.Fatal("noncanonical symbol ABI accepted")
		}
	}
}

func FuzzERC20TokenPolicy(f *testing.F) {
	f.Add(tokenSymbolABI("TEST"), "TEST", uint8(6), importedTokenFixture().Asset.Contract)
	f.Add("0x", "<script>", uint8(255), "0x0")
	f.Fuzz(func(t *testing.T, raw, symbol string, decimals uint8, contract string) {
		if len(raw) > 4096 || len(symbol) > 1024 || len(contract) > 256 {
			return
		}
		if decoded, err := decodeTokenSymbol(raw); err == nil && !tokenSymbol(decoded) {
			t.Fatal("ABI decoder accepted unsafe unbounded display metadata")
		}
		policy := importedTokenFixture()
		policy.Asset.Symbol, policy.Asset.Decimals, policy.Asset.Contract = symbol, decimals, contract
		policy.Asset.ID = policy.Network + ":erc20:" + contract
		if ValidateTokenPolicy(policy) == nil && (decimals > 77 || !tokenSymbol(symbol) || common.HexToAddress(contract).Hex() != contract) {
			t.Fatal("policy validator authorized an ambiguous identity")
		}
	})
}
