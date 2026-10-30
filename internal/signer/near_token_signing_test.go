package signer

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/near"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func nearTokenSignerPlan(account model.Account, policy model.NEP141Token, registration bool) model.Plan {
	now := time.Now().Unix()
	value, recipient, asset := "1234567", "receiver.testnet", policy.Asset.ID
	call := near.TokenCall{Policy: policy, Method: "ft_transfer", Args: `{"receiver_id":"receiver.testnet","amount":"1234567"}`, Deposit: "1", Gas: near.TokenGas}
	if registration {
		value = "1250000000000000000000"
		recipient = account.Address
		asset = account.Network + ":native"
		call.Method = "storage_deposit"
		call.Deposit = value
		call.Args = `{"account_id":"` + account.Address + `","registration_only":true}`
	}
	payload, _ := json.Marshal(near.Payload{Version: 1, BlockHash: base58.Encode(make([]byte, 32)), BlockHeight: "123", Protocol: "88", Gas: near.TokenGas, GasPrice: "1000000000", StorageReserve: "0", RecipientExists: true, Prepared: now, FeePolicy: "protocol-assessed fee; estimate reserve is not a signed fee cap", Token: &call})
	return model.Plan{ID: "token-" + call.Method, Account: account, Asset: asset, FeeAsset: account.Network + ":native", Recipient: recipient, Amount: value, MaxFee: "60000000000000000000000", Nonce: "42", Expires: now + 60, Payload: payload}
}

func importedNEARSigner(t *testing.T) (*Engine, *testUI, model.Account, model.NEP141Token) {
	t.Helper()
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, e, ui, "testnet")
	account := findAccount(t, e, "near-testnet")
	token := signerNEP141Token(account.Network)
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.ImportNEP141Token(account.ID, token); err != nil {
		t.Fatal(err)
	}
	return e, ui, account, token
}

func assertExactNEARTokenSignature(t *testing.T, plan model.Plan, signed model.Signed, token model.NEP141Token) {
	t.Helper()
	if len(signed.Bytes) < 65 {
		t.Fatal("missing real signed NEAR bytes")
	}
	raw := signed.Bytes[:len(signed.Bytes)-65]
	r := bytes.NewReader(raw)
	read := func(n int) []byte {
		out := make([]byte, n)
		if _, err := io.ReadFull(r, out); err != nil {
			t.Fatal("truncated NEAR transaction")
		}
		return out
	}
	text := func() string {
		n := binary.LittleEndian.Uint32(read(4))
		if n > uint32(r.Len()) {
			t.Fatal("invalid Borsh length")
		}
		return string(read(int(n)))
	}
	if text() != plan.Account.Address || read(1)[0] != 0 {
		t.Fatal("signed transaction changed source or key type")
	}
	key := read(32)
	if !bytes.Equal(key, base58.Decode(strings.TrimPrefix(plan.Account.PublicKey, "ed25519:"))) || binary.LittleEndian.Uint64(read(8)) != 42 || text() != token.Asset.Contract {
		t.Fatal("source key, nonce or actual token contract changed")
	}
	if !bytes.Equal(read(32), make([]byte, 32)) || binary.LittleEndian.Uint32(read(4)) != 1 || read(1)[0] != 2 {
		t.Fatal("token call changed block or action count/variant")
	}
	var payload near.Payload
	if json.Unmarshal(plan.Payload, &payload) != nil || payload.Token == nil {
		t.Fatal("invalid expected token plan")
	}
	if text() != payload.Token.Method || text() != payload.Token.Args || binary.LittleEndian.Uint64(read(8)) != 30_000_000_000_000 {
		t.Fatal("signed method, arguments or gas differ from exact approval")
	}
	deposit, _ := new(big.Int).SetString(payload.Token.Deposit, 10)
	want := deposit.FillBytes(make([]byte, 16))
	for l, h := 0, len(want)-1; l < h; l, h = l+1, h-1 {
		want[l], want[h] = want[h], want[l]
	}
	if !bytes.Equal(read(16), want) || r.Len() != 0 || signed.Bytes[len(raw)] != 0 {
		t.Fatal("attached yoctoNEAR or signature encoding changed")
	}
	digest := sha256.Sum256(raw)
	if !ed25519.Verify(key, digest[:], signed.Bytes[len(raw)+1:]) || signed.Identity != base58.Encode(digest[:]) {
		t.Fatal("independent signature or transaction identity verification failed")
	}
}

