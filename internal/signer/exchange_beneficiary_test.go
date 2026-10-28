package signer

import (
	"bytes"
	"encoding/json"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/vault"
	"strings"
	"testing"
)

var publicUAEFields1 = []string{"ARE", "PUBLIC-TEST-EMIRATE-1761", "PUBLIC-TEST-CITY-8274", "PUBLIC-TEST-BUILDING-2019"}
var publicUAEFields2 = []string{"PUBLIC-TEST-STREET-6713", "00000", "1990-02-15", "PUBLIC-TEST-BIRTHPLACE-1731"}

func TestExchangeTP040UAEPrivateInputsExactApprovalEncryptedPersistence(t *testing.T) {
	e, u, a := exchangeEngine(t)
	u.inputs = append(u.inputs, input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.ae"); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(publicUAEFields1...), input(publicUAEFields2...))
	in := ExchangeIntent{Kind: "withdraw", AccountID: a.ID, AssetID: a.Network + ":native", ChainType: "Ethereum", Withdrawal: &bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: a.Address, Amount: "0.2", ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	op, err := e.PrepareExchange(in)
	if err != nil {
		t.Fatal(err)
	}
	var w bybit.WithdrawalRequest
	if json.Unmarshal([]byte(op.Payload), &w) != nil || w.Beneficiary == nil || w.Beneficiary.Country != "ARE" || w.Beneficiary.WalletType != "1" || w.Beneficiary.UnhostedWalletType != "0" {
		t.Fatal("documented UAE data not bound to owned withdrawal")
	}
	ordinary, _ := json.Marshal(e.Snapshot())
	for _, value := range append(publicUAEFields1[1:], publicUAEFields2...) {
		if bytes.Contains(ordinary, []byte(value)) {
			t.Fatal("PII escaped ordinary wallet snapshot")
		}
		for _, name := range []string{"vault.lwv", "vault.previous.lwv"} {
			cipher, err := e.Files.Read(name)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(cipher, []byte(value)) {
				t.Fatal("PII persisted outside encryption")
			}
		}
	}
	op = authorizeExchange(t, e, u, op)
	summary := u.summaries[len(u.summaries)-1]
	for _, value := range append(publicUAEFields1, publicUAEFields2...) {
		if !strings.Contains(summary, value) {
			t.Fatal("private approval omitted exact data to be transmitted")
		}
	}
	if !strings.Contains(summary, "https://api.bybit.ae") || !strings.Contains(summary, "your own individual noncustodial wallet") {
		t.Fatal("PII destination/ownership absent")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(op)); err != nil {
		t.Fatal(err)
	}
	w.Beneficiary.City = "changed-after-approval"
	raw, _ := json.Marshal(w)
	request := exchangeRequest(op)
	request.Payload = string(raw)
	if _, err = e.ExchangeAuth(request); err == nil {
		t.Fatal("modified beneficiary authenticated")
	}
}

func TestExchangeTP040UAEInformationCancellationAndMalformedInputFailClosed(t *testing.T) {
	for _, scenario := range []string{"consent", "first-cancel", "first-count", "second-cancel", "second-count", "invalid", "lock"} {
		t.Run(scenario, func(t *testing.T) {
			e, u, _ := exchangeEngine(t)
			e.data.Exchange.Credentials.Endpoint = "https://api.bybit.ae"
			switch scenario {
			case "consent":
				u.approve = false
			case "first-cancel":
			case "first-count":
				u.inputs = append(u.inputs, input("ARE"))
			case "second-cancel":
				u.inputs = append(u.inputs, input(publicUAEFields1...))
			case "second-count":
				u.inputs = append(u.inputs, input(publicUAEFields1...), input("one"))
			case "invalid":
				u.inputs = append(u.inputs, input("ZZZ", "State", "City", "Building"), input(publicUAEFields2...))
			case "lock":
				u.inputs = append(u.inputs, input(publicUAEFields1...))
				e.UI = &exchangeHookUI{testUI: u, inputHook: e.Lock}
			}
			if out, err := e.exchangeUAEBeneficiary(); err == nil || out != nil {
				t.Fatal("cancelled/malformed PII accepted")
			}
		})
	}
	e, _, a := exchangeEngine(t)
	if _, err := e.exchangeUAEBeneficiary(); err == nil {
		t.Fatal("non-UAE endpoint asked for PII")
	}
	in := ExchangeIntent{Kind: "withdraw", AccountID: a.ID, AssetID: a.Network + ":native", ChainType: "Ethereum", Withdrawal: &bybit.WithdrawalRequest{Beneficiary: &bybit.UAEBeneficiary{}}}
	if _, err := e.PrepareExchange(in); err == nil {
		t.Fatal("caller injected private beneficiary instead of masked input")
	}
	e.Lock()
	if _, err := e.exchangeUAEBeneficiary(); err == nil {
		t.Fatal("locked PII prompt")
	}
	if exchangeBeneficiarySummary(nil) != "" {
		t.Fatal("invented beneficiary summary")
	}
}

func TestExchangeTP060DefinitiveRejectionReleasesOnlyDocumentedFirstPostFailures(t *testing.T) {
	e, u, a := exchangeEngine(t)
	for _, code := range []int64{10001, 10003, 10004, 10005} {
		op, err := e.PrepareExchange(exchangeSwap(a))
		if err != nil {
			t.Fatal(err)
		}
		if e.RejectExchange(op.ID, code) == nil {
			t.Fatal("unsubmitted operation rejected as server response")
		}
		op = authorizeExchange(t, e, u, op)
		if _, err = e.ExchangeAuth(exchangeRequest(op)); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []int64{0, 10000, 10002, 10006, 10014, 10016, 429, 403, 110072, 170141, 131085, 131214} {
			if e.RejectExchange(op.ID, bad) == nil {
				t.Fatal("ambiguous/unsupported error released reservation")
			}
		}
		if err = e.RejectExchange(op.ID, code); err != nil {
			t.Fatal(err)
		}
		stored, err := e.ExchangeOperation(op.ID)
		if err != nil || stored.State != "FAILED" || stored.RejectionCode != code || stored.ExternalID != "" || stored.Digest != op.Digest {
			t.Fatal("definitive rejection lost exact identity or fabricated remote ID")
		}
		if e.RejectExchange(op.ID, code) == nil {
			t.Fatal("terminal rejection replayed")
		}
	}
	op, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	op = authorizeExchange(t, e, u, op)
	if _, err = e.ExchangeAuth(exchangeRequest(op)); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(op.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	if e.RejectExchange(op.ID, 10003) == nil {
		t.Fatal("new auth failure falsely resolved earlier timeout")
	}
	e.data.Exchange.Operations[len(e.data.Exchange.Operations)-1].State = "SUBMITTING"
	e.data.Exchange.Operations[len(e.data.Exchange.Operations)-1].ExternalID = "known-remote"
	if e.RejectExchange(op.ID, 10001) == nil {
		t.Fatal("known remote record erased by generic error")
	}
	e.data.Exchange.Operations[len(e.data.Exchange.Operations)-1].ExternalID = ""
	e.parameters = vault.Parameters{}
	if e.RejectExchange(op.ID, 10001) == nil || !e.Snapshot().Locked {
		t.Fatal("failed durable rejection not quarantined")
	}
	if e.RejectExchange("missing", 10001) == nil {
		t.Fatal("locked rejection mutation")
	}
}

func TestExchangeKnownUnsupportedRegionalWithdrawalFormsFailBeforePersistence(t *testing.T) {
	e, u, a := exchangeEngine(t)
	in := ExchangeIntent{Kind: "withdraw", AccountID: a.ID, AssetID: a.Network + ":native", ChainType: "Ethereum", Withdrawal: &bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: a.Address, Amount: "0.2", ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	beforeGeneration := e.data.Generation
	beforeConfirm := len(u.summaries)
	for _, endpoint := range []string{"https://api.bybit.tr", "https://api.bybit.kz", "https://api.bybit.eu", "https://api.bybit.id"} {
		e.data.Exchange.Credentials.Endpoint = endpoint
		if _, err := e.PrepareExchange(in); err == nil {
			t.Fatal("unsupported regional form bypassed")
		}
		if len(e.data.Exchange.Operations) != 0 || e.data.Generation != beforeGeneration || len(u.summaries) != beforeConfirm {
			t.Fatal("unsupported withdrawal created operation or misleading approval")
		}
	}
}
