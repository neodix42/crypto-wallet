package evm

// USDT-only tests use public deterministic fixtures, the retained deployed
// bytecode and an independent ethers/Ganache oracle. No test
// connects to a public RPC or Bybit, reads a user vault, or spends real funds.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/amount"
	"localwallet/internal/model"
)

type usdtOracleRow struct {
	Index                uint32 `json:"index"`
	Address              string `json:"address"`
	PublicKey            string `json:"publicKey"`
	ChainID              string `json:"chainId"`
	Contract             string `json:"contract"`
	Recipient            string `json:"recipient"`
	Decimals             uint8  `json:"decimals"`
	AmountAtomic         string `json:"amountAtomic"`
	AmountDecimal        string `json:"amountDecimal"`
	Data                 string `json:"data"`
	Unsigned             string `json:"unsigned"`
	Signed               string `json:"signed"`
	Hash                 string `json:"hash"`
	InitialAtomic        string `json:"initialAtomic"`
	SourceAfterAtomic    string `json:"sourceAfterAtomic"`
	RecipientAfterAtomic string `json:"recipientAfterAtomic"`
	SourceStorageKey     string `json:"sourceStorageKey"`
	RecipientStorageKey  string `json:"recipientStorageKey"`
	Simulation           string `json:"simulation"`
	Receipt              struct {
		Status  string `json:"status"`
		GasUsed string `json:"gasUsed"`
		Log     struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		} `json:"log"`
	} `json:"receipt"`
}
type usdtOracle struct {
	SchemaVersion       int             `json:"schemaVersion"`
	Oracle              string          `json:"oracle"`
	Seed                string          `json:"seed"`
	Profile             string          `json:"profile"`
	GeneratorSHA256     string          `json:"generatorSHA256"`
	RuntimeSourceSHA256 string          `json:"runtimeSourceSHA256"`
	RuntimeKeccak256    string          `json:"runtimeKeccak256"`
	PackageLockSHA256   string          `json:"packageLockSHA256"`
	Fixtures            []usdtOracleRow `json:"fixtures"`
}

