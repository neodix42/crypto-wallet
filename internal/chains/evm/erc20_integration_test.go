package evm

// Real, explicitly selected local-node acceptance. Only the committed public
// 00..3f seed and newly deployed contracts/funds in an owned loopback EVM are
// used. This never starts a public node, imports a user vault or proves public
// network consensus, a wallet approval dialog or independent security review.
import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"localwallet/internal/model"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type erc20NodeSnapshot struct {
	Status   string     `json:"status"`
	Native   []string   `json:"native"`
	Nonces   []string   `json:"nonces"`
	Balances [][]string `json:"balances"`
}

type erc20NodeReady struct {
	Status         string            `json:"status"`
	Endpoint       string            `json:"endpoint"`
	ChainID        string            `json:"chainId"`
	Network        string            `json:"network"`
	RollupFees     map[string]any    `json:"rollupFeeFixture"`
	ProducerSHA256 string            `json:"producerSHA256"`
	Packages       map[string]string `json:"packages"`
	Accounts       []struct {
		Address   string `json:"address"`
		PublicKey string `json:"publicKey"`
	} `json:"accounts"`
	Tokens []struct {
		Contract      string `json:"contract"`
		Symbol        string `json:"symbol"`
		Decimals      uint8  `json:"decimals"`
		CodeHash      string `json:"codeHash"`
		InitialAtomic string `json:"initialAtomic"`
	} `json:"tokens"`
}

