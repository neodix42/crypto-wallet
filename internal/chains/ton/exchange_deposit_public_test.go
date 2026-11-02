package ton

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"localwallet/internal/model"
)

type publicExchangeDepositFixture struct {
	SourceAddress       string `json:"sourceAddress"`
	RecipientAddress    string `json:"recipientAddress"`
	PublicKey           string `json:"publicKey"`
	ExternalHash        string `json:"externalHash"`
	SourceHash          string `json:"sourceHash"`
	RecipientHash       string `json:"recipientHash"`
	InternalMessageHash string `json:"internalMessageHash"`
	SourceBOC           string `json:"sourceBOC"`
	RecipientBOC        string `json:"recipientBOC"`
}

// Public, already executed chain data only. The original user's encrypted plan
// is deliberately unavailable: fee/state bookkeeping below is synthetic test
// metadata, while the signed recipient, amount, memo, nonce and expiry are
// reconstructed and checked against the real public external-message signature.
func publicExchangeDeposit(t *testing.T) (*Adapter, model.Plan, model.Signed, *tlb.Transaction, *tlb.Transaction, publicExchangeDepositFixture) {
	t.Helper()
	raw, err := os.ReadFile("testdata/bybit-native-deposit-public.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture publicExchangeDepositFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	readTransaction := func(boc, hash string) *tlb.Transaction {
		c, err := decodeBOC(boc)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(c.Hash()) != hash {
			t.Fatal("public transaction fixture identity changed")
		}
		slice, err := c.BeginParse()
		if err != nil {
			t.Fatal(err)
		}
		tx := new(tlb.Transaction)
		if err = tlb.LoadFromCell(tx, slice); err != nil {
			t.Fatal(err)
		}
		tx.Hash = c.Hash()
		return tx
	}
	source := readTransaction(fixture.SourceBOC, fixture.SourceHash)
	recipient := readTransaction(fixture.RecipientBOC, fixture.RecipientHash)
	pub, err := hex.DecodeString(fixture.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	account, err := accountForProfile(pub, 0, "ton-mainnet", Profile)
	if err != nil || account.Address != fixture.SourceAddress {
		t.Fatal("public source key/immutable V5R1 descriptor mismatch", err)
	}
	if source.IO.In == nil || source.IO.In.MsgType != tlb.MsgTypeExternalIn {
		t.Fatal("public source has no external-in message")
	}
	ext := source.IO.In.AsExternalIn()
	body, err := ext.Body.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = body.LoadUInt(64); err != nil {
		t.Fatal(err)
	}
	expiry, err := body.LoadUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := body.LoadUInt(32)
	if err != nil || nonce != 2 {
		t.Fatal("public source sequence changed", err)
	}
	x := Payload{Seqno: uint32(nonce), Bounce: true, SourceBalance: "10000000000", EstimatedFee: "1000000", StorageReserve: "10000000", SourceDataHash: strings.Repeat("0", 64)}
	p := model.Plan{Account: account, Asset: "ton-mainnet:TON", Recipient: fixture.RecipientAddress, Amount: "6000000000", Nonce: "2", FeeAsset: "ton-mainnet:TON", MaxFee: "3000000", Expires: int64(expiry)}
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	externalCell, err := source.IO.In.ToCell()
	if err != nil || hex.EncodeToString(externalCell.Hash()) != fixture.ExternalHash {
		t.Fatal("public external message identity changed", err)
	}
	return New(), p, model.Signed{Identity: fixture.ExternalHash, Bytes: externalCell.ToBOC()}, source, recipient, fixture
}

func TestTONPublicBybitDepositReconcilesExactRecipientCredit(t *testing.T) {
	a, plan, signed, source, recipient, fixture := publicExchangeDeposit(t)
	outgoingCell, err := tlb.ToCell(outgoing(t, source))
	if err != nil || hex.EncodeToString(outgoingCell.Hash()) != fixture.InternalMessageHash {
		t.Fatal("public outgoing message identity changed", err)
	}
	incomingCell, err := recipient.IO.In.ToCell()
	if err != nil || hex.EncodeToString(incomingCell.Hash()) != fixture.InternalMessageHash {
		t.Fatal("public source-to-recipient message linkage changed", err)
	}
	_, close := archivalServer(t, a, plan, map[string][]*tlb.Transaction{plan.Account.Address: {source}, plan.Recipient: {recipient}}, 100, nil)
	defer close()
	receipt, err := a.Reconcile(context.Background(), plan, signed)
	if err != nil || receipt.State != "CONFIRMED" || receipt.Identity != fixture.ExternalHash || receipt.Block != fixture.RecipientHash || receipt.SourceTransaction != fixture.SourceHash {
		t.Fatal("real public deposit not reconciled to its recipient transaction", receipt, err)
	}
	if receipt.Block == fixture.SourceHash || receipt.Block == receipt.Identity {
		t.Fatal("recipient transaction was replaced by a different TON identity")
	}
}

// Opt-in live replay of the already published transfer. This validates the
// ordinary production RPC/archival reader, never prepares/signs/submits a new
// transaction and never reads local application storage or credentials.
func TestTONPublicBybitDepositReadOnlyProvider(t *testing.T) {
	if os.Getenv("WALLET_TON_PUBLIC_PROBE") != "1" {
		t.Skip("NOT_RUN: WALLET_TON_PUBLIC_PROBE=1 enables read-only public TON deposit reconciliation")
	}
	a, plan, signed, _, _, fixture := publicExchangeDeposit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	receipt, err := a.Reconcile(ctx, plan, signed)
	if err != nil || receipt.State != "CONFIRMED" || receipt.Identity != fixture.ExternalHash || receipt.Block != fixture.RecipientHash || receipt.SourceTransaction != fixture.SourceHash {
		t.Fatal("live archival RPC did not reconcile exact public deposit identities", receipt, err)
	}
	t.Log("PASS read-only public source/signature/action, exact source-to-recipient message linkage and 6 GRAM recipient credit; no broadcast or exchange-account access")
}

func TestTONPublicBybitDepositHostileTraceCannotConfirm(t *testing.T) {
	for _, name := range []string{"source-action-failed", "recipient-execution-failed", "recipient-short-credit", "recipient-other-message", "recipient-absent", "changed-approved-amount", "changed-approved-memo"} {
		t.Run(name, func(t *testing.T) {
			a, plan, signed, source, recipient, fixture := publicExchangeDeposit(t)
			want := "FAILED"
			wantError := false
			switch name {
			case "source-action-failed":
				ordinary(t, source, func(d *tlb.TransactionDescriptionOrdinary) { d.ActionPhase.Success = false })
			case "recipient-execution-failed":
				ordinary(t, recipient, func(d *tlb.TransactionDescriptionOrdinary) { d.Aborted = true })
			case "recipient-short-credit":
				ordinary(t, recipient, func(d *tlb.TransactionDescriptionOrdinary) { d.CreditPhase.Credit.Coins = tlb.FromNanoTONU(1) })
			case "recipient-other-message":
				recipient.IO.In.AsInternal().CreatedLT++
				want = "SUBMITTED"
				wantError = true
			case "recipient-absent":
				want = "SUBMITTED"
			case "changed-approved-amount":
				plan.Amount = "6000000001"
				plan.ID = planID(plan)
				want = ""
				wantError = true
			case "changed-approved-memo":
				plan.Memo = "not in the real signed transaction"
				plan.ID = planID(plan)
				want = ""
				wantError = true
			}
			chains := map[string][]*tlb.Transaction{plan.Account.Address: {source}, plan.Recipient: {recipient}}
			if name == "recipient-absent" {
				chains[plan.Recipient] = nil
			}
			_, close := archivalServer(t, a, plan, chains, 100, nil)
			defer close()
			receipt, err := a.Reconcile(context.Background(), plan, signed)
			if receipt.State != want || (err != nil) != wantError {
				t.Fatal("hostile public trace did not preserve unresolved/failure state", receipt, err)
			}
			wantSource := ""
			if want == "SUBMITTED" {
				wantSource = fixture.SourceHash
			}
			if receipt.SourceTransaction != wantSource {
				t.Fatal("source transaction metadata was omitted or exposed before exact source execution verification", receipt)
			}
		})
	}
}