func usdtRead(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../../tests/oracles/evm/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func usdtVectors(t *testing.T) usdtOracle {
	t.Helper()
	var f usdtOracle
	if e := json.Unmarshal(usdtRead(t, "usdt-fixtures.json"), &f); e != nil {
		t.Fatal(e)
	}
	if f.SchemaVersion != 1 || f.Profile != Profile || f.Oracle != "ethers 6.17.0 + Ganache 7.9.2 actual pinned USDT runtime execution" || len(f.Fixtures) != 3 {
		t.Fatal("independent USDT oracle scope changed")
	}
	for name, want := range map[string]string{"usdt-generate.mjs": f.GeneratorSHA256, "usdt-runtime.json": f.RuntimeSourceSHA256, "package-lock.json": f.PackageLockSHA256} {
		sum := sha256.Sum256(usdtRead(t, name))
		if hex.EncodeToString(sum[:]) != want {
			t.Fatalf("USDT oracle provenance changed: %s; regenerate and review", name)
		}
	}
	return f
}
func usdtRuntime(t *testing.T) []byte {
	t.Helper()
	var f struct {
		Contract, Network, ChainID string
		Decimals                   uint8
		Code                       string
		RuntimeKeccak256           string
	}
	if e := json.Unmarshal(usdtRead(t, "usdt-runtime.json"), &f); e != nil {
		t.Fatal(e)
	}
	code, e := hex.DecodeString(strings.TrimPrefix(f.Code, "0x"))
	if e != nil {
		t.Fatal(e)
	}
	const pinned = "0xb44fb4e949d0f78f87f79ee46428f23a2a5713ce6fc6e0beb3dda78c2ac1ea55"
	if f.Contract != USDTContract || f.Network != "ethereum-mainnet" || f.ChainID != "1" || f.Decimals != 6 || f.RuntimeKeccak256 != pinned || crypto.Keccak256Hash(code).Hex() != pinned || tokenCodeHashes["ethereum-mainnet"] != pinned {
		t.Fatal("canonical USDT identity/runtime mismatch")
	}
	return code
}
func usdtPlan(t *testing.T, row usdtOracleRow) model.Plan {
	t.Helper()
	a := New()
	p := fixturePlan(t, a, "ethereum-mainnet")
	p.Account, _ = a.Derive(syntheticSeed(), row.Index, "ethereum-mainnet")
	p.Asset = USDTAsset
	p.Recipient = row.Recipient
	p.Amount = row.AmountAtomic
	p.MaxFee = "200000000100000"
	var x payload
	_ = json.Unmarshal(p.Payload, &x)
	x.To = USDTContract
	x.Value = "0"
	x.Data = row.Data
	x.Gas = "100000"
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	return p
}
func usdtNode(t *testing.T) *nodeFixture {
	t.Helper()
	f := fakeNode(t, "ethereum-mainnet")
	f.override["eth_estimateGas"] = "0x13880"
	code := "0x" + hex.EncodeToString(usdtRuntime(t))
	prior := f.intercept
	f.intercept = func(method string, params []json.RawMessage) (any, bool) {
		if method == "eth_getCode" {
			var address string
			_ = json.Unmarshal(params[0], &address)
			if strings.EqualFold(address, USDTContract) {
				return code, true
			}
		}
		if method == "eth_call" {
			var msg map[string]string
			_ = json.Unmarshal(params[0], &msg)
			if strings.EqualFold(msg["to"], USDTContract) {
				switch msg["data"] {
				case callData("deprecated()"), callData("paused()"), callData("basisPointsRate()"):
					return "0x" + strings.Repeat("0", 64), true
				}
				if strings.HasPrefix(msg["data"], "0xa9059cbb") {
					return "0x", true
				}
			}
		}
		if prior != nil {
			return prior(method, params)
		}
		return nil, false
	}
	return f
}
func usdtOverrideCall(f *nodeFixture, signature string, value any) {
	old := f.intercept
	f.intercept = func(method string, params []json.RawMessage) (any, bool) {
		if method == "eth_call" {
			var msg map[string]string
			_ = json.Unmarshal(params[0], &msg)
			if strings.EqualFold(msg["to"], USDTContract) && strings.HasPrefix(msg["data"], callData(signature)[:10]) {
				return value, true
			}
		}
		return old(method, params)
	}
}
func usdtReceipt(p model.Plan, s model.Signed, row usdtOracleRow) map[string]any {
	return map[string]any{"transactionHash": s.Identity, "blockHash": "0x" + strings.Repeat("a", 64), "blockNumber": "0x10", "status": "0x1", "logs": []any{map[string]any{"address": row.Receipt.Log.Address, "topics": append([]string(nil), row.Receipt.Log.Topics...), "data": row.Receipt.Log.Data}}}
}

func TestTP002USDTIndependentEthersTransactionsAndPrecision(t *testing.T) {
	f := usdtVectors(t)
	seed, e := hex.DecodeString(strings.TrimPrefix(f.Seed, "0x"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.EqualFold(f.RuntimeKeccak256, crypto.Keccak256Hash(usdtRuntime(t)).Hex()) {
		t.Fatal("runtime identity mismatch")
	}
	seen := map[uint32]bool{}
	for _, row := range f.Fixtures {
		seen[row.Index] = true
		if row.Contract != USDTContract || row.ChainID != "1" || row.Decimals != 6 || row.AmountDecimal != "123.456789" || row.AmountAtomic != "123456789" {
			t.Fatal("USDT identity/amount fixture changed")
		}
		a := New()
		p := usdtPlan(t, row)
		if p.Account.Address != row.Address || p.Account.PublicKey != row.PublicKey {
			t.Fatal("independent USDT source derivation mismatch")
		}
		value, e := amount.Parse(row.AmountDecimal, row.Decimals)
		if e != nil || value != row.AmountAtomic {
			t.Fatal("USDT decimal conversion is not exact")
		}
		if _, e = amount.Parse("123.4567891", 6); e == nil {
			t.Fatal("excess USDT decimal precision accepted")
		}
		atomic, _ := decimal(p.Amount)
		if transferData(p.Recipient, atomic) != row.Data {
			t.Fatal("independent transfer calldata mismatch")
		}
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		if types.LatestSignerForChainID(big.NewInt(1)).Hash(transaction(p, x)) != crypto.Keccak256Hash(common.FromHex(row.Unsigned)) {
			t.Fatal("independent unsigned type-2 transaction mismatch")
		}
		signed, e := a.Sign(seed, p)
		if e != nil {
			t.Fatal(e)
		}
		if "0x"+hex.EncodeToString(signed.Bytes) != row.Signed || signed.Identity != row.Hash {
			t.Fatal("independent USDT signed bytes/hash mismatch")
		}
	}
	for _, index := range []uint32{0, 1, 127} {
		if !seen[index] {
			t.Fatal("USDT fixture index coverage narrowed")
		}
	}
}

func TestTP050USDTPrepareBalancesSubmitPendingAndFinality(t *testing.T) {
	ctx := context.Background()
	f := usdtNode(t)
	p, e := f.prepare(t, "ethereum-mainnet", true)
	if e != nil {
		t.Fatal(e)
	}
	if p.Asset != USDTAsset || p.FeeAsset != "ethereum-mainnet:native" || p.Amount != "123456789" {
		t.Fatal("USDT asset/gas/atomic amount changed")
	}
	var x payload
	_ = json.Unmarshal(p.Payload, &x)
	if x.To != USDTContract || x.Value != "0" || !strings.HasPrefix(x.Data, "0xa9059cbb") {
		t.Fatal("USDT transfer does not call exact token")
	}
	balances, e := f.a.Balance(ctx, p.Account)
	if e != nil || len(balances) != 3 || balances[0].Asset != "ethereum-mainnet:native" || balances[1].Asset != USDTAsset || balances[1].Atomic != "1000000000000000000000000" {
		t.Fatalf("exact USDT+gas balance missing: %v", e)
	}
	s, e := f.a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.a.Submit(ctx, p, s)
	if e != nil || receipt.State != "SUBMITTED" || f.sent != s.Identity {
		t.Fatal("USDT exact artifact submission failed", receipt, e)
	}
	for _, known := range []any{nil, map[string]string{"hash": s.Identity}} {
		f.known = known
		receipt, e = f.a.Reconcile(ctx, p, s)
		want := "UNKNOWN"
		if known != nil {
			want = "SUBMITTED"
		}
		if e != nil || receipt.State != want {
			t.Fatal("USDT pending identity falsely finalized", receipt, e)
		}
	}
	row := usdtVectors(t).Fixtures[0]
	f.receipt = usdtReceipt(p, s, row)
	receipt, e = f.a.Reconcile(ctx, p, s)
	if e != nil || receipt.State != "CONFIRMED" {
		t.Fatal("exact canonical finalized USDT receipt rejected", receipt, e)
	}
	f.receipt.(map[string]any)["status"] = "0x0"
	receipt, e = f.a.Reconcile(ctx, p, s)
	if e != nil || receipt.State != "FAILED" {
		t.Fatal("reverted USDT receipt not failed", receipt, e)
	}
	f.receipt.(map[string]any)["blockHash"] = "0x" + strings.Repeat("b", 64)
	receipt, e = f.a.Reconcile(ctx, p, s)
	if e != nil || receipt.State != "REORGED" {
		t.Fatal("USDT reorg not retained", receipt, e)
	}
}

func TestTP052USDTRuntimeDecimalsIssuerStateAndNoReturnPolicy(t *testing.T) {
	ctx := context.Background()
	for _, signature := range []string{"deprecated()", "paused()", "basisPointsRate()"} {
		for _, value := range []string{"0x", "0x01", "0x" + strings.Repeat("0", 63) + "1", "0x" + strings.Repeat("f", 64)} {
			t.Run(signature+value, func(t *testing.T) {
				f := usdtNode(t)
				usdtOverrideCall(f, signature, value)
				if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
					t.Fatal("unsupported USDT issuer state prepared")
				}
				ac, _ := f.a.Derive(syntheticSeed(), 0, "ethereum-mainnet")
				if balances, e := f.a.Balance(ctx, ac); e != nil || len(balances) != 3 || balances[0].State != "provider-observed" || balances[1].Asset != USDTAsset || balances[1].State != "unavailable" || balances[1].Atomic != "" {
					t.Fatal("unsupported USDT issuer state invented a token balance or hid verified ETH")
				}
			})
		}
	}
	for _, value := range []string{"0x", "0x" + hex.EncodeToString(word(big.NewInt(18))), "0x" + hex.EncodeToString(word(big.NewInt(262)))} {
		f := usdtNode(t)
		usdtOverrideCall(f, "decimals()", value)
		if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
			t.Fatal("wrong or truncated USDT decimals accepted")
		}
	}
	for _, code := range []string{"0x", "0x6000", "0xzz"} {
		f := usdtNode(t)
		old := f.intercept
		f.intercept = func(method string, params []json.RawMessage) (any, bool) {
			if method == "eth_getCode" {
				var address string
				_ = json.Unmarshal(params[0], &address)
				if strings.EqualFold(address, USDTContract) {
					return code, true
				}
			}
			return old(method, params)
		}
		if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
			t.Fatal("wrong USDT runtime accepted")
		}
	}
	for _, value := range []string{"0x", "0x" + strings.Repeat("0", 63) + "1"} {
		f := usdtNode(t)
		usdtOverrideCall(f, "transfer(address,uint256)", value)
		if _, e := f.prepare(t, "ethereum-mainnet", true); e != nil {
			t.Fatal("exact registered USDT successful simulation rejected", e)
		}
	}
	for _, value := range []string{"0x00", "0x01", "0x" + strings.Repeat("0", 64), "0x" + strings.Repeat("0", 63) + "2"} {
		f := usdtNode(t)
		usdtOverrideCall(f, "transfer(address,uint256)", value)
		if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
			t.Fatal("false/malformed USDT simulation accepted")
		}
	}
	f := fakeNode(t, "base-sepolia")
	old := f.intercept
	f.intercept = func(method string, params []json.RawMessage) (any, bool) {
		if method == "eth_call" {
			var msg map[string]string
			_ = json.Unmarshal(params[0], &msg)
			if strings.HasPrefix(msg["data"], "0xa9059cbb") {
				return "0x", true
			}
		}
		if old != nil {
			return old(method, params)
		}
		return nil, false
	}
	if _, e := f.prepare(t, "base-sepolia", true); e == nil {
		t.Fatal("USDT no-return exception weakened ZEN transfer policy")
	}
}

