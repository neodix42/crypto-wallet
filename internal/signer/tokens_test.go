package signer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

func signerToken(network string) model.ERC20Token {
	contract := common.HexToAddress("0x1234567890123456789012345678901234567890").Hex()
	return model.ERC20Token{Network: network, Asset: model.Asset{ID: network + ":erc20:" + contract, Symbol: "TEST", Decimals: 6, Contract: contract}, CodeHash: "0x" + strings.Repeat("a", 64)}
}

func customTokenPlan(account model.Account, token model.ERC20Token) model.Plan {
	plan := evmPlan(account)
	plan.Asset = token.Asset.ID
	var body map[string]any
	_ = json.Unmarshal(plan.Payload, &body)
	body["to"], body["value"] = token.Asset.Contract, "0"
	body["data"] = "0xa9059cbb" + strings.TrimPrefix(common.BytesToHash(common.HexToAddress(plan.Recipient).Bytes()).Hex(), "0x") + fmt.Sprintf("%064x", 123456789)
	plan.Payload, _ = json.Marshal(body)
	plan.ID = ""
	raw, _ := json.Marshal(plan)
	digest := sha256.Sum256(raw)
	plan.ID = "evm:" + hex.EncodeToString(digest[:])
	return plan
}

func TestTokenImportPersistsAcrossFreshEngineAndSignsOnlyExactTransfer(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	account := findAccount(t, e, "ethereum-sepolia")
	token := signerToken(account.Network)
	if _, err := e.adapter(account.Network); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.ImportToken(account.ID, token); err != nil {
		t.Fatal(err)
	}
	for _, detail := range []string{token.Asset.Contract, token.CodeHash, "Decimals: 6", account.Address, "proxy implementation"} {
		if !strings.Contains(ui.summaries[len(ui.summaries)-1], detail) {
			t.Fatal("private import approval omitted exact identity or issuer limits")
		}
	}
	policies := e.TokenPolicies()
	policies[0].Asset.Decimals = 18
	if e.TokenPolicies()[0] != token || e.ImportToken(account.ID, token) == nil {
		t.Fatal("token policies aliased public output or accepted duplicate import")
	}
	e.Lock()
	if e.TokenPolicies() != nil {
		t.Fatal("locked signer exposed encrypted watch metadata")
	}
	e2 := New(e.Files, ui, []model.Adapter{evm.New()})
	e2.parameters = e.parameters
	t.Cleanup(e2.Lock)
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e2.Unlock(); err != nil || len(e2.TokenPolicies()) != 1 || e2.TokenPolicies()[0] != token {
		t.Fatal("fresh signer failed to restore encrypted token metadata", err)
	}
	adapter, _ := e2.adapter(account.Network)
	plan := customTokenPlan(account, token)
	if err := adapter.ValidatePlan(plan); err != nil {
		t.Fatal("restored policy does not authorize its exact token transfer", err)
	}
	if err := e2.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	signed, err := e2.Sign(plan.ID)
	if err != nil || signed.Identity == "" {
		t.Fatal("actual signer refused password-approved imported token payment", err)
	}
	var transaction types.Transaction
	if transaction.UnmarshalBinary(signed.Bytes) != nil || transaction.To() == nil || transaction.To().Hex() != token.Asset.Contract || transaction.Value().Sign() != 0 || transaction.ChainId().String() != "11155111" || len(transaction.Data()) != 68 {
		t.Fatal("imported-token signature changed network, contract, native value or transfer calldata")
	}
	approval := ui.summaries[len(ui.summaries)-1]
	if !strings.Contains(approval, token.CodeHash) || !strings.Contains(approval, "Token decimals: 6") || !strings.Contains(approval, token.Asset.Contract) || strings.Contains(approval, syntheticPassword) {
		t.Fatal("spend approval omitted imported policy or leaked credentials")
	}
	if e2.tokenApproval(evmPlan(account)) != "" {
		t.Fatal("native transfer gained imported token effects")
	}
}

