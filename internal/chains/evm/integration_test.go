package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"localwallet/internal/model"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"
)

// independentRPC deliberately uses neither the adapter RPC implementation nor its
// receipt interpretation. This confirms recipient state from actual EVM execution.
func independentRPC(t *testing.T, endpoint, method string, params any, out any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 17, "method": method, "params": params})
	r, e := http.Post(endpoint, "application/json", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if e = json.Unmarshal(raw, &response); e != nil || response.Error != nil {
		t.Fatal("independent node query failed", e, response.Error)
	}
	if e = json.Unmarshal(response.Result, out); e != nil {
		t.Fatal(e)
	}
}
func TestTP050LocalEVMProtocolAcceptance(t *testing.T) {
	endpoint := os.Getenv("LOCALWALLET_EVM_NODE")
	if endpoint == "" {
		t.Skip("NOT_RUN: set LOCALWALLET_EVM_NODE to explicit loopback test node")
	}
	if e := ValidateEndpoint(endpoint); e != nil {
		t.Fatal(e)
	}
	net := os.Getenv("LOCALWALLET_EVM_NETWORK")
	if net == "" {
		net = "ethereum-sepolia"
	}
	n, e := network(net)
	if e != nil || !n.Testnet {
		t.Fatal("integration requires allowlisted test network")
	}
	a, e := NewWithEndpoints(map[string]string{net: endpoint})
	if e != nil {
		t.Fatal(e)
	}
	source, _ := a.Derive(syntheticSeed(), 0, net)
	recipient, _ := a.Derive(syntheticSeed(), 1, net)
	amount := "100000000000000000"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	records := []map[string]any{}
	send := func(from, to model.Account, asset, value string) model.Signed {
		p, e := a.Prepare(ctx, from, model.Transfer{AccountID: from.ID, Asset: asset, Recipient: to.Address, Amount: value})
		if e != nil {
			t.Fatal(e)
		}
		s, e := a.Sign(syntheticSeed(), p)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = a.Submit(ctx, p, s); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 100; i++ {
			var r *map[string]json.RawMessage
			independentRPC(t, endpoint, "eth_getTransactionReceipt", []any{s.Identity}, &r)
			if r != nil {
				if string((*r)["status"]) != `"0x1"` {
					t.Fatal("node execution reverted")
				}
				records = append(records, map[string]any{"identity": s.Identity, "from": from.Address, "to": to.Address, "asset": asset, "atomicAmount": value, "receipt": r})
				return s
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("local node failed to mine transaction")
		return s
	}
	var before string
	independentRPC(t, endpoint, "eth_getBalance", []any{recipient.Address, "latest"}, &before)
	initial, _ := hexInteger(before)
	send(source, recipient, n.Assets[0].ID, amount)
	var after string
	independentRPC(t, endpoint, "eth_getBalance", []any{recipient.Address, "latest"}, &after)
	received, _ := hexInteger(after)
	delta := new(big.Int).Sub(received, initial)
	if delta.String() != amount {
		t.Fatal("independently queried recipient balance did not receive exact native value")
	}
	last := send(recipient, source, n.Assets[0].ID, "50000000000000000")
	if len(n.Assets) > 1 {
		var balance string
		// Explicit recipient encoding is performed independently of transferData.
		recipientWord := "0x70a08231000000000000000000000000" + recipient.Address[2:]
		independentRPC(t, endpoint, "eth_call", []any{map[string]string{"to": n.Assets[1].Contract, "data": recipientWord}, "latest"}, &balance)
		before, e := abiInteger(balance)
		if e != nil {
			t.Fatal(e)
		}
		send(source, recipient, n.Assets[1].ID, amount)
		independentRPC(t, endpoint, "eth_call", []any{map[string]string{"to": n.Assets[1].Contract, "data": recipientWord}, "latest"}, &balance)
		v, e := abiInteger(balance)
		if e != nil || new(big.Int).Sub(v, before).String() != amount {
			t.Fatal("recipient token storage does not show exact transfer", e)
		}
		last = send(recipient, source, n.Assets[1].ID, "50000000000000000")
	}
	history, e := a.History(ctx, source)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, r := range history {
		if r.Identity == last.Identity {
			found = true
		}
	}
	if !found {
		t.Fatal("actual incoming transfer absent from bounded node history")
	}
	if path := os.Getenv("LOCALWALLET_EVM_EVIDENCE"); path != "" {
		b, e := json.MarshalIndent(map[string]any{"status": "PASS", "network": net, "endpoint": endpoint, "localSyntheticFundsOnly": true, "observedUTC": time.Now().UTC().Format(time.RFC3339), "source": source, "recipient": recipient, "nativeRecipientBalanceBefore": initial.String(), "nativeRecipientBalanceAfterFunding": received.String(), "transfers": records, "history": history}, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, append(b, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
