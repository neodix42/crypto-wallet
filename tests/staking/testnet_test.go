package staking

// This opt-in harness uses only a new OS-random testnet root. Unlike local
// oracle tests it NEVER imports or funds the committed public mnemonic.
// The stable test-only password protects only this disposable testnet fixture.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

const publicTestnetRPC = "https://rpc.testnet.near.org"
const publicTestnetPool = "aurora.pool.f863973.m0"
const publicTestnetStake = "10000000000000000000000"      // 0.01 NEAR, never mainnet.
const publicTestnetEffectCap = "100000000000000000000000" // 0.1 NEAR effect cap.
const publicTestnetFeeCap = "1000000000000000000000000"   // <1 testnet NEAR cumulative fee reserve.
const testnetFixturePassword = "PUBLIC-TESTNET-ONLY-disposable-staking-fixture-928641"
const testnetOwnerMarker = `{"fixture":"cryptowallet-near-staking-public-testnet-v1","network":"testnet","pool":"aurora.pool.f863973.m0","disposable":true}`

type publicTestnetAdapter struct{ *near.Adapter }

func (a publicTestnetAdapter) Networks() []model.Network {
	for _, n := range a.Adapter.Networks() {
		if n.ID == "near-testnet" {
			return []model.Network{n}
		}
	}
	return nil
}

type testnetPrompts struct {
	method  string
	account string
}

func (*testnetPrompts) Input(title string, labels ...string) ([][]byte, error) {
	if title == "Create local wallet — set app password" && len(labels) == 2 {
		return [][]byte{[]byte(testnetFixturePassword), []byte(testnetFixturePassword)}, nil
	}
	if len(labels) == 1 && (title == "Unlock local wallet" || title == "Complete recovery setup" || title == "Reauthenticate this NEAR staking operation") {
		return [][]byte{[]byte(testnetFixturePassword)}, nil
	}
	return nil, errors.New("unexpected test-only private input")
}

func (*testnetPrompts) Recovery(_ string, words []byte) bool {
	// Phrase lifetime is confined to the Engine's private callback. Nothing is
	// copied into test reports, files, environment or command arguments.
	return len(bytes.Fields(words)) == 12
}

func (u *testnetPrompts) Confirm(title, details string) bool {
	if title == "Associate verified public account" {
		return strings.Contains(details, "Network: near-testnet\n") && strings.Contains(details, "Verified account: "+u.account+"\n")
	}
	if title != "Authorize NEAR staking operation — Go signer" || u.method == "" {
		return false
	}
	for _, expected := range []string{"Network: near-testnet\n", "Source: " + u.account + "\n", "Destination: " + publicTestnetPool + "\n", "Method: " + u.method + "\n", "Prepaid gas: " + near.StakingGas + " gas"} {
		if !strings.Contains(details, expected) {
			return false
		}
	}
	return !strings.Contains(details, "near-mainnet")
}

func (*testnetPrompts) File(string, bool) (string, error) {
	return "", errors.New("testnet fixture has no file prompt")
}
func (*testnetPrompts) Receive(string, string, []byte) bool { return false }

func testnetHTTP() *chainutil.Client {
	client := chainutil.NewClient()
	client.HTTP.Transport = &http.Transport{Proxy: nil}
	return client
}

func testnetCall(ctx context.Context, method string, params any, out any) error {
	// No caller-selected endpoint; neither this helper nor the harness accepts
	// a mainnet URL, existing profile, phrase, password or private key.
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "staking-testnet", "method": method, "params": params})
	var envelope struct {
		Version string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := testnetHTTP().Do(ctx, "POST", publicTestnetRPC, "application/json", body, &envelope); err != nil {
		return err
	}
	if envelope.Version != "2.0" || envelope.ID != "staking-testnet" || len(envelope.Error) != 0 || len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) {
		return errors.New("public testnet RPC rejected or returned an ambiguous result")
	}
	if out != nil && json.Unmarshal(envelope.Result, out) != nil {
		return errors.New("malformed public testnet result")
	}
	return nil
}

func testnetIdentity(ctx context.Context) error {
	var status struct {
		Chain string `json:"chain_id"`
		Sync  struct {
			Syncing bool   `json:"syncing"`
			Time    string `json:"latest_block_time"`
		} `json:"sync_info"`
	}
	if err := testnetCall(ctx, "status", []any{}, &status); err != nil {
		return err
	}
	when, err := time.Parse(time.RFC3339Nano, status.Sync.Time)
	if err != nil || status.Chain != "testnet" || status.Sync.Syncing || time.Since(when) > 2*time.Minute || time.Until(when) > time.Minute {
		return errors.New("refusing non-testnet or stale/synchronizing public ledger")
	}
	return nil
}