func TestTP050ImportedERC20GoSignedTwoWayExecutionOnIndependentGanache(t *testing.T) {
	if os.Getenv("WALLET_ERC20_EVM") != "1" {
		t.Skip("NOT_RUN: set WALLET_ERC20_EVM=1 with cached pinned Node/solc/Ganache/ethers for owned loopback execution; no public RPC or funds")
	}
	const fixture = "tests/oracles/evm/erc20-node.mjs"
	const scope = "Go InspectToken/WithTokens/Balance/Prepare/Sign/Submit/Reconcile for two actual deployed ERC20 contracts on both Ethereum Sepolia-shaped and Base Sepolia-shaped owned loopback EVMs, with independent ethers signature/calldata/receipt/log/balance/gas/nonce checks; Base L1/operator fee oracle is deterministic fixture-only and not actually charged; synthetic public fixture keys/funds only; no GUI, public network, independent consensus, Base rollup economics or real-finality claim"
	writeReport := func(name string, value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil || os.MkdirAll("../../../reports", 0700) != nil {
			t.Fatal("cannot prepare local ERC20 execution report")
		}
		if os.WriteFile("../../../reports/"+name+".json.tmp", append(raw, '\n'), 0600) != nil ||
			os.Rename("../../../reports/"+name+".json.tmp", "../../../reports/"+name+".json") != nil {
			t.Fatal("cannot persist local ERC20 execution report")
		}
	}
	for _, name := range []string{"erc20-execution", "base-erc20-execution"} {
		writeReport(name, map[string]any{"status": "RUNNING", "sourceStable": false, "scope": scope})
	}
	t.Cleanup(func() {
		if t.Failed() {
			for _, name := range []string{"erc20-execution", "base-erc20-execution"} {
				writeReport(name, map[string]any{"status": "FAIL", "sourceStable": false, "scope": scope,
					"reason": "local-erc20-execution-failed; no acceptance claim"})
			}
		}
	})
	snapshotSources := func(t *testing.T) map[string]string {
		t.Helper()
		names := []string{fixture, "tests/oracles/evm/package.json", "tests/oracles/evm/package-lock.json", "go.mod", "go.sum", "internal/model/model.go"}
		paths, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatal("cannot enumerate ERC20 source identity")
		}
		for _, name := range paths {
			names = append(names, "internal/chains/evm/"+name)
		}
		hashes := map[string]string{}
		for _, name := range names {
			raw, err := os.ReadFile("../../../" + name)
			if err != nil {
				t.Fatal("local ERC20 execution source input missing")
			}
			digest := sha256.Sum256(raw)
			hashes[name] = hex.EncodeToString(digest[:])
		}
		return hashes
	}
	beforeSources := snapshotSources(t)
	results := map[string]map[string]any{}
	for _, profile := range []struct{ network, chainID string }{{"ethereum-sepolia", "11155111"}, {"base-sepolia", "84532"}} {
		t.Run(profile.network, func(t *testing.T) {
			node, err := exec.LookPath("node")
			if err != nil {
				t.Fatal("explicit local ERC20 acceptance requires the pinned Node runtime")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, node, "../../../"+fixture, profile.chainID)
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = io.Discard // Oracle errors contain no protocol/secret dumps.
			if err = command.Start(); err != nil {
				t.Fatal("local ERC20 node could not start")
			}
			waited := false
			t.Cleanup(func() {
				input.Close()
				if !waited {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			reader := bufio.NewScanner(output)
			reader.Buffer(make([]byte, 4096), 1<<20)
			next := func(out any) {
				t.Helper()
				if !reader.Scan() || json.Unmarshal(reader.Bytes(), out) != nil {
					t.Fatal("bounded independent local ERC20 oracle response missing")
				}
			}
			request := func(value any, out any) {
				t.Helper()
				if json.NewEncoder(input).Encode(value) != nil {
					t.Fatal("local ERC20 oracle command could not be delivered")
				}
				next(out)
			}
			var ready erc20NodeReady
			next(&ready)
			endpoint, err := url.Parse(ready.Endpoint)
			if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.Port() == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
				t.Fatal("local ERC20 oracle attempted an external or unbounded endpoint")
			}
			if ready.Status != "READY" || ready.ChainID != profile.chainID || ready.Network != profile.network || ready.ProducerSHA256 != beforeSources[fixture] ||
				!reflect.DeepEqual(ready.Packages, map[string]string{"ethers": "6.17.0", "ganache": "7.9.2", "solc": "0.8.30"}) || len(ready.Accounts) != 2 || len(ready.Tokens) != 2 {
				t.Fatal("independent local ERC20 producer/chain/dependency identity mismatch")
			}
			if (profile.network == "base-sepolia" && ready.RollupFees["status"] != "FIXTURE_ONLY") ||
				(profile.network == "ethereum-sepolia" && ready.RollupFees["status"] != "NOT_APPLICABLE") || ready.RollupFees["actuallyChargedByNode"] != false {
				t.Fatal("independent node misrepresented the deterministic rollup fee fixture")
			}
			base, err := NewWithEndpoints(map[string]string{profile.network: ready.Endpoint})
			if err != nil {
				t.Fatal(err)
			}
			accounts := make([]model.Account, 2)
			for i := range accounts {
				accounts[i], err = base.Derive(syntheticSeed(), uint32(i), profile.network)
				if err != nil || accounts[i].Address != ready.Accounts[i].Address || accounts[i].PublicKey != ready.Accounts[i].PublicKey {
					t.Fatal("independent public derivation identity differs from the wallet")
				}
			}
			tokens := make([]model.ERC20Token, 2)
			for i := range tokens {
				tokens[i], err = base.InspectToken(ctx, accounts[0], ready.Tokens[i].Contract)
				if err != nil || tokens[i].Network != profile.network || tokens[i].Asset.ID != profile.network+":erc20:"+ready.Tokens[i].Contract ||
					tokens[i].Asset.Symbol != ready.Tokens[i].Symbol || tokens[i].Asset.Decimals != ready.Tokens[i].Decimals || tokens[i].CodeHash != ready.Tokens[i].CodeHash {
					t.Fatal("standard deployed token metadata/code identity was not independently reproduced", err)
				}
			}
			adapter, err := base.WithTokens(tokens)
			if err != nil {
				t.Fatal(err)
			}
			pinnedUnavailable := map[string]bool{}
			for _, network := range base.Networks() {
				if network.ID == profile.network {
					for _, asset := range network.Assets {
						if asset.Contract != "" {
							if profile.network != "base-sepolia" || asset.Symbol != "ZEN" {
								t.Fatal("unexpected default token in the isolated imported-token fixture")
							}
							pinnedUnavailable[asset.ID] = true
						}
					}
				}
			}
			if profile.network == "base-sepolia" && len(pinnedUnavailable) != 1 {
				t.Fatal("Base fixture lost its default pinned ZEN policy")
			}
			assertBalances := func(snapshot erc20NodeSnapshot) {
				t.Helper()
				if snapshot.Status != "OBSERVED" || len(snapshot.Native) != 2 || len(snapshot.Nonces) != 2 || len(snapshot.Balances) != 2 {
					t.Fatal("independent balance snapshot missing")
				}
				for i, account := range accounts {
					balances, err := adapter.Balance(ctx, account)
					if err != nil || len(balances) != 3+len(pinnedUnavailable) {
						t.Fatal("ETH and both imported-token balances were not returned", err)
					}
					observed := map[string]model.Balance{}
					for _, balance := range balances {
						observed[balance.Asset] = balance
						if pinnedUnavailable[balance.Asset] {
							if balance.State != "unavailable" || balance.Atomic != "" || balance.At == "" {
								t.Fatal("absent pinned Base ZEN became a verified balance or zero")
							}
							continue
						}
						if balance.State != "provider-observed" || balance.At == "" {
							t.Fatal("actual available balance was not associated with verified provider state")
						}
					}
					if len(observed) != len(balances) {
						t.Fatal("actual balance response duplicated an asset identity")
					}
					for asset := range pinnedUnavailable {
						if value := observed[asset]; value.State != "unavailable" || value.Atomic != "" || value.At == "" {
							t.Fatal("unavailable pinned ZEN was hidden instead of retaining its own unknown observation")
						}
					}
					if observed[profile.network+":native"].Atomic != snapshot.Native[i] {
						t.Fatal("wallet native balance differs from independent node query")
					}
					for j, token := range tokens {
						if len(snapshot.Balances[j]) != 2 || observed[token.Asset.ID].Atomic != snapshot.Balances[j][i] {
							t.Fatal("one imported-token balance replaced or corrupted the other")
						}
					}
				}
			}
			observe := func() erc20NodeSnapshot {
				var observed erc20NodeSnapshot
				request(map[string]string{"op": "observe"}, &observed)
				return observed
			}
			initial := observe()
			assertBalances(initial)
			if initial.Nonces[0] != "0" || initial.Nonces[1] != "0" || initial.Balances[0][0] != "1000000000" || initial.Balances[1][0] != "1000000000000000000000" || initial.Balances[0][1] != "0" || initial.Balances[1][1] != "0" {
				t.Fatal("actual deployed fixture does not have the expected initial state")
			}
			negativeCases := []string{}
			for _, fault := range []string{"decimals", "missing-code", "false-return"} {
				var reply map[string]any
				request(map[string]string{"op": "fault", "kind": fault}, &reply)
				if reply["status"] != "FAULT_SET" || reply["kind"] != fault {
					t.Fatal("actual negative token fixture was not installed")
				}
				if _, err := adapter.Prepare(ctx, accounts[0], model.Transfer{AccountID: accounts[0].ID, Asset: tokens[0].Asset.ID, Recipient: accounts[1].Address, Amount: "1"}); err == nil {
					t.Fatal("real invalid token metadata/false-return simulation authorized a transfer")
				}
				if fault == "missing-code" {
					if _, err := base.InspectToken(ctx, accounts[0], tokens[0].Asset.Contract); err == nil {
						t.Fatal("unavailable token metadata was guessed during inspection")
					}
				}
				balances, err := adapter.Balance(ctx, accounts[0])
				observed := map[string]model.Balance{}
				for _, balance := range balances {
					observed[balance.Asset] = balance
				}
				if err != nil || len(balances) != 3+len(pinnedUnavailable) || observed[profile.network+":native"].State != "provider-observed" || observed[tokens[1].Asset.ID].State != "provider-observed" || observed[tokens[1].Asset.ID].Atomic != initial.Balances[1][0] {
					t.Fatal("one unavailable imported token hid ETH or the healthy token balance")
				}
				if fault != "false-return" && (observed[tokens[0].Asset.ID].State != "unavailable" || observed[tokens[0].Asset.ID].Atomic != "") {
					t.Fatal("invalid token metadata became a verified balance or zero")
				}
				for asset := range pinnedUnavailable {
					if value := observed[asset]; value.State != "unavailable" || value.Atomic != "" || value.At == "" {
						t.Fatal("imported-token fault corrupted the independently unavailable pinned ZEN observation")
					}
				}
				negativeCases = append(negativeCases, fault)
				request(map[string]string{"op": "fault", "kind": "none"}, &reply)
				if reply["status"] != "FAULT_SET" || reply["kind"] != "none" {
					t.Fatal("actual token fixture could not be restored")
				}
			}
			amounts := [2][2]string{{"123456789", "40000001"}, {"1234567890123456789", "345678901234567890"}}
			identities := []string{}
			reconciled := []model.Receipt{}
			for tokenIndex, token := range tokens {
				for direction := 0; direction < 2; direction++ {
					from, to := accounts[direction], accounts[1-direction]
					assertBalances(observe())
					plan, err := adapter.Prepare(ctx, from, model.Transfer{AccountID: from.ID, Asset: token.Asset.ID, Recipient: to.Address, Amount: amounts[tokenIndex][direction]})
					if err != nil || plan.Nonce != strconv.Itoa(tokenIndex) || plan.Amount != amounts[tokenIndex][direction] || plan.FeeAsset != profile.network+":native" {
						t.Fatal("real ERC20 preparation changed amount, shared account nonce or native fee currency", err)
					}
					foreign := plan
					foreign.Asset = tokens[1-tokenIndex].Asset.ID
					foreign.ID = planID(foreign)
					if _, err := adapter.Sign(syntheticSeed(), foreign); err == nil {
						t.Fatal("cross-token substitution survived independently reconstructed signing policy")
					}
					signed, err := adapter.Sign(syntheticSeed(), plan)
					if err != nil {
						t.Fatal("real imported-token signing failed", err)
					}
					var approved payload
					if StrictJSON(plan.Payload, &approved) != nil || approved.ChainID != profile.chainID {
						t.Fatal("Go-approved token transaction lost its exact chain domain")
					}
					if profile.network == "base-sepolia" && tokenIndex == 0 && direction == 0 {
						for _, fault := range []string{"missing-code", "increased"} {
							var reply map[string]any
							request(map[string]string{"op": "rollup-fee-fault", "kind": fault}, &reply)
							if reply["status"] != "FEE_FAULT_SET" || reply["kind"] != fault {
								t.Fatal("deterministic rollup fee fault was not installed")
							}
							if _, err := adapter.Submit(ctx, plan, signed); err == nil {
								t.Fatal("unavailable or increased Base fee fixture authorized broadcast")
							}
							negativeCases = append(negativeCases, "rollup-fee-"+fault)
							request(map[string]string{"op": "rollup-fee-fault", "kind": "none"}, &reply)
							if reply["status"] != "FEE_FAULT_SET" || reply["kind"] != "none" {
								t.Fatal("deterministic rollup fee fixture could not be restored")
							}
						}
					}
					accepted, err := adapter.Submit(ctx, plan, signed)
					if err != nil || accepted.State != "SUBMITTED" || accepted.Identity != signed.Identity {
						t.Fatal("actual local token submission failed", err)
					}
					receipt, err := adapter.Reconcile(ctx, plan, signed)
					if err != nil || receipt.State != "CONFIRMED" || receipt.Identity != signed.Identity || receipt.Block == "" {
						t.Fatal("actual local token receipt could not be reconciled", receipt.State, err)
					}
					var independent map[string]any
					request(map[string]any{"op": "verify", "token": tokenIndex, "from": direction, "to": 1 - direction,
						"identity": signed.Identity, "raw": "0x" + hex.EncodeToString(signed.Bytes), "amount": plan.Amount,
						"nonce": plan.Nonce, "maxFeeAtomic": plan.MaxFee,
						"dataFeeReserve": approved.DataFeeReserve, "operatorFeeReserve": approved.OperatorFeeReserve}, &independent)
					if independent["status"] != "PASS" || independent["identity"] != signed.Identity || independent["contract"] != token.Asset.Contract ||
						independent["amountAtomic"] != plan.Amount || independent["source"] != from.Address || independent["recipient"] != to.Address ||
						independent["nativeDebitAtomic"] != independent["gasChargeAtomic"] || independent["receiptStatus"] != "0x1" {
						t.Fatal("independent ethers execution disagreed with the approved token transfer")
					}
					identities = append(identities, signed.Identity)
					reconciled = append(reconciled, receipt)
				}
			}
			assertBalances(observe())
			var final map[string]any
			request(map[string]string{"op": "finish"}, &final)
			if final["status"] != "PASS" || final["producerSHA256"] != beforeSources[fixture] || final["chainId"] != profile.chainID || final["network"] != profile.network {
				t.Fatal("actual four-transfer/no-extra-transaction oracle did not complete")
			}
			transactions, ok := final["minedTransactions"].([]any)
			if !ok || len(transactions) != 6 {
				t.Fatal("actual chain contains unexpected transactions")
			}
			seen := map[string]bool{}
			for _, value := range transactions {
				hash, ok := value.(string)
				if !ok || !strings.HasPrefix(hash, "0x") || seen[hash] {
					t.Fatal("actual chain transaction inventory is ambiguous")
				}
				seen[hash] = true
			}
			for _, identity := range identities {
				if !seen[identity] {
					t.Fatal("Go-approved transfer absent from independent chain inventory")
				}
			}
			input.Close()
			err = command.Wait()
			waited = true
			if err != nil || !reflect.DeepEqual(beforeSources, snapshotSources(t)) {
				t.Fatal("local ERC20 oracle failed or measured source changed during execution")
			}
			networkScope := "Complete two-token out/back Go-signed execution on owned loopback " + profile.network + " (chain " + profile.chainID + "); synthetic public keys/funds only; any Base fee oracle quotes are deterministic fixtures, not charged rollup fees; no public-network, GUI, independent-consensus or real-finality claim"
			results[profile.network] = map[string]any{"status": "PASS", "sourceStable": true, "scope": networkScope, "network": profile.network, "producerSHA256": beforeSources[fixture],
				"sourceSHA256": beforeSources, "negativeCases": negativeCases, "tokenPolicies": tokens,
				"goReconciledReceipts": reconciled, "independentExecution": final, "pinnedUnavailableTokens": pinnedUnavailable}
		})
	}
	if t.Failed() || len(results) != 2 {
		return
	}
	// Keep the original Ethereum report fields for existing evidence collectors;
	// the overall PASS additionally requires the complete Base execution case.
	combined := map[string]any{}
	for key, value := range results["ethereum-sepolia"] {
		combined[key] = value
	}
	combined["networks"] = results
	combined["scope"] = scope
	writeReport("erc20-execution", combined)
	writeReport("base-erc20-execution", results["base-sepolia"])
}
