package decred

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/wire"
	"localwallet/internal/model"
)

type vectors struct {
	Seed     string `json:"seed"`
	Accounts []struct {
		Network   string `json:"network"`
		Index     uint32 `json:"index"`
		Address   string `json:"address"`
		PublicKey string `json:"publicKey"`
		XPub      string `json:"xpub"`
	} `json:"accounts"`
	Leading struct {
		Seed            string `json:"seed"`
		ChildAddress    string `json:"childAddress"`
		StandardAddress string `json:"standardAddress"`
		PublicKey       string `json:"publicKey"`
	} `json:"leadingZero"`
}

func fixtures(t *testing.T) (vectors, []byte) {
	t.Helper()
	b, e := os.ReadFile("../../../tests/oracles/decred/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var v vectors
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	s, e := hex.DecodeString(v.Seed)
	if e != nil {
		t.Fatal(e)
	}
	return v, s
}
func TestOfficialProfilesAndLeadingZero(t *testing.T) {
	v, seed := fixtures(t)
	a := New()
	for _, row := range v.Accounts {
		t.Run(fmt.Sprintf("%s/%d", row.Network, row.Index), func(t *testing.T) {
			ac, e := a.Derive(seed, row.Index, row.Network)
			if e != nil {
				t.Fatal(e)
			}
			meta, _, e := descriptor(ac)
			if e != nil {
				t.Fatal(e)
			}
			if ac.Address != row.Address || ac.PublicKey != row.PublicKey || meta.AccountXPub != row.XPub {
				t.Fatal("official address/profile mismatch")
			}
			if e = a.ValidateAddress(row.Network, ac.Address); e != nil {
				t.Fatal(e)
			}
		})
	}
	s, _ := hex.DecodeString(v.Leading.Seed)
	ac, e := a.Derive(s, 0, "decred-mainnet")
	if e != nil {
		t.Fatal(e)
	}
	if ac.Address != v.Leading.ChildAddress || ac.PublicKey != v.Leading.PublicKey || ac.Address == v.Leading.StandardAddress {
		t.Fatal("genuine Decred Child/strict-BIP32 divergence lost")
	}
	strict, e := hdkeychain.NewMaster(s, chaincfg.MainNetParams())
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []uint32{44 + 0x80000000, 42 + 0x80000000, 0x80000000, 0, 0} {
		next, e := strict.ChildBIP32Std(n)
		strict.Zero()
		if e != nil {
			t.Fatal(e)
		}
		strict = next
	}
	defer strict.Zero()
	legacy, e := childPath(s, 0, chaincfg.MainNetParams())
	if e != nil {
		t.Fatal(e)
	}
	defer legacy.Zero()
	if bytes.Equal(strict.SerializedPubKey(), mustPub(t, legacy)) {
		t.Fatal("fixture does not actually distinguish derivation schemes")
	}
}
func mustPub(t *testing.T, k *hdkeychain.ExtendedKey) []byte {
	t.Helper()
	b, e := k.Child(0)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Zero()
	c, e := b.Child(0)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Zero()
	return c.SerializedPubKey()
}
func testPlan(t *testing.T) (*Adapter, []byte, model.Plan) {
	t.Helper()
	_, seed := fixtures(t)
	a := New()
	ac, e := a.Derive(seed, 0, "decred-simnet")
	if e != nil {
		t.Fatal(e)
	}
	dest, e := a.Derive(seed, 1, "decred-simnet")
	if e != nil {
		t.Fatal(e)
	}
	_, s, e := paymentScript(ac.Network, ac.Address)
	if e != nil {
		t.Fatal(e)
	}
	prev := wire.NewMsgTx()
	h := chainhash.Hash{1}
	prev.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&h, 1, 0), 200000000, nil))
	prev.AddTxOut(&wire.TxOut{Value: 100000000, PkScript: s})
	raw, _ := prev.Bytes()
	in := Input{TxID: prev.TxHash().String(), Value: 100000000, Script: hex.EncodeToString(s), Previous: hex.EncodeToString(raw), Height: 1}
	change, e := AddressAt(ac, 1, 0)
	if e != nil {
		t.Fatal(e)
	}
	x := Payload{Inputs: []Input{in}, ChangeAddress: change, Change: 50000000, FeeRate: 10000, FeeSource: "relay-minimum-no-estimate", TipHeight: 100, TipHash: chainhash.Hash{9}.String(), Expiry: 112}
	p := model.Plan{Account: ac, Asset: ac.Network + ":DCR", Recipient: dest.Address, Amount: "10000000", FeeAsset: ac.Network + ":DCR", Nonce: "utxo:" + x.TipHash, Expires: time.Now().Add(5 * time.Minute).Unix()}
	tx, e := makeTx(p, x)
	if e != nil {
		t.Fatal(e)
	}
	x.Fee = feeFor(signedSize(tx), x.FeeRate)
	x.Change = in.Value - 10000000 - x.Fee
	p.MaxFee = fmt.Sprint(x.Fee)
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	return a, seed, p
}
func TestRealSignaturesBothTreesAndSpendEffects(t *testing.T) {
	a, seed, p := testPlan(t)
	if e := a.ValidatePlan(p); e != nil {
		t.Fatal(e)
	}
	s, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.validateSigned(p, s); e != nil {
		t.Fatal(e)
	}
	expired := p
	expired.Expires = time.Now().Add(-time.Minute).Unix()
	expired.ID = planID(expired)
	if _, e := a.Sign(seed, expired); e == nil {
		t.Fatal("expired approval allowed a new signature")
	}
	if _, _, e := a.validateArchivedSigned(expired, s); e != nil {
		t.Fatal("exact signed-byte retransmission denied after UI deadline", e)
	}
	txWithBadSig := wire.NewMsgTx()
	if err := txWithBadSig.FromBytes(s.Bytes); err != nil {
		t.Fatal(err)
	}
	txWithBadSig.TxIn[0].SignatureScript[8] ^= 1
	badBytes, err := txWithBadSig.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, e = a.validateSigned(p, model.Signed{Identity: s.Identity, Bytes: badBytes}); e == nil {
		t.Fatal("official script engine accepted invalid signature")
	}
	x, _ := decodePlan(p)
	x.Inputs[0].Tree = wire.TxTreeStake
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	stakeSigned, err := a.Sign(seed, p)
	if err != nil {
		t.Fatal("stake tree is part of signed outpoint", err)
	}
	stakeTx := wire.NewMsgTx()
	if err = stakeTx.FromBytes(stakeSigned.Bytes); err != nil || stakeTx.TxIn[0].PreviousOutPoint.Tree != wire.TxTreeStake {
		t.Fatal("signed transaction discarded stake tree", err)
	}
	if _, e = a.Sign(seed, p); e != nil {
		t.Fatal("stake tree is part of signed outpoint", e)
	}
	for _, mutate := range []func(*model.Plan){func(q *model.Plan) { q.Recipient = p.Account.Address }, func(q *model.Plan) { q.Amount = "10000001" }, func(q *model.Plan) { q.Asset = "decred-mainnet:DCR" }, func(q *model.Plan) { q.MaxFee = "1" }} {
		q := p
		mutate(&q)
		q.ID = planID(q)
		if _, _, e = a.validateSigned(q, s); e == nil {
			t.Fatal("old signature accepted modified effects")
		}
	}
	other := append([]byte(nil), seed...)
	other[0] ^= 1
	if _, e = a.Sign(other, p); e == nil {
		t.Fatal("foreign root accepted")
	}
	s.Bytes = append(s.Bytes, 0)
	if _, _, e = a.validateSigned(p, s); e == nil {
		t.Fatal("trailing transaction garbage accepted")
	}
}
func TestAdverseInputsChangeAndFees(t *testing.T) {
	a, _, p := testPlan(t)
	mutations := map[string]func(*Payload){"duplicate": func(x *Payload) { x.Inputs = append(x.Inputs, x.Inputs[0]) }, "fake value": func(x *Payload) { x.Inputs[0].Value++ }, "fake script": func(x *Payload) { x.Inputs[0].Script = "51" }, "fake previous": func(x *Payload) { x.Inputs[0].Previous = "00" }, "foreign change": func(x *Payload) { x.ChangeAddress = p.Recipient }, "invalid tree": func(x *Payload) { x.Inputs[0].Tree = 2 }, "foreign branch": func(x *Payload) { x.Inputs[0].Branch = 1 }, "future input": func(x *Payload) { x.Inputs[0].Height = 101 }, "unbalanced": func(x *Payload) { x.Change++ }, "fee overflow": func(x *Payload) { x.FeeRate = MaxAtoms }, "dust": func(x *Payload) { x.Change = 1 }, "expiry": func(x *Payload) { x.Expiry++ }}
	mutations["balanced fake value"] = func(x *Payload) { x.Inputs[0].Value++; x.Change++ }
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			q := p
			x, _ := decodePlan(p)
			mutate(&x)
			q.Payload, _ = json.Marshal(x)
			q.ID = planID(q)
			if e := a.ValidatePlan(q); e == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	q := p
	x, _ := decodePlan(q)
	prev := wire.NewMsgTx()
	prev.AddTxIn(wire.NewTxIn(wire.NewOutPoint(new(chainhash.Hash), ^uint32(0), wire.TxTreeRegular), 0, nil))
	script, _ := hex.DecodeString(x.Inputs[0].Script)
	prev.AddTxOut(&wire.TxOut{Value: x.Inputs[0].Value, PkScript: script})
	raw, _ := prev.Bytes()
	x.Inputs[0].TxID = prev.TxHash().String()
	x.Inputs[0].Previous = hex.EncodeToString(raw)
	x.Inputs[0].Height = 100
	q.Payload, _ = json.Marshal(x)
	q.ID = planID(q)
	if e := a.ValidatePlan(q); e == nil {
		t.Fatal("immature coinbase accepted")
	}
	q = p
	q.Payload = []byte(`{"fee":1,"fee":2}`)
	q.ID = planID(q)
	if e := a.ValidatePlan(q); e == nil {
		t.Fatal("duplicate JSON accepted")
	}
}
func TestRecoveryAddressSequenceAndNetwork(t *testing.T) {
	a, seed, p := testPlan(t)
	ac, e := AdvanceReceive(p.Account)
	if e != nil || ac.Address == p.Account.Address {
		t.Fatal("receive sequence did not advance", e)
	}
	addr, e := AddressAt(p.Account, 0, 1)
	if e != nil || addr != ac.Address {
		t.Fatal("receive recovery mismatch")
	}
	change, e := AdvanceChange(ac)
	if e != nil {
		t.Fatal(e)
	}
	meta, _, _ := descriptor(change)
	if meta.ChangeIndex != 1 || change.Address != ac.Address {
		t.Fatal("change counter altered receive descriptor")
	}
	if _, e := a.Derive(seed[:32], 0, p.Account.Network); e == nil {
		t.Fatal("truncated seed accepted")
	}
	if _, e := a.Derive(seed, maxIndex, p.Account.Network); e == nil {
		t.Fatal("unbounded account accepted")
	}
	for _, address := range []string{"", p.Recipient + "!", strings.Repeat("a", 1000)} {
		if e := a.ValidateAddress(p.Account.Network, address); e == nil {
			t.Fatal("invalid address accepted")
		}
	}
	if e := a.ValidateAddress("decred-mainnet", p.Account.Address); e == nil {
		t.Fatal("wrong network address accepted")
	}
	if e := a.Configure("decred-mainnet", Endpoint{URL: "http://example.com", User: "public-test", Password: "public-test"}); e == nil {
		t.Fatal("plaintext remote RPC accepted")
	}
}
func TestDcrdSimnet(t *testing.T) {
	bin := os.Getenv("WALLET_DCRD")
	if bin == "" {
		t.Skip("NOT_RUN: WALLET_DCRD must point to pinned dcrd v1.10.9 executable")
	}
	a, seed, p := testPlan(t)
	dir := t.TempDir()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	config := fmt.Sprintf("simnet=1\nnotls=1\nrpcuser=public-simnet-fixture\nrpcpass=public-simnet-fixture-only\nrpclisten=127.0.0.1:%d\nnolisten=1\nconnect=127.0.0.1:1\nnodnsseed=1\nminingaddr=%s\nallowunsyncedmining=1\nnominingstatesync=1\ntxindex=1\nnofilelogging=1\ndebuglevel=error\n", port, p.Account.Address)
	configPath := filepath.Join(dir, "dcrd.conf")
	if e = os.WriteFile(configPath, []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(bin, "--configfile="+configPath, "--appdata="+dir, "--datadir="+filepath.Join(dir, "data"))
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	if e = a.Configure("decred-simnet", Endpoint{URL: fmt.Sprintf("http://127.0.0.1:%d", port), User: "public-simnet-fixture", Password: "public-simnet-fixture-only"}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ready := false
	for i := 0; i < 100; i++ {
		var count int
		if a.rpc(ctx, p.Account.Network, "getblockcount", []any{}, &count) == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("dcrd did not start: %s", logs.String())
	}
	var hashes []string
	if e = a.rpc(ctx, p.Account.Network, "generate", []any{24}, &hashes); e != nil {
		t.Fatalf("simnet mining: %v logs=%s", e, logs.String())
	}
	report, e := a.Sync(ctx, p.Account)
	if e != nil {
		t.Fatalf("sync: %v logs=%s", e, logs.String())
	}
	if len(report.Spendable) == 0 || report.ImmatureAtoms == 0 {
		t.Fatal("coinbase maturity/spendability not discovered")
	}
	prepared, e := a.Prepare(ctx, p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: "10000000"})
	if e != nil {
		t.Fatal("prepare", e)
	}
	signed, e := a.Sign(seed, prepared)
	if e != nil {
		t.Fatal("sign", e)
	}
	receipt, e := a.Submit(ctx, prepared, signed)
	if e != nil || receipt.State != "SUBMITTED" {
		t.Fatalf("submit %v %+v logs=%s", e, receipt, logs.String())
	}
	if e = a.rpc(ctx, p.Account.Network, "generate", []any{8}, &hashes); e != nil {
		t.Fatal(e)
	}
	receipt, e = a.Reconcile(ctx, prepared, signed)
	if e != nil || receipt.State != "CONFIRMED" {
		t.Fatalf("reconcile %v %+v", e, receipt)
	}
	recipient, e := a.Derive(seed, 1, p.Account.Network)
	if e != nil {
		t.Fatal(e)
	}
	balances, e := a.Balance(ctx, recipient)
	if e != nil || balances[0].Atomic != "10000000" {
		t.Fatal("independent recipient state", e, balances)
	}
	var creation struct {
		Vout []struct {
			Value json.Number `json:"value"`
		} `json:"vout"`
		Confirmations int64 `json:"confirmations"`
	}
	if e = a.rpc(ctx, p.Account.Network, "getrawtransaction", []any{signed.Identity, 1}, &creation); e != nil || len(creation.Vout) < 1 || creation.Vout[0].Value.String() != "0.1" || creation.Confirmations < 6 {
		t.Fatal("independent node output evidence failed", e, creation)
	}
	t.Logf("PASS: dcrd v1.10.9 simnet accepted %s; %d confirmations; recipient independently has 10,000,000 atoms", signed.Identity, creation.Confirmations)
	if os.Getenv("WALLET_EVIDENCE_DIR") != "" {
		evidence := map[string]any{"status": "PASS", "node": "dcrd v1.10.9", "network": "simnet", "identity": signed.Identity, "confirmations": creation.Confirmations, "recipientAtoms": balances[0].Atomic, "matureInputs": len(report.Spendable), "immatureAtoms": report.ImmatureAtoms}
		raw, _ := json.MarshalIndent(evidence, "", "  ")
		if e = os.WriteFile(filepath.Join(os.Getenv("WALLET_EVIDENCE_DIR"), "decred-simnet.json"), append(raw, '\n'), 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func FuzzAddressesScriptsOutpoints(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("SsZwj6zrYrAb2oQoWR94P4wX7zrLd4UsnXw"))
	f.Add([]byte{1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		_ = New().ValidateAddress("decred-simnet", string(b))
		_, _ = previous(Input{Previous: hex.EncodeToString(b)})
		var x Payload
		_ = strict(b, &x)
	})
}