func testnetPosition(ctx context.Context, account string) (near.StakingState, error) {
	args, _ := json.Marshal(map[string]string{"account_id": account})
	var result struct {
		Bytes  []uint8     `json:"result"`
		Hash   string      `json:"block_hash"`
		Height json.Number `json:"block_height"`
	}
	if err := testnetCall(ctx, "query", map[string]string{"request_type": "call_function", "finality": "final", "account_id": publicTestnetPool, "method_name": "get_account", "args_base64": base64.StdEncoding.EncodeToString(args)}, &result); err != nil {
		return near.StakingState{}, err
	}
	var position struct {
		Account     string `json:"account_id"`
		Staked      string `json:"staked_balance"`
		Unstaked    string `json:"unstaked_balance"`
		CanWithdraw *bool  `json:"can_withdraw"`
	}
	if len(result.Bytes) > 1024 || chainutil.DecodeStrict(result.Bytes, &position) != nil || position.Account != account || position.CanWithdraw == nil || result.Hash == "" {
		return near.StakingState{}, errors.New("malformed testnet pool account observation")
	}
	if _, err := chainutil.Big(position.Staked, 128); err != nil {
		return near.StakingState{}, err
	}
	if _, err := chainutil.Big(position.Unstaked, 128); err != nil {
		return near.StakingState{}, err
	}
	if _, err := chainutil.Uint(result.Height.String(), 64); err != nil {
		return near.StakingState{}, err
	}
	return near.StakingState{AccountID: account, Validator: publicTestnetPool, Staked: position.Staked, Unstaked: position.Unstaked, CanWithdraw: *position.CanWithdraw, BlockHash: result.Hash, BlockHeight: result.Height.String()}, nil
}

func testnetLiquid(ctx context.Context, account string) (*big.Int, error) {
	var result struct {
		Amount string `json:"amount"`
	}
	if err := testnetCall(ctx, "query", map[string]string{"request_type": "view_account", "finality": "final", "account_id": account}, &result); err != nil {
		return nil, err
	}
	return chainutil.Big(result.Amount, 128)
}

type testnetWithdrawalObservation struct {
	Account      string `json:"account"`
	Operation    string `json:"operation"`
	BeforeLiquid string `json:"beforeLiquid"`
	Unstaked     string `json:"unstaked"`
}