func TestTP060USDTPendingTokenAndSeparateGasCannotBeDoubleSpent(t *testing.T) {
	for _, tc := range []struct {
		name, method, value string
		pending             bool
	}{{"latest gas", "eth_getBalance", "0x0", false}, {"pending gas", "eth_getBalance", "0x0", true}, {"latest token", "eth_call", "0x" + strings.Repeat("0", 64), false}, {"pending token", "eth_call", "0x" + strings.Repeat("0", 64), true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := usdtNode(t)
			old := f.intercept
			f.intercept = func(method string, params []json.RawMessage) (any, bool) {
				if method == tc.method {
					var at string
					_ = json.Unmarshal(params[1], &at)
					match := tc.pending && at == "pending" || !tc.pending && at != "pending"
					if match && method == "eth_getBalance" {
						return tc.value, true
					}
					if match && method == "eth_call" {
						var msg map[string]string
						_ = json.Unmarshal(params[0], &msg)
						if strings.HasPrefix(msg["data"], "0x70a08231") {
							return tc.value, true
						}
					}
				}
				return old(method, params)
			}
			if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
				t.Fatal("USDT prepared without independently spendable gas/token balance")
			}
		})
	}
}

func TestTP042USDTRejectsChangedContractsDecimalsDomainsAndEffects(t *testing.T) {
	row := usdtVectors(t).Fixtures[0]
	a := New()
	base := usdtPlan(t, row)
	for name, mutate := range map[string]func(*model.Plan){"ticker only": func(p *model.Plan) { p.Asset = "USDT" }, "foreign network token": func(p *model.Plan) { p.Asset = "base-mainnet:erc20:" + USDTContract }, "foreign account": func(p *model.Plan) { p.Account.Network = "base-mainnet" }, "USDT as fee": func(p *model.Plan) { p.FeeAsset = USDTAsset }, "token recipient": func(p *model.Plan) { p.Recipient = USDTContract }, "wrong atomic amount": func(p *model.Plan) { p.Amount = "123456788" }, "native value": func(p *model.Plan) {
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		x.Value = "1"
		p.Payload, _ = json.Marshal(x)
	}, "wrong contract": func(p *model.Plan) {
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		x.To = "0x1111111111111111111111111111111111111111"
		p.Payload, _ = json.Marshal(x)
	}, "approval calldata": func(p *model.Plan) {
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		x.Data = "0x095ea7b3" + strings.Repeat("0", 128)
		p.Payload, _ = json.Marshal(x)
	}, "wrong chain": func(p *model.Plan) {
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		x.ChainID = "8453"
		p.Payload, _ = json.Marshal(x)
	}} {
		t.Run(name, func(t *testing.T) {
			p := base
			mutate(&p)
			p.ID = planID(p)
			if a.ValidatePlan(p) == nil {
				t.Fatal("changed USDT signing effects accepted")
			}
			if _, e := a.Sign(syntheticSeed(), p); e == nil {
				t.Fatal("changed USDT signing plan signed")
			}
		})
	}
	s, e := a.Sign(syntheticSeed(), base)
	if e != nil {
		t.Fatal(e)
	}
	s.Bytes = append([]byte(nil), s.Bytes...)
	s.Bytes[len(s.Bytes)-1] ^= 1
	if _, e = a.verifySigned(base, s); e == nil {
		t.Fatal("tampered USDT signed artifact verified")
	}
}

