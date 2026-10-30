package signer

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/decred/base58"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
)

func signerNEP141Token(network string) model.NEP141Token {
	const contract = "token.testnet"
	return model.NEP141Token{Network: network, Asset: model.Asset{ID: network + ":nep141:" + contract, Symbol: "PUBLIC", Decimals: 6, Contract: contract}, CodeHash: base58.Encode(bytes.Repeat([]byte{0x42}, 32))}
}

func TestNEP141ImportRequiresExactApprovalAndRestoresEncryptedPolicies(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, e, ui, "testnet")
	account := findAccount(t, e, "near-testnet")
	token := signerNEP141Token(account.Network)
	before := e.Snapshot().Accounts
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.ImportNEP141Token(account.ID, token); err != nil {
		t.Fatal(err)
	}
	for _, detail := range []string{token.Network, token.Asset.Contract, token.Asset.Symbol, "Decimals: 6", token.CodeHash, account.Address, "1 yoctoNEAR", "storage registration", "not an audit"} {
		if !strings.Contains(ui.summaries[len(ui.summaries)-1], detail) {
			t.Fatal("import approval omitted exact authority or trust limit", detail)
		}
	}
	policies := e.NEP141TokenPolicies()
	policies[0].Asset.Decimals = 18
	if e.NEP141TokenPolicies()[0] != token || e.ImportNEP141Token(account.ID, token) == nil {
		t.Fatal("policy output aliased authority or duplicate import accepted")
	}
	if _, _, err := e.exchangeAsset(account.ID, token.Asset.ID); err == nil {
		t.Fatal("imported NEAR token gained an unreviewed Bybit route")
	}
	e.Lock()
	if e.NEP141TokenPolicies() != nil {
		t.Fatal("locked signer retained public policy cache")
	}
	for _, network := range e.Adapters[0].Networks() {
		for _, asset := range network.Assets {
			if asset.ID == token.Asset.ID {
				t.Fatal("lock retained token signing policy")
			}
		}
	}
	fresh := New(e.Files, ui, []model.Adapter{near.New()})
	fresh.parameters = e.parameters
	defer fresh.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := fresh.Unlock(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh.Snapshot().Accounts, before) || !reflect.DeepEqual(fresh.NEP141TokenPolicies(), []model.NEP141Token{token}) {
		t.Fatal("fresh signer changed canonical accounts or lost approved policy")
	}
	found := false
	for _, network := range fresh.Adapters[0].Networks() {
		for _, asset := range network.Assets {
			if asset == token.Asset {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("unlock did not install exact approved adapter policy")
	}
}

func TestNEP141ImportDeniesCancelledWrongPasswordStaleAndFailedPersistence(t *testing.T) {
	for _, mode := range []string{"cancel", "password", "missing-password", "generation-confirm", "generation-password", "lock-confirm", "lock-password", "persist"} {
		t.Run(mode, func(t *testing.T) {
			e, original, _ := newEngine(t, []model.Adapter{near.New()})
			createImported(t, e, original, "testnet")
			account := findAccount(t, e, "near-testnet")
			ui := &tokenLifecycleUI{testUI: original}
			e.UI = ui
			if mode != "missing-password" {
				password := syntheticPassword
				if mode == "password" {
					password = "WRONG-PUBLIC-PASSWORD"
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
			if e.ImportNEP141Token(account.ID, signerNEP141Token(account.Network)) == nil || len(e.NEP141TokenPolicies()) != 0 {
				t.Fatal("failed authorization or storage installed a NEAR token policy")
			}
		})
	}
}

func TestNEP141ImportBoundsOwnershipEnvironmentAndRecovery(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	if e.ImportNEP141Token("foreign", signerNEP141Token("near-testnet")) == nil {
		t.Fatal("locked import accepted")
	}
	createImported(t, e, ui, "testnet")
	account := findAccount(t, e, "near-testnet")
	token := signerNEP141Token(account.Network)
	for _, request := range []struct {
		id    string
		token model.NEP141Token
	}{{"foreign", token}, {account.ID, signerNEP141Token("near-mainnet")}, {account.ID, model.NEP141Token{}}} {
		if e.ImportNEP141Token(request.id, request.token) == nil {
			t.Fatal("foreign/malformed policy accepted")
		}
	}
	e.data.RecoveryConfirmed = false
	if e.ImportNEP141Token(account.ID, token) == nil {
		t.Fatal("unconfirmed recovery authorized token")
	}
	e.data.RecoveryConfirmed = true
	e.data.NEP141Tokens = make([]model.NEP141Token, near.MaxImportedTokens)
	if e.ImportNEP141Token(account.ID, token) == nil {
		t.Fatal("policy count unbounded")
	}
	e.data.NEP141Tokens = []model.NEP141Token{{}}
	if e.ImportNEP141Token(account.ID, token) == nil {
		t.Fatal("invalid existing policy state accepted")
	}
	e.data.NEP141Tokens = nil
	if _, err := adaptersWithNEP141Tokens([]model.Adapter{near.New()}, []model.NEP141Token{{}}); err == nil {
		t.Fatal("invalid policy bypassed adapter setup")
	}
}

func TestNEP141MainnetWatchPolicyCannotGainBybitAuthority(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, e, ui, "mainnet")
	account := findAccount(t, e, "near-mainnet")
	token := signerNEP141Token(account.Network)
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.ImportNEP141Token(account.ID, token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.exchangeAsset(account.ID, token.Asset.ID); err == nil {
		t.Fatal("mainnet imported NEAR token gained an unreviewed Bybit route")
	}
	if _, _, err := e.exchangeAsset(account.ID, account.Network+":native"); err != nil {
		t.Fatal("token policy removed existing native NEAR route", err)
	}
}
