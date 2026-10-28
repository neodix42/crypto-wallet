package staking

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"localwallet/internal/chains/near"
	"localwallet/internal/signer"
)

// The ordinary contract cycle has enough node validity for the wallet's
// unchanged approval lifetime. This separate real-node control retains the
// short lifetime that reproduced CI's Expired rejection, proving it cannot
// become a false success or release the original signed nonce reservation.
func TestLocalStakingExpiry(t *testing.T) {
	if os.Getenv("WALLET_NEAR_STAKING_EXPIRY_CONTROL") != "1" {
		t.Skip("NOT_RUN: use the pinned local staking fixture with --expiry-control")
	}
	ctx, endpoint, a, _, engine, account := localStakingFixture(t)
	var protocol struct {
		Validity uint64 `json:"transaction_validity_period"`
	}
	rpc(t, ctx, endpoint, "EXPERIMENTAL_protocol_config", map[string]string{"finality": "final"}, &protocol)
	if protocol.Validity != 20 {
		t.Fatal("expiry control requires its explicit 20-block local genesis")
	}
	before := liquid(t, ctx, endpoint, account.Address)
	var keyBefore struct {
		Nonce uint64 `json:"nonce"`
	}
	keyQuery := map[string]string{"request_type": "view_access_key", "finality": "final", "account_id": account.Address, "public_key": account.PublicKey}
	rpc(t, ctx, endpoint, "query", keyQuery, &keyBefore)
	p, err := a.PrepareStaking(ctx, account, near.StakingRequest{AccountID: account.ID, Validator: pool, Action: "stake", Amount: "1234567890123456789012345"})
	if err != nil {
		t.Fatal(err)
	}
	p.ID = signer.ID()
	if err := engine.Prepare(p); err != nil {
		t.Fatal(err)
	}
	signed, err := engine.Sign(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Update(p.ID, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	var prepared near.Payload
	if json.Unmarshal(p.Payload, &prepared) != nil {
		t.Fatal("invalid local expiry payload")
	}
	height, err := strconv.ParseUint(prepared.BlockHeight, 10, 64)
	if err != nil {
		t.Fatal("invalid local expiry reference height")
	}
	// Twenty-one actual local blocks exceed only this control's node lifetime.
	// Their timestamps advance about eleven seconds, below the production
	// future-head limit. No contract storage or balances are patched.
	rpc(t, ctx, endpoint, "sandbox_fast_forward", map[string]uint64{"delta_height": 21}, nil)
	deadline := time.Now().Add(20 * time.Second)
	var finalHeight uint64
	for {
		var head struct {
			Header struct {
				Height uint64 `json:"height"`
			} `json:"header"`
		}
		rpc(t, ctx, endpoint, "block", map[string]string{"finality": "final"}, &head)
		finalHeight = head.Header.Height
		if finalHeight > height+protocol.Validity {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("local expiry control failed to advance real final blocks")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := a.ValidatePlan(p); err != nil {
		t.Fatal("node-expiry control exceeded the unchanged wallet approval lifetime")
	}
	receipt, err := a.Submit(ctx, p, signed)
	if err == nil || receipt.State != "UNKNOWN" || receipt.Identity != signed.Identity {
		t.Fatal("expired local transaction was reported as accepted or lost its identity")
	}
	// Independently ask the owned node to classify the exact same signed bytes.
	// This deliberately rejected synthetic transaction is never re-signed or
	// replaced. Only the fixed expected error category is logged.
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "expiry-control", "method": "send_tx", "params": map[string]string{"signed_tx_base64": base64.StdEncoding.EncodeToString(signed.Bytes), "wait_until": "FINAL"}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var rejected struct {
		Error struct {
			Data struct {
				Execution struct {
					Invalid string `json:"InvalidTxError"`
				} `json:"TxExecutionError"`
			} `json:"data"`
		} `json:"error"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&rejected) != nil || rejected.Error.Data.Execution.Invalid != "Expired" {
		t.Fatal("independent local node did not classify the original bytes as Expired")
	}
	if err := engine.Update(p.ID, "UNKNOWN", &receipt); err != nil {
		t.Fatal(err)
	}
	engine.Lock()
	if err := engine.Unlock(); err != nil {
		t.Fatal(err)
	}
	journal := engine.Pending()
	if len(journal) != 1 || journal[0].State != "UNKNOWN" || journal[0].Plan.ID != p.ID || journal[0].Signed == nil || journal[0].Signed.Identity != signed.Identity || !bytes.Equal(journal[0].Signed.Bytes, signed.Bytes) {
		t.Fatal("restart lost the expired transaction's exact UNKNOWN reservation")
	}
	conflict, err := a.PrepareStaking(ctx, account, near.StakingRequest{AccountID: account.ID, Validator: pool, Action: "stake", Amount: "1"})
	if err != nil {
		t.Fatal(err)
	}
	conflict.ID = signer.ID()
	if err := engine.Prepare(conflict); err == nil {
		t.Fatal("an unresolved expired submission released its nonce reservation")
	}
	var keyAfter struct {
		Nonce uint64 `json:"nonce"`
	}
	rpc(t, ctx, endpoint, "query", keyQuery, &keyAfter)
	delegated := observe(t, ctx, endpoint, account.Address)
	if keyAfter.Nonce != keyBefore.Nonce || liquid(t, ctx, endpoint, account.Address).Cmp(before) != 0 || delegated.Staked != "0" || delegated.Unstaked != "0" {
		t.Fatal("expired rejected transaction changed nonce, liquid balance or delegated balance")
	}
	t.Logf("PASS actual node rejection: Expired; prepared block %d, final block %d, validity %d blocks; unchanged balance/nonce and durable exact UNKNOWN reservation", height, finalHeight, protocol.Validity)
}