func TestTP060USDTReceiptRequiresExactUnremovedTokenEffect(t *testing.T) {
	row := usdtVectors(t).Fixtures[0]
	f := usdtNode(t)
	p, e := f.prepare(t, "ethereum-mainnet", true)
	if e != nil {
		t.Fatal(e)
	}
	s, e := f.a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(map[string]any){"missing effect": func(r map[string]any) { r["logs"] = []any{} }, "foreign token": func(r map[string]any) {
		r["logs"].([]any)[0].(map[string]any)["address"] = "0x1111111111111111111111111111111111111111"
	}, "wrong amount": func(r map[string]any) {
		r["logs"].([]any)[0].(map[string]any)["data"] = "0x" + hex.EncodeToString(word(big.NewInt(123456788)))
	}, "removed": func(r map[string]any) { r["logs"].([]any)[0].(map[string]any)["removed"] = true }, "wrong source": func(r map[string]any) {
		r["logs"].([]any)[0].(map[string]any)["topics"].([]string)[1] = common.Hash{}.Hex()
	}, "wrong recipient": func(r map[string]any) {
		r["logs"].([]any)[0].(map[string]any)["topics"].([]string)[2] = common.Hash{}.Hex()
	}, "wrong identity": func(r map[string]any) { r["transactionHash"] = "0x" + strings.Repeat("b", 64) }} {
		t.Run(name, func(t *testing.T) {
			r := usdtReceipt(p, s, row)
			mutate(r)
			f.receipt = r
			receipt, e := f.a.Reconcile(context.Background(), p, s)
			if e == nil || receipt.State == "CONFIRMED" {
				t.Fatal("hostile USDT receipt certified", receipt, e)
			}
		})
	}
}