func TestNEP141TransferAndSelfRegistrationUseExactSignedCallsAndDurableJournal(t *testing.T) {
	for _, registration := range []bool{false, true} {
		t.Run(map[bool]string{false: "transfer", true: "registration"}[registration], func(t *testing.T) {
			e, ui, account, token := importedNEARSigner(t)
			plan := nearTokenSignerPlan(account, token, registration)
			if err := e.Prepare(plan); err != nil {
				t.Fatal(err)
			}
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			signed, err := e.Sign(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertExactNEARTokenSignature(t, plan, signed, token)
			approval := ui.summaries[len(ui.summaries)-1]
			for _, value := range []string{token.Asset.Contract, token.CodeHash, "decimals: 6", account.Address, "30000000000000", "NEAR protocol has no signed fee cap"} {
				if !strings.Contains(approval, value) {
					t.Fatal("exact spend approval omitted", value)
				}
			}
			if registration && (!strings.Contains(approval, "storage_deposit") || !strings.Contains(approval, "registration_only") || !strings.Contains(approval, "transfers no tokens")) {
				t.Fatal("registration approval hid native storage cost/effects")
			}
			if !registration && (!strings.Contains(approval, "ft_transfer") || !strings.Contains(approval, "1 yoctoNEAR")) {
				t.Fatal("transfer approval omitted fixed one-yocto deposit")
			}
			if _, err = e.Sign(plan.ID); err == nil {
				t.Fatal("one-use token approval replayed")
			}
			if err = e.Update(plan.ID, "SUBMITTING", nil); err != nil {
				t.Fatal(err)
			}
			if err = e.Update(plan.ID, "UNKNOWN", &model.Receipt{Identity: signed.Identity, State: "UNKNOWN"}); err != nil {
				t.Fatal(err)
			}
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatal(err)
			}
			op, err := e.Operation(plan.ID)
			if err != nil || op.State != "UNKNOWN" || op.Signed == nil || op.Signed.Identity != signed.Identity || !bytes.Equal(op.Signed.Bytes, signed.Bytes) {
				t.Fatal("restart lost unresolved original signed artifact")
			}
			replacement := nearTokenSignerPlan(account, token, !registration)
			replacement.ID = "forbidden-replacement"
			replacement.Nonce = "99"
			if e.Prepare(replacement) == nil {
				t.Fatal("unresolved token call released full-access nonce/account reservation")
			}
		})
	}
}

