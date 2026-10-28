// Package staking exercises real, disposable local-node contract execution.
// Every key and password in this test is a PUBLIC SYNTHETIC fixture.
package staking

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/near"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
const password = "PUBLIC-local-staking-fixture-password-9281"
const pool = "pool.sandbox"

type localAdapter struct{ *near.Adapter }

func (a localAdapter) Networks() []model.Network {
	for _, n := range a.Adapter.Networks() {
		if n.ID == "near-localnet" {
			return []model.Network{n}
		}
	}
	return nil
}

// This test implements the authorized prompt responses only. It never replaces
// transaction construction, password verification, signing, storage or RPC.
type approvals struct {
	first     bool
	allow     bool
	summaries []string
}

func (a *approvals) Input(_ string, _ ...string) ([][]byte, error) {
	if !a.first {
		a.first = true
		return [][]byte{[]byte(password), []byte(password), []byte(phrase), {}}, nil
	}
	return [][]byte{[]byte(password)}, nil
}
func (a *approvals) Confirm(title, body string) bool {
	if strings.Contains(title, "staking operation") {
		a.summaries = append(a.summaries, body)
		return a.allow
	}
	return true // Importing this explicit published fixture is acknowledged.
}
func (*approvals) File(string, bool) (string, error)   { return "", errors.New("unused fixture UI") }
func (*approvals) Receive(string, string, []byte) bool { return false }

func rpc(t *testing.T, ctx context.Context, endpoint, method string, params any, result any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "independent", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&envelope) != nil || len(envelope.Error) != 0 {
		t.Fatalf("independent local RPC %s failed (no secret-bearing response logged)", method)
	}
	if result != nil && json.Unmarshal(envelope.Result, result) != nil {
		t.Fatal("invalid independent RPC result")
	}
}

type position struct {
	Account     string `json:"account_id"`
	Staked      string `json:"staked_balance"`
	Unstaked    string `json:"unstaked_balance"`
	CanWithdraw bool   `json:"can_withdraw"`
}

func observe(t *testing.T, ctx context.Context, endpoint, account string) position {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"account_id": account})
	var result struct {
		Result []uint8 `json:"result"`
	}
	rpc(t, ctx, endpoint, "query", map[string]string{"request_type": "call_function", "finality": "final", "account_id": pool, "method_name": "get_account", "args_base64": base64.StdEncoding.EncodeToString(args)}, &result)
	var p position
	if json.Unmarshal(result.Result, &p) != nil || p.Account != account {
		t.Fatal("foreign pool observation")
	}
	return p
}

func number(t *testing.T, value string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() < 0 {
		t.Fatal("invalid independent amount")
	}
	return n
}

func liquid(t *testing.T, ctx context.Context, endpoint, account string) *big.Int {
	t.Helper()
	var result struct {
		Amount string `json:"amount"`
	}
	rpc(t, ctx, endpoint, "query", map[string]string{"request_type": "view_account", "finality": "final", "account_id": account}, &result)
	return number(t, result.Amount)
}