func TestTP050USDTGoSignedTransactionsExecuteOnIndependentGanache(t *testing.T) {
	if os.Getenv("WALLET_USDT_EVM") != "1" {
		t.Skip("NOT_RUN: explicit WALLET_USDT_EVM=1 and pinned cached Node/Ganache/ethers required; no public RPC or funds")
	}
	writeReport := func(value any) {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll("../../../reports", 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile("../../../reports/usdt-execution.json.tmp", append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename("../../../reports/usdt-execution.json.tmp", "../../../reports/usdt-execution.json"); err != nil {
			t.Fatal(err)
		}
	}
	writeReport(map[string]any{"status": "RUNNING", "sourceStable": false, "scope": "Explicit local Go-signed USDT integration started; no acceptance result yet"})
	t.Cleanup(func() {
		if t.Failed() {
			writeReport(map[string]any{"status": "FAIL", "sourceStable": false, "reason": "local-usdt-integration-failed; no acceptance claim"})
		}
	})
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal("explicit USDT integration requested without Node")
	}
	f := usdtVectors(t)
	sourceNames := []string{"internal/chains/evm/usdt_test.go", "internal/chains/evm/adapter.go", "internal/chains/evm/registry.go", "internal/amount/amount.go", "tests/oracles/evm/usdt-replay.mjs", "tests/oracles/evm/usdt-generate.mjs", "tests/oracles/evm/usdt-fixtures.json", "tests/oracles/evm/usdt-runtime.json", "tests/oracles/evm/package-lock.json", "go.mod", "go.sum"}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, name := range sourceNames {
			b, err := os.ReadFile("../../../" + name)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			out[name] = hex.EncodeToString(sum[:])
		}
		return out
	}
	before := snapshot()
	results := []map[string]any{}
	stable := false
	report := func(status string) {
		writeReport(map[string]any{"status": status, "sourceSHA256": before, "scope": "Actual Go-signed type-2 Ethereum-shaped USDT transactions executed against pinned deployed USDT runtime in isolated in-process Ganache; synthetic storage/funds only; no Bybit, public RPC, mainnet funds, consensus or finality claim", "results": results, "sourceStable": stable && !t.Failed()})
	}
	report("RUNNING")
	t.Cleanup(func() {
		status := "PASS"
		if t.Failed() {
			status = "FAIL"
		}
		report(status)
	})
	for _, row := range f.Fixtures {
		t.Run(fmt.Sprintf("index-%d", row.Index), func(t *testing.T) {
			signed, err := New().Sign(syntheticSeed(), usdtPlan(t, row))
			if err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(map[string]string{"signed": "0x" + hex.EncodeToString(signed.Bytes), "source": row.Address, "recipient": row.Recipient, "amount": row.AmountAtomic})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, node, "../../../tests/oracles/evm/usdt-replay.mjs")
			cmd.Stdin = bytes.NewReader(input)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal("actual Go-signed USDT execution oracle failed; no acceptance claim")
			}
			var result map[string]any
			if len(out) > 65536 || json.Unmarshal(out, &result) != nil {
				t.Fatal("bounded independent oracle report missing")
			}
			if result["status"] != "PASS" || result["identity"] != signed.Identity || result["source"] != row.Address || result["recipient"] != row.Recipient || result["sourceAfterAtomic"] != row.SourceAfterAtomic || result["recipientAfterAtomic"] != row.RecipientAfterAtomic || result["amountAtomic"] != row.AmountAtomic || result["nonceAfter"] != "8" || result["receiptStatus"] != "0x1" || result["chainId"] != "1" || result["simulation"] != "0x" || result["transactionType"] != float64(2) || result["nativeDebitAtomic"] != result["gasChargeAtomic"] {
				t.Fatal("independent execution report differs from Go-approved USDT effects")
			}
			results = append(results, result)
		})
	}
	after := snapshot()
	stable = true
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("USDT integration source changed during execution: %s", name)
		}
	}
}