func TestNEP141SigningRejectsWrongPasswordCancellationAndPolicyOrPlanTampering(t *testing.T) {
	for _, mode := range []string{"password", "cancel", "lock", "plan"} {
		t.Run(mode, func(t *testing.T) {
			e, ui, account, token := importedNEARSigner(t)
			plan := nearTokenSignerPlan(account, token, false)
			if err := e.Prepare(plan); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "password":
				ui.inputs = append(ui.inputs, input("WRONG-PUBLIC-PASSWORD"))
			case "cancel":
				ui.approve = false
			case "lock":
				e.UI = &tokenLifecycleUI{testUI: ui, confirmHook: e.Lock}
			case "plan":
				e.UI = &tokenLifecycleUI{testUI: ui, inputHook: func() { e.data.Operations[0].Plan.Recipient = "attacker.testnet" }}
				ui.inputs = append(ui.inputs, input(syntheticPassword))
			}
			if signed, err := e.Sign(plan.ID); err == nil || len(signed.Bytes) != 0 {
				t.Fatal("unapproved or changed token plan released signature")
			}
			if _, err := e.Sign(plan.ID); err == nil {
				t.Fatal("cancelled or stale token authorization replayed")
			}
		})
	}
	for _, mode := range []string{"contract", "precision", "codehash", "method", "deposit", "args", "network", "native asset"} {
		t.Run(mode, func(t *testing.T) {
			e, _, account, token := importedNEARSigner(t)
			p := nearTokenSignerPlan(account, token, false)
			var d near.Payload
			_ = json.Unmarshal(p.Payload, &d)
			switch mode {
			case "contract":
				d.Token.Policy.Asset.Contract = "attacker.testnet"
			case "precision":
				d.Token.Policy.Asset.Decimals = 18
			case "codehash":
				d.Token.Policy.CodeHash = base58.Encode(bytes.Repeat([]byte{0x43}, 32))
			case "method":
				d.Token.Method = "ft_transfer_call"
			case "deposit":
				d.Token.Deposit = "2"
			case "args":
				d.Token.Args = `{"receiver_id":"attacker.testnet","amount":"1234567"}`
			case "network":
				d.Token.Policy.Network = "near-mainnet"
			case "native asset":
				p.Asset = account.Network + ":native"
			}
			p.Payload, _ = json.Marshal(d)
			if e.Prepare(p) == nil || len(e.Pending()) != 0 {
				t.Fatal("tampered token authority entered durable signing queue")
			}
		})
	}
}

func TestNEP141PhraseRecoveryPreservesAddressWithoutRestoringWatchApproval(t *testing.T) {
	e, _, account, _ := importedNEARSigner(t)
	before := e.Snapshot().Accounts
	fresh, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, fresh, ui, "testnet")
	if !bytes.Equal([]byte(findAccount(t, fresh, "near-testnet").Address), []byte(account.Address)) || !reflectAccountsEqual(before, fresh.Snapshot().Accounts) || len(fresh.NEP141TokenPolicies()) != 0 {
		t.Fatal("phrase restore changed keys or inherited local token approval")
	}
}

func reflectAccountsEqual(a, b []model.Account) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestNEP145DeniedApprovalCanAcknowledgeOnlyItsDurablyCancelledUnsignedOperation(t *testing.T) {
	for _, mode := range []string{"cancel", "password"} {
		t.Run(mode, func(t *testing.T) {
			e, ui, account, token := importedNEARSigner(t)
			plan := nearTokenSignerPlan(account, token, true)
			if err := e.Prepare(plan); err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				ui.approve = false
			} else {
				ui.inputs = append(ui.inputs, input("WRONG-PUBLIC-PASSWORD"))
			}
			if signed, err := e.Sign(plan.ID); err == nil || len(signed.Bytes) != 0 {
				t.Fatal("rejected registration released a signature")
			}
			op, err := e.Operation(plan.ID)
			if err != nil || op.State != "CANCELLED" || op.Signed != nil {
				t.Fatal("denied approval was not durably cancelled")
			}
			before, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			generation := e.data.Generation
			if err = e.CancelPrepared(plan.ID); err != nil {
				t.Fatal("original cancelled operation could not be acknowledged", err)
			}
			after, err := e.Files.Read("vault.lwv")
			if err != nil || !bytes.Equal(before, after) || e.data.Generation != generation {
				t.Fatal("idempotent cancellation rewrote vault or changed generation")
			}
			if e.CancelPrepared("foreign-operation") == nil {
				t.Fatal("foreign cancellation accepted")
			}
			e.data.Operations[0].Digest = "corrupt"
			if e.CancelPrepared(plan.ID) == nil {
				t.Fatal("corrupt cancelled journal acknowledged")
			}
			e.data.Operations[0].Digest = journal.Digest(e.data.Operations[0].Plan)
			e.data.Operations[0].Plan.Account.ID = "foreign-account"
			e.data.Operations[0].Digest = journal.Digest(e.data.Operations[0].Plan)
			if e.CancelPrepared(plan.ID) == nil {
				t.Fatal("unowned cancelled journal acknowledged")
			}
		})
	}
}
