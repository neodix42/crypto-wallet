package decred

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"localwallet/internal/model"
)

func TestDescriptorRecoveryAndSequenceBoundaries(t *testing.T) {
	a, seed, p := testPlan(t)
	if _, e := SetDiscovery(model.Account{}, 0, 20, 100); e == nil {
		t.Fatal("foreign discovery account accepted")
	}
	q := p.Account
	meta, cp, _ := descriptor(q)
	meta.AccountXPub = "not-an-xpub"
	q.Parameters, _ = json.Marshal(meta)
	if _, _, e := descriptor(q); e == nil {
		t.Fatal("malformed xpub accepted")
	}
	if _, e := AddressAt(q, 0, 0); e == nil {
		t.Fatal("malformed xpub generated address")
	}
	if e := ValidateRecovery(model.Account{}, p.Account); e == nil {
		t.Fatal("missing recovery root accepted")
	}
	q = p.Account
	q.ID = "another-account"
	if e := ValidateRecovery(p.Account, q); e == nil {
		t.Fatal("restored stable ID ignored")
	}
	meta, _, _ = descriptor(p.Account)
	key, e := hdkeychain.NewKeyFromString(meta.AccountXPub, cp)
	if e != nil {
		t.Fatal(e)
	}
	defer key.Zero()
	meta.ReceiveIndex = maxIndex - 1
	addr, pub, e := publicAt(key, cp, 0, meta.ReceiveIndex)
	if e != nil {
		t.Fatal(e)
	}
	q = p.Account
	q.Address = addr
	q.PublicKey = hex.EncodeToString(pub)
	q.Parameters, _ = json.Marshal(meta)
	if _, e := AdvanceReceive(q); e == nil {
		t.Fatal("receive counter overflow reused an address")
	}
	meta, _, _ = descriptor(p.Account)
	meta.ChangeIndex = maxIndex - 1
	q = p.Account
	q.Parameters, _ = json.Marshal(meta)
	if _, e := AdvanceChange(q); e == nil {
		t.Fatal("change counter overflow reused an address")
	}
	wiped, e := hdkeychain.NewMaster(seed, cp)
	if e != nil {
		t.Fatal(e)
	}
	wiped.Zero()
	if _, _, e := publicAt(wiped, cp, 0, 0); e == nil {
		t.Fatal("destroyed extended key generated an address")
	}
	if e := a.ValidateAddress("other", p.Recipient); e == nil {
		t.Fatal("unknown address network accepted")
	}
	scriptAddr, e := stdaddr.NewAddressScriptHashV0([]byte{0x51}, cp)
	if e != nil {
		t.Fatal(e)
	}
	if e := a.ValidateAddress(p.Account.Network, scriptAddr.String()); e == nil {
		t.Fatal("unsupported script-hash payment accepted")
	}
	if _, _, e := paymentScript(p.Account.Network, scriptAddr.String()); e == nil {
		t.Fatal("script codec accepted unsupported script-hash transfer")
	}
	if _, _, e := paymentScript(p.Account.Network, "!"); e == nil {
		t.Fatal("script codec accepted bad address")
	}
}
func TestPaymentCodecAndFeeCommitmentRejections(t *testing.T) {
	a, seed, p := testPlan(t)
	x, _ := decodePlan(p)
	for name, mutate := range map[string]func(*model.Plan, *Payload){"outpoint-hash": func(p *model.Plan, x *Payload) { x.Inputs[0].TxID = "!" }, "destination": func(p *model.Plan, x *Payload) { p.Recipient = "!" }, "amount": func(p *model.Plan, x *Payload) { p.Amount = "!" }, "change": func(p *model.Plan, x *Payload) { x.ChangeAddress = "!" }} {
		t.Run(name, func(t *testing.T) {
			q := p
			y, _ := decodePlan(p)
			mutate(&q, &y)
			if _, e := makeTx(q, y); e == nil {
				t.Fatal("malformed codec input serialized")
			}
		})
	}
	for name, mutate := range map[string]func(*model.Plan, *Payload){"descriptor": func(p *model.Plan, x *Payload) { p.Account.Parameters = []byte(`{}`) }, "dust": func(p *model.Plan, x *Payload) { p.Amount = "1" }, "max-fee-syntax": func(p *model.Plan, x *Payload) { p.MaxFee = "-1" }, "fee-provenance": func(p *model.Plan, x *Payload) { x.FeeSource = "UNKNOWN" }, "checkpoint": func(p *model.Plan, x *Payload) { x.TipHash = "!"; p.Nonce = "utxo:!" }, "destination": func(p *model.Plan, x *Payload) { p.Recipient = "!" }, "underpriced-balanced-fee": func(p *model.Plan, x *Payload) { x.Fee--; x.Change++; p.MaxFee = stringAmount(x.Fee) }, "excess-balanced-fee": func(p *model.Plan, x *Payload) { x.Fee += 10000; x.Change -= 10000; p.MaxFee = stringAmount(x.Fee) }, "fee-rate-dust": func(p *model.Plan, x *Payload) {
		x.FeeRate = 1000000
		p.Amount = "10000"
		x.Change = x.Inputs[0].Value - 10000 - x.Fee
	}} {
		t.Run(name, func(t *testing.T) {
			q := p
			y, _ := decodePlan(p)
			mutate(&q, &y)
			q.Payload, _ = json.Marshal(y)
			q.ID = planID(q)
			if e := a.ValidatePlan(q); e == nil {
				t.Fatal("changed payment policy accepted")
			}
		})
	}
	if _, e := previous(Input{Previous: "!"}); e == nil {
		t.Fatal("malformed previous transaction hex accepted")
	}
	if e := strict(make([]byte, (2<<20)+1), new(Payload)); e == nil {
		t.Fatal("oversized plan metadata accepted")
	}
	if _, e := a.Sign(seed[:32], p); e == nil {
		t.Fatal("truncated seed signed transaction")
	}
	signed, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	huge := signed
	huge.Bytes = make([]byte, (1<<20)+1)
	if _, _, e := a.validateSigned(p, huge); e == nil {
		t.Fatal("oversized signed transaction accepted")
	}
	tx, e := makeTx(p, x)
	if e != nil {
		t.Fatal(e)
	}
	badScript := x
	badScript.Inputs = append([]Input(nil), x.Inputs...)
	badScript.Inputs[0].Script = "!"
	if e := verify(tx, badScript); e == nil {
		t.Fatal("malformed verification script hex accepted")
	}
	badScript.Inputs[0].Script = strings.Repeat("51", 20000)
	if e := verify(tx, badScript); e == nil {
		t.Fatal("oversized verification script accepted")
	}
	q := p
	q.ID = "changed"
	if _, _, e := a.validateArchivedSigned(q, signed); e == nil {
		t.Fatal("unbound archived payment accepted")
	}
	if _, e := a.Reconcile(context.Background(), q, signed); e == nil {
		t.Fatal("unbound archived receipt accepted")
	}
}
func stringAmount(n int64) string { return strconv.FormatInt(n, 10) }