func localStakingFixture(t *testing.T) (context.Context, string, localAdapter, *approvals, *signer.Engine, model.Account) {
	t.Helper()
	endpoint := os.Getenv("WALLET_NEAR_STAKING_LOCAL_RPC")
	if endpoint == "" {
		t.Skip("NOT_RUN: use tests/oracles/near/run-staking-local.mjs with the pinned real sandbox and contract")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" {
		t.Fatal("owned loopback sandbox only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	var status struct {
		Chain string `json:"chain_id"`
	}
	rpc(t, ctx, endpoint, "status", []any{}, &status)
	if status.Chain != "localnet" {
		t.Fatal("refusing non-localnet chain")
	}
	a := localAdapter{near.New()}
	if err := a.SetEndpoint("near-localnet", endpoint); err != nil {
		t.Fatal(err)
	}
	files, err := vault.NewFiles(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	ui := &approvals{allow: true}
	engine := signer.New(files, ui, []model.Adapter{a})
	t.Cleanup(engine.Lock)
	if err := engine.Create("testnet", 12, true); err != nil {
		t.Fatal(err)
	}
	accounts := engine.Snapshot().Accounts
	if len(accounts) != 1 || accounts[0].Network != "near-localnet" {
		t.Fatal("fixture escaped the local network")
	}
	return ctx, endpoint, a, ui, engine, accounts[0]
}

func TestLocalStakingCycle(t *testing.T) {
	ctx, endpoint, a, ui, engine, account := localStakingFixture(t)
	var protocol struct {
		EpochLength uint64 `json:"epoch_length"`
		Validity    uint64 `json:"transaction_validity_period"`
	}
	rpc(t, ctx, endpoint, "EXPERIMENTAL_protocol_config", map[string]string{"finality": "final"}, &protocol)
	if protocol.EpochLength != 64 || protocol.Validity != 128 {
		t.Fatal("ordinary staking cycle requires its approval-lifetime-aligned local genesis")
	}
	initial := observe(t, ctx, endpoint, account.Address)
	if initial.Staked != "0" || initial.Unstaked != "0" {
		t.Fatal("fixture is not initially empty")
	}
	before := liquid(t, ctx, endpoint, account.Address)
	amount := "1234567890123456789012345"
	request := func(action, atomic string) model.Plan {
		t.Helper()
		p, err := a.PrepareStaking(ctx, account, near.StakingRequest{AccountID: account.ID, Validator: pool, Action: action, Amount: atomic})
		if err != nil {
			t.Fatal(err)
		}
		p.ID = signer.ID()
		return p
	}
	// Cancellation cannot move funds, reserve a future nonce indefinitely or sign.
	declined := request("stake", amount)
	if engine.Prepare(declined) != nil {
		t.Fatal("prepare declined fixture")
	}
	ui.allow = false
	if signed, err := engine.Sign(declined.ID); err == nil || len(signed.Bytes) != 0 {
		t.Fatal("declined staking signed")
	}
	ui.allow = true
	if got := observe(t, ctx, endpoint, account.Address); got.Staked != "0" {
		t.Fatal("cancelled staking moved funds")
	}

	execute := func(action, atomic, method string, lostAck bool) model.Plan {
		t.Helper()
		p := request(action, atomic)
		if err := engine.Prepare(p); err != nil {
			t.Fatal(err)
		}
		signed, err := engine.Sign(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		last := ui.summaries[len(ui.summaries)-1]
		for _, detail := range []string{pool, method, near.StakingGas, account.Address} {
			if !strings.Contains(last, detail) {
				t.Fatalf("approval omitted %s", detail)
			}
		}
		if _, err := engine.Sign(p.ID); err == nil {
			t.Fatal("consumed approval signed twice")
		}
		if err := engine.Update(p.ID, "SUBMITTING", nil); err != nil {
			t.Fatal(err)
		}
		var prepared near.Payload
		if json.Unmarshal(p.Payload, &prepared) != nil {
			t.Fatal("invalid local staking fixture payload")
		}
		var head struct {
			Header struct {
				Height uint64 `json:"height"`
			} `json:"header"`
		}
		rpc(t, ctx, endpoint, "block", map[string]string{"finality": "final"}, &head)
		t.Logf("Local %s submission: prepared block %s, current final block %d", action, prepared.BlockHeight, head.Header.Height)
		receipt, err := a.Submit(ctx, p, signed)
		if err != nil || receipt.State != "CONFIRMED" {
			t.Fatalf("%s staking execution state %s: %v", action, receipt.State, err)
		}
		if lostAck {
			engine.Lock()
			if err := engine.Unlock(); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, op := range engine.Pending() {
				if op.Plan.ID == p.ID {
					found = op.State == "UNKNOWN" && op.Signed != nil && bytes.Equal(op.Signed.Bytes, signed.Bytes) && op.Signed.Identity == signed.Identity
				}
			}
			if !found {
				t.Fatal("restart lost exact signed bytes or UNKNOWN reservation")
			}
			conflict := request("stake", "1")
			if err := engine.Prepare(conflict); err == nil {
				t.Fatal("unknown transaction did not reserve account")
			}
			receipt, err = a.Reconcile(ctx, p, signed)
			if err != nil || receipt.Identity != signed.Identity || receipt.State != "CONFIRMED" {
				t.Fatal("original transaction did not reconcile")
			}
		}
		if err := engine.Update(p.ID, receipt.State, &receipt); err != nil {
			t.Fatal(err)
		}
		return p
	}

	execute("stake", amount, "deposit_and_stake", true)
	staked := observe(t, ctx, endpoint, account.Address)
	// The upstream pool rounds share purchases and can leave a yoctoNEAR in
	// unstaked balance. Check conserved value, rather than inventing a rule
	// that deposit_and_stake consumes every last atomic unit as shares.
	credited := new(big.Int).Add(number(t, staked.Staked), number(t, staked.Unstaked))
	if number(t, staked.Staked).Sign() <= 0 || credited.Cmp(number(t, amount)) < 0 {
		t.Fatalf("independent pool state did not credit stake: %+v", staked)
	}
	afterStake := liquid(t, ctx, endpoint, account.Address)
	if new(big.Int).Sub(before, afterStake).Cmp(number(t, amount)) <= 0 {
		t.Fatal("deposit and gas were not debited from liquid wallet")
	}

	partial := "234567890123456789012345"
	execute("unstake", partial, "unstake", false)
	unstaking := observe(t, ctx, endpoint, account.Address)
	if number(t, unstaking.Unstaked).Cmp(number(t, partial)) < 0 || number(t, unstaking.Staked).Sign() == 0 || unstaking.CanWithdraw {
		t.Fatal("partial unstaking did not retain a locked remainder")
	}
	if _, err := a.PrepareStaking(ctx, account, near.StakingRequest{AccountID: account.ID, Validator: pool, Action: "withdraw_all"}); err == nil {
		t.Fatal("early withdrawal prepared before contract unlock")
	}
	execute("unstake_all", "", "unstake_all", false)
	all := observe(t, ctx, endpoint, account.Address)
	if all.Staked != "0" || number(t, all.Unstaked).Sign() == 0 || all.CanWithdraw {
		t.Fatal("unstake_all state does not match contract effects")
	}
	// Sandbox fast-forward adds a permanent timestamp offset of 500 ms per
	// skipped block. Bound it to 96 blocks (at most 48 seconds), leaving margin
	// inside the unchanged production one-minute future-head guard. Wait for
	// the remaining actual epochs; never patch contract storage or balances.
	rpc(t, ctx, endpoint, "sandbox_fast_forward", map[string]uint64{"delta_height": protocol.EpochLength * 3 / 2}, nil)
	// Allow five ordinary epochs at 500 ms/block plus a 20-second finality
	// margin. This is a bounded contract wait, not a transaction/RPC retry.
	deadline := time.Now().Add(time.Duration(5*protocol.EpochLength)*500*time.Millisecond + 20*time.Second)
	for {
		all = observe(t, ctx, endpoint, account.Address)
		if all.CanWithdraw {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pool did not unlock after four local epochs")
		}
		time.Sleep(500 * time.Millisecond)
	}
	beforeWithdraw := liquid(t, ctx, endpoint, account.Address)
	withdraw := execute("withdraw_all", "", "withdraw_all", false)
	final := observe(t, ctx, endpoint, account.Address)
	if final.Staked != "0" || final.Unstaked != "0" {
		t.Fatal("withdraw_all left a delegated balance")
	}
	afterWithdraw := liquid(t, ctx, endpoint, account.Address)
	credit := new(big.Int).Sub(afterWithdraw, beforeWithdraw)
	minimum := new(big.Int).Sub(number(t, all.Unstaked), number(t, withdraw.MaxFee))
	if credit.Sign() <= 0 || credit.Cmp(minimum) < 0 {
		t.Fatal("withdrawal failed to return independently observed NEAR less bounded fees")
	}
	engine.Lock()
	if err := engine.Unlock(); err != nil {
		t.Fatal(err)
	}
	confirmed := 0
	for _, op := range engine.Pending() {
		if op.State == "CONFIRMED" {
			confirmed++
		}
	}
	if confirmed != 4 {
		t.Fatalf("durable confirmed operations: %d", confirmed)
	}
	t.Log(fmt.Sprintf("PASS actual contract cycle: stake, partial/all unstake, epoch-gated withdraw; four durable confirmations; cancellation, one-use approval, lost-ack restart and exact-hash reconciliation. Liquid returned %s yoctoNEAR.", credit))
}