type tokenLifecycleUI struct {
	*testUI
	confirmHook func()
	inputHook   func()
}

func (ui *tokenLifecycleUI) Confirm(title, summary string) bool {
	accepted := ui.testUI.Confirm(title, summary)
	if ui.confirmHook != nil {
		ui.confirmHook()
	}
	return accepted
}
func (ui *tokenLifecycleUI) Input(title string, labels ...string) ([][]byte, error) {
	values, err := ui.testUI.Input(title, labels...)
	if ui.inputHook != nil {
		ui.inputHook()
	}
	return values, err
}

func TestTokenImportRejectsCancelledWrongPasswordStaleAndLockedRequestsAtomically(t *testing.T) {
	for _, mode := range []string{"cancel", "password", "missing-password", "generation-confirm", "generation-password", "lock-confirm", "lock-password", "persist"} {
		t.Run(mode, func(t *testing.T) {
			e, original, _ := newEngine(t, []model.Adapter{evm.New()})
			createImported(t, e, original, "testnet")
			account := findAccount(t, e, "ethereum-sepolia")
			ui := &tokenLifecycleUI{testUI: original}
			e.UI = ui
			if mode != "missing-password" {
				password := syntheticPassword
				if mode == "password" {
					password = "wrong-synthetic-password"
				}
				original.inputs = append(original.inputs, input(password))
			}
			switch mode {
			case "cancel":
				original.approve = false
			case "generation-confirm":
				ui.confirmHook = func() { e.data.Generation++ }
			case "generation-password":
				ui.inputHook = func() { e.data.Generation++ }
			case "lock-confirm":
				ui.confirmHook = e.Lock
			case "lock-password":
				ui.inputHook = e.Lock
			case "persist":
				e.Files.Close()
			}
			if e.ImportToken(account.ID, signerToken(account.Network)) == nil || len(e.TokenPolicies()) != 0 {
				t.Fatal("cancelled/stale/failed authorization persisted an imported token")
			}
		})
	}
}

func TestTokenImportBoundsOwnershipAndExcludesUnreviewedBybitRoutes(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	if e.ImportToken("foreign", signerToken("ethereum-mainnet")) == nil {
		t.Fatal("locked import succeeded")
	}
	createImported(t, e, ui, "mainnet")
	account := findAccount(t, e, "ethereum-mainnet")
	token := signerToken(account.Network)
	if e.ImportToken("foreign", token) == nil || e.ImportToken(account.ID, signerToken("ethereum-sepolia")) == nil || e.ImportToken(account.ID, model.ERC20Token{}) == nil {
		t.Fatal("foreign account/network or malformed token policy accepted")
	}
	e.data.RecoveryConfirmed = false
	if e.ImportToken(account.ID, token) == nil {
		t.Fatal("unconfirmed recovery allowed policy authorization")
	}
	e.data.RecoveryConfirmed = true
	e.data.ERC20Tokens = make([]model.ERC20Token, 32)
	if e.ImportToken(account.ID, token) == nil {
		t.Fatal("unbounded token policies accepted")
	}
	e.data.ERC20Tokens = nil
	e.data.ERC20Tokens = []model.ERC20Token{{}}
	if e.ImportToken(account.ID, token) == nil {
		t.Fatal("invalid existing policy state authorized a new import")
	}
	e.data.ERC20Tokens = nil
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if e.ImportToken(account.ID, token) != nil {
		t.Fatal("confirmed bounded token import failed")
	}
	if _, _, err := e.exchangeAsset(account.ID, token.Asset.ID); err == nil {
		t.Fatal("unreviewed imported token became an authorized Bybit route")
	}
	if _, _, err := e.exchangeAsset(account.ID, account.Network+":native"); err != nil {
		t.Fatal("imported token removed the existing native Bybit route")
	}
	if _, err := adaptersWithTokens([]model.Adapter{evm.New()}, []model.ERC20Token{{}}); err == nil {
		t.Fatal("invalid policy bypassed signer adapter construction")
	}
}