func TestTP060USDTPendingIssuerChangeAndMalformedReadsFailClosed(t *testing.T) {
	for _, signature := range []string{"deprecated()", "paused()", "basisPointsRate()"} {
		t.Run(signature+" pending change", func(t *testing.T) {
			f := usdtNode(t)
			old := f.intercept
			f.intercept = func(method string, params []json.RawMessage) (any, bool) {
				if method == "eth_call" {
					var msg map[string]string
					var at string
					_ = json.Unmarshal(params[0], &msg)
					_ = json.Unmarshal(params[1], &at)
					if at == "pending" && strings.EqualFold(msg["to"], USDTContract) && msg["data"] == callData(signature) {
						return "0x" + strings.Repeat("0", 63) + "1", true
					}
				}
				return old(method, params)
			}
			if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
				t.Fatal("pending issuer policy change accepted under stale latest-state approval")
			}
		})
		for name, value := range map[string]any{"null": nil, "numeric": 1, "object": map[string]string{"error": "untrusted result"}} {
			t.Run(signature+name, func(t *testing.T) {
				f := usdtNode(t)
				usdtOverrideCall(f, signature, value)
				if _, e := f.prepare(t, "ethereum-mainnet", true); e == nil {
					t.Fatal("malformed issuer RPC response accepted")
				}
			})
		}
	}
}