func testnetReport(t *testing.T, name string, value map[string]any) {
	t.Helper()
	value["network"] = "testnet"
	value["rpc"] = publicTestnetRPC
	value["pool"] = publicTestnetPool
	value["recordedAt"] = time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join("..", "..", "reports", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal("cannot create public testnet report directory")
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil || os.WriteFile(path, append(body, '\n'), 0600) != nil {
		t.Fatal("cannot write public testnet evidence")
	}
}

func TestPublicTestnetStakingReadOnly(t *testing.T) {
	if os.Getenv("WALLET_NEAR_STAKING_TESTNET") != "1" {
		t.Skip("NOT_RUN: public testnet requests require WALLET_NEAR_STAKING_TESTNET=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	report := map[string]any{"status": "BLOCKED", "scope": "Read-only testnet identity and standard get_account schema; no signing or funding"}
	defer testnetReport(t, "near-staking-testnet-readonly.json", report)
	if err := testnetIdentity(ctx); err != nil {
		report["reason"] = err.Error()
		t.Skip("BLOCKED: public testnet identity unavailable")
	}
	state, err := testnetPosition(ctx, "aa")
	if err != nil {
		report["reason"] = err.Error()
		t.Skip("BLOCKED: public testnet staking-pool query unavailable")
	}
	report["status"], report["observation"] = "PASS", state
	t.Log("PASS: testnet chain identity and actual pool get_account query verified; no funds moved")
}

func openTestnetFixture(t *testing.T) *vault.Files {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", ".tools", "near-staking-testnet"))
	if err != nil {
		t.Fatal("invalid fixed testnet fixture path")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal("cannot create the ignored disposable test-tool directory")
	}
	_, err = os.Lstat(path)
	fresh := os.IsNotExist(err)
	if fresh {
		if err = os.Mkdir(path, 0700); err != nil {
			t.Fatal("cannot create disposable testnet fixture")
		}
	} else if err != nil {
		t.Fatal("cannot inspect disposable testnet fixture")
	}
	files, err := vault.NewFiles(path)
	if err != nil {
		t.Fatal("unsafe or busy disposable testnet fixture")
	}
	marker, err := files.Read("test-fixture-owner.json")
	if fresh {
		if !errors.Is(err, os.ErrNotExist) || files.Write("test-fixture-owner.json", []byte(testnetOwnerMarker), false) != nil {
			files.Close()
			t.Fatal("cannot bind new disposable fixture")
		}
	} else if err != nil || string(marker) != testnetOwnerMarker {
		files.Close()
		t.Fatal("refusing a directory not owned by this explicit disposable testnet harness")
	}
	return files
}

func TestPublicTestnetStakingResumableCycle(t *testing.T) {
	if os.Getenv("WALLET_NEAR_STAKING_TESTNET") != "1" {
		t.Skip("NOT_RUN: capped disposable public testnet writes require WALLET_NEAR_STAKING_TESTNET=1")
	}
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		t.Fatal("cannot disable fixture core dumps")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	report := map[string]any{"status": "BLOCKED", "scope": "Fresh OS-random disposable testnet root, actual signer/journal, capped 0.01 NEAR stake, unstake_all, separately resumed epoch-gated withdraw_all", "stakeNEAR": "0.01", "stakingEffectCapNEAR": "0.1", "feeReserveCapNEAR": "1", "feePolicy": "Cumulative maximum-prepaid-gas estimate reserves, not actual spending or a protocol-signed fee ceiling", "notRun": []string{"complete funded testnet cycle", "mainnet spending", "graphical staking approval"}}
	defer func() {
		if t.Failed() {
			report["status"] = "FAIL"
		}
		testnetReport(t, "near-staking-testnet.json", report)
	}()
	blocked := func(reason string) { report["reason"] = reason; t.Skip("BLOCKED: " + reason) }
	if err := testnetIdentity(ctx); err != nil {
		blocked("verified testnet ledger unavailable before creating/funding any fixture")
	}
	if _, err := testnetPosition(ctx, "aa"); err != nil {
		blocked("reviewed testnet pool unavailable before any funding")
	}
	files := openTestnetFixture(t)
	defer files.Close()
	adapter := publicTestnetAdapter{near.New()}
	ui := &testnetPrompts{}
	engine := signer.New(files, ui, []model.Adapter{adapter})
	engine.Environment = "testnet"
	defer engine.Lock()
	exists, err := files.Exists("vault.lwv")
	if err != nil {
		t.Fatal("cannot inspect disposable encrypted testnet vault")
	}
	if exists {
		err = engine.Unlock()
	} else {
		err = engine.Create("testnet", 12, false)
	}
	if err != nil {
		t.Fatal("disposable testnet vault creation/unlock failed")
	}
	if !engine.Snapshot().RecoveryConfirmed {
		if engine.ConfirmRecovery() != nil {
			t.Fatal("cannot resume private fixture recovery acknowledgment")
		}
	}
	accounts := engine.Snapshot().Accounts
	if len(accounts) != 1 || accounts[0].Network != "near-testnet" || engine.Snapshot().Environment != "testnet" {
		t.Fatal("fixture contains an unexpected wallet or network")
	}
	account := accounts[0]
	var descriptor near.Parameters
	if chainutil.DecodeStrict(account.Parameters, &descriptor) != nil || len(descriptor.Implicit) != 64 {
		t.Fatal("malformed disposable testnet descriptor")
	}
	named := "cw-stake-" + descriptor.Implicit[:20] + ".testnet"
	ui.account = named
	report["account"] = named
	if !descriptor.Named {
		// Reconcile any previous faucet attempt before requesting account creation.
		associated, associationErr := adapter.Associate(ctx, account, named)
		if associationErr != nil {
			if testnetIdentity(ctx) != nil {
				blocked("testnet identity changed before faucet request")
			}
			body, _ := json.Marshal(map[string]string{"newAccountId": named, "newAccountPublicKey": account.PublicKey})
			faucetErrors := []string{}
			for _, endpoint := range []string{"https://helper.testnet.near.org/account", "https://helper.nearprotocol.com/account"} {
				err := testnetHTTP().Do(ctx, "POST", endpoint, "application/json", body, nil)
				if err != nil {
					faucetErrors = append(faucetErrors, endpoint+": "+err.Error())
				}
				// A response or timeout is not evidence of funding. Verify the
				// SAME public account/key; never create another root or name.
				associated, associationErr = adapter.Associate(ctx, account, named)
				if associationErr == nil {
					break
				}
			}
			report["faucetAttempts"] = faucetErrors
			if associationErr != nil {
				blocked("official faucet funding/account authority could not be verified; the same encrypted fixture is retained for an explicit rerun")
			}
		}
		if err := engine.Associate(associated); err != nil {
			t.Fatal("verified testnet account association failed")
		}
		account = associated
	} else if account.Address != named {
		t.Fatal("refusing an unexpected persisted testnet account association")
	}
	report["funding"] = "PASS: final on-chain account/key authority verified"
	confirmed := map[string]bool{}
	receipts := []map[string]string{}
	reservedFees := new(big.Int)
	feeCap, _ := new(big.Int).SetString(publicTestnetFeeCap, 10)
	effectCap, _ := new(big.Int).SetString(publicTestnetEffectCap, 10)
	var withdrawalPlan *model.Plan
	for _, op := range engine.Pending() {
		var payload near.Payload
		if json.Unmarshal(op.Plan.Payload, &payload) != nil || payload.Staking == nil || op.Plan.Account.ID != account.ID || op.Plan.Account.Network != "near-testnet" || op.Plan.Recipient != publicTestnetPool {
			t.Fatal("unexpected journal operation in disposable fixture")
		}
		method := payload.Staking.Method
		if method != "deposit_and_stake" && method != "unstake_all" && method != "withdraw_all" {
			t.Fatal("unexpected testnet staking method")
		}
		if op.State == "CANCELLED" {
			continue
		}
		fee, err := chainutil.Big(op.Plan.MaxFee, 128)
		if err != nil {
			t.Fatal("invalid persisted testnet fee reserve")
		}
		reservedFees.Add(reservedFees, fee)
		if reservedFees.Cmp(feeCap) >= 0 {
			blocked("cumulative testnet fee reserve reaches the explicit 1-testnet-NEAR limit")
		}
		state := op.State
		if state != "CONFIRMED" {
			if op.Signed == nil || state == "SIGNED_NOT_SUBMITTED" {
				blocked("an existing operation requires reconciliation; no automatic replacement or expiry rebroadcast is permitted")
			}
			r, err := adapter.Reconcile(ctx, op.Plan, *op.Signed)
			if err != nil || r.State != "CONFIRMED" {
				blocked("original signed testnet operation remains unresolved; no new transaction was generated")
			}
			if engine.Update(op.Plan.ID, r.State, &r) != nil {
				t.Fatal("could not persist original operation reconciliation")
			}
			state = r.State
		}
		if confirmed[method] {
			t.Fatal("duplicate staking method found; refusing further testnet spending")
		}
		if op.Signed == nil {
			t.Fatal("confirmed testnet operation lacks its original signed identity")
		}
		if method == "withdraw_all" {
			copyPlan := op.Plan
			withdrawalPlan = &copyPlan
		}
		confirmed[method] = true
		receipts = append(receipts, map[string]string{"method": method, "identity": op.Signed.Identity, "state": state})
	}
	report["receipts"] = receipts
	execute := func(action, atomic, method string) {
		t.Helper()
		if testnetIdentity(ctx) != nil {
			blocked("testnet identity could not be reverified before a write")
		}
		p, err := adapter.PrepareStaking(ctx, account, near.StakingRequest{AccountID: account.ID, Validator: publicTestnetPool, Action: action, Amount: atomic})
		if err != nil {
			blocked("testnet staking preparation unavailable, balance insufficient, or pool still locked")
		}
		fee, err := chainutil.Big(p.MaxFee, 128)
		report["latestFeeReserveYoctoNEAR"] = p.MaxFee
		if err != nil || new(big.Int).Add(reservedFees, fee).Cmp(feeCap) >= 0 {
			blocked("cumulative testnet fee reserve exceeds the explicit less-than-1-testnet-NEAR cap")
		}
		effect, err := chainutil.Big(p.Amount, 128)
		if err != nil || effect.Cmp(effectCap) > 0 {
			blocked("testnet staking effect exceeds the disposable 0.1-NEAR cap")
		}
		p.ID = signer.ID()
		if method == "withdraw_all" {
			before, err := testnetLiquid(ctx, account.Address)
			if err != nil {
				blocked("independent liquid balance unavailable before withdrawal")
			}
			observation, _ := json.Marshal(testnetWithdrawalObservation{Account: account.Address, Operation: p.ID, BeforeLiquid: before.String(), Unstaked: p.Amount})
			encrypted, err := vault.Seal(observation, []byte(testnetFixturePassword), vault.Default)
			clear(observation)
			if err != nil || files.Write("withdrawal-observation.lwv", encrypted, false) != nil {
				t.Fatal("cannot preserve encrypted withdrawal balance checkpoint; no transaction submitted")
			}
			copyPlan := p
			withdrawalPlan = &copyPlan
		}
		if engine.Prepare(p) != nil {
			t.Fatal("testnet operation did not persist before authorization")
		}
		ui.method = method
		signed, err := engine.Sign(p.ID)
		ui.method = ""
		if err != nil {
			t.Fatal("exact capped testnet signer approval failed")
		}
		if engine.Update(p.ID, "SUBMITTING", nil) != nil {
			t.Fatal("testnet submission was not durably recorded")
		}
		r, err := adapter.Submit(ctx, p, signed)
		if err != nil {
			r = model.Receipt{Identity: signed.Identity, State: "UNKNOWN", Detail: "Public testnet submission acknowledgment unavailable; reconcile original identity"}
		}
		if engine.Update(p.ID, r.State, &r) != nil {
			t.Fatal("testnet receipt persistence failed")
		}
		receipts = append(receipts, map[string]string{"method": method, "identity": signed.Identity, "state": r.State})
		report["receipts"] = receipts
		if r.State != "CONFIRMED" {
			blocked("testnet operation is pending, failed or ambiguous; no replacement will be created")
		}
		confirmed[method] = true
		reservedFees.Add(reservedFees, fee)
	}
	state, err := testnetPosition(ctx, account.Address)
	if err != nil {
		blocked("independent testnet pool state unavailable")
	}
	if !confirmed["deposit_and_stake"] {
		if state.Staked != "0" || state.Unstaked != "0" || len(confirmed) > 0 {
			t.Fatal("fresh fixture contains unexpected existing stake")
		}
		execute("stake", publicTestnetStake, "deposit_and_stake")
		state, err = testnetPosition(ctx, account.Address)
		if err != nil || state.Staked == "0" {
			blocked("independent pool stake credit has not been verified")
		}
	}
	if !confirmed["unstake_all"] {
		execute("unstake_all", "", "unstake_all")
	}
	state, err = testnetPosition(ctx, account.Address)
	if err != nil {
		blocked("independent unstaked state unavailable")
	}
	report["observation"] = state
	if !confirmed["withdraw_all"] {
		if !state.CanWithdraw {
			report["status"] = "PARTIAL"
			report["reason"] = "Stake and unstake_all confirmed; withdraw_all NOT_RUN until the real testnet pool unlocks after its epoch wait. Rerun the same opt-in command to resume this encrypted fixture."
			t.Skip("PARTIAL: stake and unstake confirmed; real epoch-gated withdrawal is NOT_RUN")
		}
		if state.Unstaked == "0" {
			blocked("no withdrawable testnet pool balance has been verified")
		}
		execute("withdraw_all", "", "withdraw_all")
	}
	final, err := testnetPosition(ctx, account.Address)
	if err != nil || final.Staked != "0" || final.Unstaked != "0" {
		blocked("final empty pool state has not been independently verified")
	}
	if withdrawalPlan == nil {
		blocked("original withdrawal plan is missing")
	}
	encrypted, err := files.Read("withdrawal-observation.lwv")
	if err != nil {
		blocked("independent pre-withdrawal balance checkpoint is missing")
	}
	plain, err := vault.Open(encrypted, []byte(testnetFixturePassword))
	if err != nil {
		t.Fatal("withdrawal balance checkpoint authentication failed")
	}
	defer clear(plain)
	var before testnetWithdrawalObservation
	if chainutil.DecodeStrict(plain, &before) != nil || before.Account != account.Address || before.Operation != withdrawalPlan.ID || before.Unstaked != withdrawalPlan.Amount {
		t.Fatal("withdrawal balance checkpoint does not bind the original operation")
	}
	after, err := testnetLiquid(ctx, account.Address)
	if err != nil {
		blocked("independent post-withdrawal liquid balance is unavailable")
	}
	beforeLiquid, err := chainutil.Big(before.BeforeLiquid, 128)
	if err != nil {
		t.Fatal("malformed encrypted withdrawal observation")
	}
	credit := new(big.Int).Sub(after, beforeLiquid)
	withdrawn, _ := chainutil.Big(before.Unstaked, 128)
	fee, _ := chainutil.Big(withdrawalPlan.MaxFee, 128)
	minimum := new(big.Int).Sub(withdrawn, fee)
	if credit.Sign() <= 0 || credit.Cmp(minimum) < 0 {
		blocked("independent wallet credit does not match the observed withdrawal less its approved fee reserve")
	}
	report["independentWalletCreditYoctoNEAR"] = credit.String()
	report["status"], report["observation"] = "PASS", final
	report["notRun"] = []string{"mainnet spending", "graphical staking approval", "independent security audit"}
	t.Log(fmt.Sprintf("PASS: actual capped testnet stake → unstake_all → withdraw_all; %d original confirmed operation identities", len(receipts)))
}
