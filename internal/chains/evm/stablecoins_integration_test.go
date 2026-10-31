package evm

// Actual public runtime bytes execute on isolated loopback EVMs. This verifies
// token behavior and the Go transfer engine, not issuer reserves, public network
// consensus, bridge operations, rollup fee economics or funded acceptance.
import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
)

func TestDefaultStablecoinActualPinnedRuntimeTwoWayExecution(t *testing.T) {
	if os.Getenv("WALLET_STABLECOIN_EVM") != "1" {
		t.Skip("NOT_RUN: WALLET_STABLECOIN_EVM=1 requires pinned cached Node/Ganache/ethers/solc; synthetic isolated local funds only")
	}
	const producer = "tests/oracles/evm/stablecoin-node.mjs"
	const scope = "Actual pinned Circle USDC proxy+implementation, Arbitrum USD₮0 proxy+implementation and Base bridged USDT runtimes execute twelve Go-signed out/back transfers across four isolated loopback EVM chain IDs; independently checked ethers bytes/events/balances/nonces/native gas and production readonly history. Synthetic initialized issuer/bridge storage; no bridge calls, public network, issuer/consensus/finality acceptance. Base fee oracle is a deterministic planning fixture, not charged rollup economics."
	write := func(value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil || os.WriteFile("../../../reports/stablecoin-execution.json", append(raw, '\n'), 0600) != nil {
			t.Fatal("cannot retain stablecoin execution evidence")
		}
	}
	snapshotSources := func() map[string]string {
		t.Helper()
		files, _ := filepath.Glob("*.go")
		names := []string{producer, "tests/oracles/evm/package-lock.json", "internal/chains/evm/testdata/stablecoin-runtime.json", "go.mod", "go.sum"}
		for _, file := range files {
			names = append(names, "internal/chains/evm/"+file)
		}
		hashes := map[string]string{}
		for _, name := range names {
			raw, err := os.ReadFile("../../../" + name)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			hashes[name] = hex.EncodeToString(sum[:])
		}
		return hashes
	}
	before := snapshotSources()
	results := []map[string]any{}
	write(map[string]any{"status": "RUNNING", "sourceStable": false, "scope": scope})
	t.Cleanup(func() {
		stable := reflect.DeepEqual(before, snapshotSources())
		status := "PASS"
		if t.Failed() || !stable {
			status = "FAIL"
		}
		write(map[string]any{"status": status, "sourceStable": stable, "sources": before, "scope": scope, "networks": results})
		if !stable {
			t.Error("stablecoin execution sources changed during run")
		}
	})
	for _, network := range []string{"ethereum-mainnet", "base-mainnet", "arbitrum-mainnet", "hyperevm-mainnet"} {
		t.Run(network, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			node, err := exec.LookPath("node")
			if err != nil {
				t.Fatal("pinned Node required")
			}
			command := exec.CommandContext(ctx, node, "../../../"+producer, network)
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = io.Discard
			if command.Start() != nil {
				t.Fatal("local oracle failed to start")
			}
			t.Cleanup(func() {
				_ = input.Close()
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			scanner := bufio.NewScanner(output)
			scanner.Buffer(make([]byte, 4096), 65536)
			next := func(out any) {
				t.Helper()
				if !scanner.Scan() || StrictJSON(scanner.Bytes(), out) != nil {
					t.Fatal("local oracle response missing or malformed")
				}
			}
			request := func(value, out any) {
				t.Helper()
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = input.Write(append(raw, '\n')); err != nil {
					t.Fatal("local oracle request failed")
				}
				next(out)
			}
			var ready struct {
				Status, Endpoint, Network, ChainID, ProducerSHA256 string
				Tokens, Accounts                                   []string
			}
			next(&ready)
			u, err := url.Parse(ready.Endpoint)
			if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.RawQuery != "" || u.User != nil || u.Fragment != "" || ready.Status != "READY" || ready.Network != network || ready.ProducerSHA256 != before[producer] || len(ready.Accounts) != 2 || len(ready.Tokens) != len(defaultStablecoins(network)) {
				t.Fatal("independent oracle identity/owned endpoint mismatch")
			}
			adapter, err := NewWithEndpoints(map[string]string{network: ready.Endpoint})
			if err != nil {
				t.Fatal(err)
			}
			n, _ := adapter.network(network)
			if ready.ChainID != n.ChainID {
				t.Fatal("local chain domain mismatch")
			}
			accounts := make([]model.Account, 2)
			for i := range accounts {
				accounts[i], err = adapter.Derive(syntheticSeed(), uint32(i), network)
				if err != nil || accounts[i].Address != ready.Accounts[i] {
					t.Fatal("independent owner recovery mismatch")
				}
			}
			observe := func() erc20NodeSnapshot {
				var snapshot erc20NodeSnapshot
				request(map[string]string{"op": "observe"}, &snapshot)
				if snapshot.Status != "OBSERVED" || len(snapshot.Native) != 2 || len(snapshot.Balances) != len(ready.Tokens) {
					t.Fatal("independent observation incomplete")
				}
				return snapshot
			}
			assertBalances := func(snapshot erc20NodeSnapshot) {
				t.Helper()
				for i, account := range accounts {
					balances, err := adapter.Balance(ctx, account)
					if err != nil || len(balances) != len(n.Assets) {
						t.Fatal("default balance query failed", err)
					}
					observed := map[string]model.Balance{}
					for _, balance := range balances {
						if _, duplicate := observed[balance.Asset]; duplicate {
							t.Fatal("duplicate token default")
						}
						observed[balance.Asset] = balance
					}
					if observed[network+":native"].Atomic != snapshot.Native[i] {
						t.Fatal("native gas observation differs from independent node")
					}
					for j, contract := range ready.Tokens {
						balance := observed[network+":erc20:"+contract]
						if balance.State != "provider-observed" || balance.Atomic != snapshot.Balances[j][i] || balance.At == "" {
							token, _ := asset(n, network+":erc20:"+contract)
							t.Fatal("default token balance not independently verified", contract, balance.State, balance.Atomic, adapter.tokenIdentity(ctx, n, token, "latest"))
						}
					}
				}
			}
			assertBalances(observe())
			expected := map[string]model.HistoryTransfer{}
			for tokenIndex, contract := range ready.Tokens {
				token, err := asset(n, network+":erc20:"+contract)
				if err != nil {
					t.Fatal(err)
				}
				for direction, amount := range []string{"123456789", "40000001"} {
					assertBalances(observe())
					from, to := accounts[direction], accounts[1-direction]
					plan, err := adapter.Prepare(ctx, from, model.Transfer{AccountID: from.ID, Asset: token.ID, Recipient: to.Address, Amount: amount})
					if err != nil {
						t.Fatal("actual canonical token prepare failed", err)
					}
					signed, err := adapter.Sign(syntheticSeed(), plan)
					if err != nil {
						t.Fatal(err)
					}
					receipt, err := adapter.Submit(ctx, plan, signed)
					if err != nil || receipt.Identity != signed.Identity || receipt.State != "SUBMITTED" {
						t.Fatal("actual canonical token submission failed", err)
					}
					receipt, err = adapter.Reconcile(ctx, plan, signed)
					if err != nil || receipt.State != "CONFIRMED" || receipt.Identity != signed.Identity {
						t.Fatal("actual canonical token receipt could not reconcile", err, receipt.State)
					}
					var independent struct{ Status, Identity, Contract, Amount, Fee, Source, Destination string }
					request(map[string]any{"op": "verify", "token": tokenIndex, "from": direction, "raw": "0x" + hex.EncodeToString(signed.Bytes), "identity": signed.Identity, "amount": amount, "maxFee": plan.MaxFee}, &independent)
					if independent.Status != "PASS" || independent.Identity != signed.Identity || independent.Contract != contract || independent.Amount != amount || independent.Source != from.Address || independent.Destination != to.Address || independent.Fee == "" {
						t.Fatal("independent actual transfer effects differ")
					}
					expected[signed.Identity] = model.HistoryTransfer{Source: from.Address, Destination: to.Address, Asset: token, Atomic: amount}
				}
			}
			assertBalances(observe())
			for _, account := range accounts {
				page, err := adapter.HistoryPage(ctx, account, "")
				if err != nil {
					t.Fatal("actual canonical token history failed", err)
				}
				seen := map[string]bool{}
				for _, entry := range page.Entries {
					want, exists := expected[entry.Identity]
					if !exists {
						continue
					}
					if seen[entry.Identity] || len(entry.Transfers) != 1 || entry.Transfers[0].Asset != want.Asset || entry.Transfers[0].Source != want.Source || entry.Transfers[0].Destination != want.Destination || entry.Transfers[0].Atomic != want.Atomic || entry.Timestamp <= 0 || entry.State != "CONFIRMED" {
						t.Fatal("canonical runtime history differs from independently verified effects")
					}
					seen[entry.Identity] = true
				}
				if len(seen) != len(expected) {
					t.Fatal("canonical history omitted a Go-signed transfer")
				}
				if len(n.Assets) > len(ready.Tokens)+1 && !strings.Contains(page.Scope, "unavailable/incomplete") {
					t.Fatal("absent other pinned runtime lost partial scope")
				}
			}
			var final map[string]any
			request(map[string]string{"op": "finish"}, &final)
			if final["status"] != "PASS" || final["transfers"] != json.Number(strconv.Itoa(len(ready.Tokens)*2)) {
				t.Fatal("independent final accounting incomplete")
			}
			_ = input.Close()
			if err = command.Wait(); err != nil {
				t.Fatal("local execution oracle did not exit cleanly")
			}
			results = append(results, final)
		})
	}
}
