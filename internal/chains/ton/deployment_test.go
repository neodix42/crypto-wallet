package ton

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"localwallet/internal/model"
)

type deploymentRPC struct {
	state, balance, code, data string
	transactions               []map[string]string
	fee                        int
	sendCount                  int
	ambiguous                  bool
}

func (fixture *deploymentRPC) configure(t *testing.T, adapter *Adapter, network string) {
	t.Helper()
	info, _ := netInfo(network)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var query struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(request.Body).Decode(&query)
		var result any
		switch query.Method {
		case "getMasterchainInfo":
			result = map[string]any{"init": map[string]any{"root_hash": info.root, "file_hash": info.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
		case "getAddressInformation":
			result = map[string]any{"state": fixture.state, "balance": fixture.balance, "code": fixture.code, "data": fixture.data, "sync_utime": time.Now().Unix()}
		case "estimateFee":
			result = map[string]any{"source_fees": map[string]int{"in_fwd_fee": 0, "storage_fee": 0, "gas_fee": fixture.fee, "fwd_fee": 0}}
		case "sendBoc":
			fixture.sendCount++
			if fixture.ambiguous {
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}
			result = map[string]any{"@type": "ok"}
		case "getTransactions":
			result = fixture.transactions
		default:
			t.Errorf("unexpected TON deployment RPC %s", query.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	if err := adapter.Configure(network, Endpoint{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
}

func deploymentPlan(t *testing.T) (*Adapter, []byte, model.Plan, *deploymentRPC) {
	t.Helper()
	adapter, seed, transfer := testPlan(t)
	rpc := &deploymentRPC{state: "uninitialized", balance: "1000000000", fee: 1000000, transactions: []map[string]string{}}
	rpc.configure(t, adapter, transfer.Account.Network)
	plan, err := adapter.PrepareDeployment(context.Background(), transfer.Account)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, seed, plan, rpc
}

func TestDeploymentStateAndExactNoTransferPlan(t *testing.T) {
	adapter, seed, plan, rpc := deploymentPlan(t)
	for _, row := range []struct {
		balance, state, status string
		canDeploy              bool
	}{{"0", "uninitialized", "uninit", false}, {"10000000", "uninitialized", "uninit", false}, {"1000000000", "uninitialized", "uninit", true}, {"1000000000", "frozen", "frozen", false}} {
		rpc.balance, rpc.state = row.balance, row.state
		state, err := adapter.WalletState(context.Background(), plan.Account)
		if err != nil || state.Status != row.status || state.CanDeploy != row.canDeploy || state.WalletID != "2147483645" || state.Address != plan.Account.Address {
			t.Fatalf("incorrect deployment state: %+v %v", state, err)
		}
		if !row.canDeploy {
			if _, err := adapter.PrepareDeployment(context.Background(), plan.Account); err == nil {
				t.Fatal("unfunded/frozen contract offered executable deployment")
			}
		}
	}
	rpc.state, rpc.balance = "uninitialized", "1000000000"
	if !IsDeploymentPlan(plan) || plan.Amount != "0" || plan.Recipient != plan.Account.Address || plan.Memo != "" || !strings.Contains(DeploymentEffects(plan), "no outgoing transfers") {
		t.Fatal("deployment did not bind its exact zero-transfer effects")
	}
	signed, err := adapter.Sign(seed, plan)
	if err != nil {
		t.Fatal(err)
	}
	_, external, err := adapter.validateSigned(plan, signed)
	if err != nil || external.StateInit == nil || external.Body.RefsNum() != 0 {
		t.Fatal("deployment missing StateInit or contains actions", err)
	}
	for name, mutate := range map[string]func(*model.Plan, *Payload){
		"nonzero transfer": func(p *model.Plan, _ *Payload) { p.Amount = "1" },
		"other recipient":  func(p *model.Plan, _ *Payload) { p.Recipient = "0:" + strings.Repeat("00", 32) },
		"comment":          func(p *model.Plan, _ *Payload) { p.Memo = "unexpected" },
		"bounce":           func(_ *model.Plan, x *Payload) { x.Bounce = true },
		"seqno":            func(p *model.Plan, x *Payload) { x.Seqno = 1; p.Nonce = "1" },
		"no StateInit":     func(_ *model.Plan, x *Payload) { x.Deploy = false },
		"other operation":  func(_ *model.Plan, x *Payload) { x.Kind = "arbitrary" },
		"authority hash":   func(_ *model.Plan, x *Payload) { x.SourceDataHash = strings.Repeat("11", 32) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := plan
			payload, _ := payload(changed)
			mutate(&changed, &payload)
			changed.Payload, _ = json.Marshal(payload)
			changed.ID = planID(changed)
			if adapter.ValidatePlan(changed) == nil {
				t.Fatal("changed deployment accepted")
			}
			if _, _, err := adapter.validateSigned(changed, signed); err == nil {
				t.Fatal("old signature accepted changed deployment")
			}
		})
	}
	receipt, err := adapter.Submit(context.Background(), plan, signed)
	if err != nil || receipt.State != "SUBMITTED" || rpc.sendCount != 1 {
		t.Fatal("exact deployment not submitted", receipt, err)
	}
	rpc.ambiguous = true
	receipt, err = adapter.Submit(context.Background(), plan, signed)
	if err == nil || receipt.State != "UNKNOWN" || receipt.Identity != signed.Identity || rpc.sendCount != 2 {
		t.Fatal("ambiguous submission lost original identity", receipt, err)
	}
	before := rpc.sendCount
	if receipt, err := adapter.Reconcile(context.Background(), plan, signed); err != nil || receipt.State != "UNKNOWN" || rpc.sendCount != before {
		t.Fatal("absent deployment history triggered replacement or success", receipt, err)
	}
}

func TestDeploymentSubmitRechecksFundingAndUninitializedAuthority(t *testing.T) {
	for _, changed := range []string{"balance", "state", "fees"} {
		t.Run(changed, func(t *testing.T) {
			adapter, seed, plan, rpc := deploymentPlan(t)
			signed, err := adapter.Sign(seed, plan)
			if err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "balance":
				rpc.balance = "0"
			case "state":
				rpc.state = "frozen"
			case "fees":
				rpc.fee = 4000000
			}
			if _, err := adapter.Submit(context.Background(), plan, signed); err == nil || rpc.sendCount != 0 {
				t.Fatal("changed deployment state reached network submission")
			}
		})
	}
}

func TestDeploymentObservationsAndPreparationFailClosed(t *testing.T) {
	for _, failure := range []string{"descriptor", "network", "state", "stale state", "wrong active code", "fee"} {
		t.Run(failure, func(t *testing.T) {
			adapter, _, transfer := testPlan(t)
			node, closeNode := tonNode(t, adapter, transfer)
			defer closeNode()
			account := transfer.Account
			switch failure {
			case "descriptor":
				account.Parameters = []byte(`{}`)
			case "network":
				node.failMethod = "getMasterchainInfo"
			case "state":
				node.failMethod = "getAddressInformation"
			case "stale state":
				node.source.SyncUTime = 1
			case "wrong active code":
				node.source.State, node.source.Code = "active", "invalid"
			case "fee":
				node.failMethod = "estimateFee"
			}
			if failure != "fee" {
				if state, err := adapter.WalletState(context.Background(), account); err == nil || state.Status != "" || state.CanDeploy {
					t.Fatal("invalid observation became a usable zero/uninit/active state", state, err)
				}
			}
			if _, err := adapter.PrepareDeployment(context.Background(), account); err == nil || node.broadcasts != 0 {
				t.Fatal("unverified deployment state produced a plan or broadcast")
			}
		})
	}
	if DeploymentEffects(model.Plan{}) != "Unsupported TON deployment" {
		t.Fatal("invalid deployment produced authoritative-looking effects")
	}
}

func TestDeploymentReconcileRejectsUnexpectedOutgoingActions(t *testing.T) {
	adapter, _, plan, _ := deploymentPlan(t)
	_, _, _, transaction, _ := receiptFixture(t)
	for _, dictionaryOnly := range []bool{false, true} {
		transaction.OutMsgCount = 1
		if dictionaryOnly {
			transaction.OutMsgCount = 0
		}
		receipt, err := adapter.reconcileDeployment(context.Background(), plan, model.Signed{Identity: "original-deployment"}, transaction)
		if err != nil || receipt.State != "FAILED" || receipt.Identity != "original-deployment" {
			t.Fatal("unexpected deployment outgoing action was accepted", receipt, err)
		}
	}
	transaction.OutMsgCount, transaction.IO.Out = 0, nil
	if receipt, err := adapter.reconcileDeployment(context.Background(), plan, model.Signed{Identity: "original-deployment"}, transaction); err != nil || receipt.State != "UNKNOWN" {
		t.Fatal("successful-looking computation without active contract falsely confirmed", receipt, err)
	}
}

func TestDeploymentCannotReplaceAnAlreadyActiveWalletAtSeqnoZero(t *testing.T) {
	adapter, _, plan, rpc := deploymentPlan(t)
	pub, _ := hex.DecodeString(plan.Account.PublicKey)
	init, _, err := StateInit(pub, plan.Account.Network)
	if err != nil {
		t.Fatal(err)
	}
	rpc.state = "active"
	rpc.code, rpc.data = base64.StdEncoding.EncodeToString(init.Code.ToBOC()), base64.StdEncoding.EncodeToString(init.Data.ToBOC())
	if _, err := adapter.PrepareDeployment(context.Background(), plan.Account); err == nil || rpc.sendCount != 0 {
		t.Fatal("valid active seqno-zero contract offered a new deployment")
	}
}

func TestDeploymentOnlyActualV5R1Emulator(t *testing.T) {
	deploymentOnlyActualEmulator(t, Profile)
}

func TestSubwallet42DeploymentOnlyActualV5R1Emulator(t *testing.T) {
	deploymentOnlyActualEmulator(t, ProfileSubwallet42)
}

func deploymentOnlyActualEmulator(t *testing.T, profile string) {
	t.Helper()
	if os.Getenv("WALLET_TON_EMULATOR") != "1" {
		t.Skip("NOT_RUN: set WALLET_TON_EMULATOR=1 with pinned TON oracle dependencies")
	}
	for _, network := range []string{"ton-mainnet", "ton-testnet"} {
		t.Run(network, func(t *testing.T) { deploymentOnlyActualNetwork(t, profile, network) })
	}
}

func deploymentOnlyActualNetwork(t *testing.T, profile, network string) {
	t.Helper()
	adapter, seed, plan, rpc := deploymentPlan(t)
	rpc.balance = "200000000"
	if network != plan.Account.Network {
		rpc.configure(t, adapter, network)
	}
	{
		account, err := adapter.DeriveProfile(seed, plan.Account.Index, network, profile)
		if err != nil {
			t.Fatal(err)
		}
		plan, err = adapter.PrepareDeployment(context.Background(), account)
		if err != nil {
			t.Fatal(err)
		}
	}
	signed, err := adapter.Sign(seed, plan)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, _ := descriptor(plan.Account)
	request, _ := json.Marshal(map[string]any{"address": plan.Account.Address, "index": plan.Account.Index, "expires": plan.Expires, "boc": base64.StdEncoding.EncodeToString(signed.Bytes), "globalId": meta.GlobalID, "subwallet": meta.Subwallet, "walletId": strconv.FormatUint(uint64(meta.WalletID), 10)})
	dir := t.TempDir()
	requestPath, reportPath := filepath.Join(dir, "request.json"), filepath.Join(dir, "report.json")
	if err := os.WriteFile(requestPath, request, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("node", "../../../tests/oracles/ton/deployment.cjs", requestPath, reportPath).CombinedOutput()
	if err != nil {
		t.Fatalf("actual deployment emulator: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
	var report struct {
		Status, Code, Data, BalanceAfter string
		Transactions                     []map[string]string
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil || json.Unmarshal(raw, &report) != nil || report.Status != "PASS" {
		t.Fatal("deployment emulator report invalid", err)
	}
	rpc.code, rpc.data, rpc.state, rpc.balance = report.Code, report.Data, "active", report.BalanceAfter
	rpc.transactions = report.Transactions
	state, err := adapter.WalletState(context.Background(), plan.Account)
	if err != nil || state.Status != "active" || state.CanDeploy {
		t.Fatal("emulated deployed authority not accepted", state, err)
	}
	if _, err := adapter.PrepareDeployment(context.Background(), plan.Account); err == nil {
		t.Fatal("active wallet offered redeployment")
	}
	if receipt, err := adapter.Reconcile(context.Background(), plan, signed); err != nil || receipt.State != "CONFIRMED" {
		t.Fatal("actual deployment trace not reconciled", receipt, err)
	}
	// A successful empty-action deployment is not an ordinary TON payment.
	transactionCell, err := decodeBOC(report.Transactions[0]["data"])
	if err != nil {
		t.Fatal(err)
	}
	slice, _ := transactionCell.BeginParse()
	var transaction tlb.Transaction
	if err := tlb.LoadFromCell(&transaction, slice); err != nil || transaction.OutMsgCount != 0 {
		t.Fatal("deployment generated outgoing payment", err)
	}
	rpc.state = "uninitialized"
	if receipt, err := adapter.Reconcile(context.Background(), plan, signed); err != nil || receipt.State != "UNKNOWN" {
		t.Fatal("deployment trace without active authority falsely confirmed", receipt, err)
	}
}
