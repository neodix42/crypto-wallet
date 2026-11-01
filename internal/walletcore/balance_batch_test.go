package walletcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/chains"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

type batchAdapter struct {
	*publicAdapter
	read func(context.Context, model.Account) ([]model.Balance, error)
}

func (a *batchAdapter) Balance(ctx context.Context, account model.Account) ([]model.Balance, error) {
	return a.read(ctx, account)
}

func batchCoordinator(t *testing.T, networks []model.Network, read func(context.Context, model.Account) ([]model.Balance, error)) (*core, string, []string, <-chan ipc.Message) {
	t.Helper()
	adapter := &batchAdapter{publicAdapter: &publicAdapter{networks: networks}, read: read}
	c, path := fakeCoordinator(t, adapter.publicAdapter, nil)
	c.adapters = []model.Adapter{adapter}
	state := readPublicState(t, path)
	accounts, ids := []model.Account{}, []string{}
	for i, network := range networks {
		account := model.Account{ID: fmt.Sprintf("owned-%d", i), Network: network.ID, Family: network.Family, Address: "public-address", Profile: "public-test-only"}
		accounts, ids = append(accounts, account), append(ids, account.ID)
	}
	state["accounts"] = accounts
	writePublicState(t, path, state)
	r, w := io.Pipe()
	c.publicChannel = &ipc.Channel{W: w}
	progress := make(chan ipc.Message, model.BalanceBatchLimit)
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	go func() {
		defer close(progress)
		channel := &ipc.Channel{R: r}
		for {
			message, err := channel.Read()
			if err != nil {
				return
			}
			progress <- message
		}
	}()
	return c, path, ids, progress
}

func batchNetwork(id, host string) model.Network {
	return model.Network{ID: id, Family: "unit", RPC: host, Assets: []model.Asset{{ID: id + ":native"}}}
}

func batchMessage(body any) ipc.Message {
	request := message("balanceBatch", body)
	request.Session = "public-balance-session-01234567"
	return request
}

func batchValue(network string) []model.Balance {
	return []model.Balance{{Asset: network + ":native", Atomic: "123456789012345678901", State: "provider-observed"}}
}

func takeBatchProgress(t *testing.T, progress <-chan ipc.Message) model.BalanceBatchResult {
	t.Helper()
	select {
	case message := <-progress:
		var result model.BalanceBatchResult
		if message.Method != "balanceProgress" || message.Error != "" || json.Unmarshal(message.Body, &result) != nil || !model.ValidBalanceBatchResult(result) {
			t.Fatal("invalid streaming observation", message)
		}
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("independent provider completion was blocked")
		return model.BalanceBatchResult{}
	}
}

func TestBalanceBatchStreamsIndependentNetworksBeforeBlockedProviderCompletes(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	networks := []model.Network{batchNetwork("slow-mainnet", "https://slow.example"), batchNetwork("fast-mainnet", "https://fast.example"), batchNetwork("failed-mainnet", "https://failed.example")}
	c, path, ids, progress := batchCoordinator(t, networks, func(ctx context.Context, account model.Account) ([]model.Balance, error) {
		if account.Network == "slow-mainnet" {
			close(blocked)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if account.Network == "failed-mainnet" {
			return nil, errors.New("sensitive provider details must not escape")
		}
		return batchValue(account.Network), nil
	})
	done := make(chan error, 1)
	go func() { _, err := c.handle(batchMessage(map[string]any{"accountIds": ids})); done <- err }()
	<-blocked
	first, second := takeBatchProgress(t, progress), takeBatchProgress(t, progress)
	results := map[string]model.BalanceBatchResult{first.Network: first, second.Network: second}
	if results["fast-mainnet"].Values[0].Atomic != "123456789012345678901" || results["failed-mainnet"].Error != "BALANCE_UNAVAILABLE" || results["failed-mainnet"].Values != nil {
		t.Fatal("fast/failure observations lost isolation", results)
	}
	select {
	case err := <-done:
		t.Fatal("batch completed before blocked provider", err)
	default:
	}
	close(release)
	if takeBatchProgress(t, progress).Network != "slow-mainnet" || <-done != nil {
		t.Fatal("blocked provider did not finish independently")
	}
	state := readPublicState(t, path)
	if len(state["calls"].([]any)) != 1 || state["calls"].([]any)[0] != "snapshot" || len(state["ops"].([]any)) != 0 {
		t.Fatal("provider workers touched signer authority or durable operations", state)
	}
}

func TestBalanceBatchBoundsWorkersAndSerializesOneConfiguredAuthority(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			var running, maximum atomic.Int32
			started := make(chan struct{}, 8)
			release := make(chan struct{})
			var networks []model.Network
			for i := 0; i < 8; i++ {
				host := fmt.Sprintf("https://provider-%d.example", i)
				if shared {
					host = "https://shared.example/" + fmt.Sprint(i)
				}
				networks = append(networks, batchNetwork(fmt.Sprintf("net%d-mainnet", i), host))
			}
			c, _, ids, progress := batchCoordinator(t, networks, func(ctx context.Context, account model.Account) ([]model.Balance, error) {
				now := running.Add(1)
				for old := maximum.Load(); now > old && !maximum.CompareAndSwap(old, now); old = maximum.Load() {
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
				running.Add(-1)
				return batchValue(account.Network), nil
			})
			done := make(chan error, 1)
			go func() { _, err := c.handle(batchMessage(map[string]any{"accountIds": ids})); done <- err }()
			want := 4 // Independent resource contract, not the implementation constant.
			if shared {
				want = 1
			}
			for i := 0; i < want; i++ {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("workers did not reach independent providers")
				}
			}
			select {
			case <-started:
				t.Fatal("worker or host concurrency exceeded its bound")
			case <-time.After(25 * time.Millisecond):
			}
			close(release)
			for range ids {
				takeBatchProgress(t, progress)
			}
			if err := <-done; err != nil || maximum.Load() > int32(want) {
				t.Fatal("batch exceeded concurrency or failed", maximum.Load(), err)
			}
		})
	}
}

func TestBalanceBatchCancellationLockAndReconfigurationSuppressLateResults(t *testing.T) {
	for _, change := range []string{"cancel", "lock", "reconfigure"} {
		t.Run(change, func(t *testing.T) {
			started, release := make(chan context.Context, 1), make(chan struct{})
			c, path, ids, progress := batchCoordinator(t, []model.Network{batchNetwork("unit-mainnet", "https://provider.example")}, func(ctx context.Context, account model.Account) ([]model.Balance, error) {
				started <- ctx
				<-release // A late provider success must not revive a cancelled context.
				return batchValue(account.Network), nil
			})
			request := batchMessage(map[string]any{"accountIds": ids})
			done := make(chan error, 1)
			go func() { _, err := c.handle(request); done <- err }()
			ctx := <-started
			generation := c.child.Generation()
			switch change {
			case "cancel":
				if err := c.cancelHistory(historyControlMessage(request)); err != nil {
					t.Fatal(err)
				}
			case "lock":
				c.lock()
			case "reconfigure":
				c.mu.Lock()
				c.adapterRevision++
				c.mu.Unlock()
			}
			close(release)
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("stale provider completion succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("balance cancellation blocked the serialized worker")
			}
			select {
			case value := <-progress:
				t.Fatal("late balance escaped its context", value)
			default:
			}
			if change == "cancel" && (ctx.Err() == nil || c.child.Generation() != generation || !c.active) {
				t.Fatal("read cancellation ended the signer session")
			}
			if len(readPublicState(t, path)["ops"].([]any)) != 0 {
				t.Fatal("read cancellation modified payment records")
			}
		})
	}
}

func TestBalanceBatchPreservesEveryDefaultNetworkAssetAndUnknownToken(t *testing.T) {
	var networks []model.Network
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if !network.Testnet {
				networks = append(networks, network)
			}
		}
	}
	assets := 0
	c, _, ids, progress := batchCoordinator(t, networks, func(_ context.Context, account model.Account) ([]model.Balance, error) {
		for _, network := range networks {
			if network.ID != account.Network {
				continue
			}
			var values []model.Balance
			for i, asset := range network.Assets {
				value := model.Balance{Asset: asset.ID, Atomic: "1000001", State: "provider-observed"}
				if i > 0 {
					value.Atomic, value.State = "", "unavailable"
				}
				values = append(values, value)
			}
			return values, nil
		}
		return nil, errors.New("unexpected network")
	})
	if _, err := c.handle(batchMessage(map[string]any{"accountIds": ids})); err != nil {
		t.Fatal(err)
	}
	for range ids {
		result := takeBatchProgress(t, progress)
		assets += len(result.Values)
		if result.Error != "" || result.Values[0].Atomic != "1000001" {
			t.Fatal("one unavailable token hid a verified native balance", result)
		}
	}
	if len(ids) != 8 || assets != 19 {
		t.Fatal("default network/asset coverage changed", len(ids), assets)
	}
}

func TestBalanceBatchRejectsForeignDuplicateAndMalformedAccountsBeforeProviders(t *testing.T) {
	var calls atomic.Int32
	c, _, ids, _ := batchCoordinator(t, []model.Network{batchNetwork("unit-mainnet", "https://provider.example")}, func(_ context.Context, account model.Account) ([]model.Balance, error) {
		calls.Add(1)
		return batchValue(account.Network), nil
	})
	for _, selected := range [][]string{nil, {}, {ids[0], ids[0]}, {ids[0], "foreign-account"}, make([]string, model.BalanceBatchLimit+1)} {
		if out, err := c.handle(batchMessage(map[string]any{"accountIds": selected})); err == nil || out != nil && !bytes.Equal(mustBatchJSON(out), []byte(`{"completed":0}`)) {
			t.Fatal("invalid account batch accepted", selected, out, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unvalidated account reached a provider")
	}
}

func TestBalanceBatchActualHTTPTransportDeliversFastBalanceWhileAnotherSocketIsBlocked(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(batchValue("http-slow-mainnet"))
	}))
	t.Cleanup(slow.Close)
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(batchValue("http-fast-mainnet"))
	}))
	t.Cleanup(fast.Close)
	endpoints := map[string]string{"http-slow-mainnet": slow.URL, "http-fast-mainnet": fast.URL}
	c, _, ids, progress := batchCoordinator(t, []model.Network{batchNetwork("http-slow-mainnet", slow.URL), batchNetwork("http-fast-mainnet", fast.URL)}, func(ctx context.Context, account model.Account) ([]model.Balance, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints[account.Network], nil)
		if err != nil {
			return nil, err
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var values []model.Balance
		err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&values)
		return values, err
	})
	done := make(chan error, 1)
	go func() { _, err := c.handle(batchMessage(map[string]any{"accountIds": ids})); done <- err }()
	<-started
	if result := takeBatchProgress(t, progress); result.Network != "http-fast-mainnet" || result.Error != "" {
		t.Fatal("real provider transport serialized independent hosts", result)
	}
	close(release)
	if takeBatchProgress(t, progress).Network != "http-slow-mainnet" || <-done != nil {
		t.Fatal("blocked HTTP provider did not resume")
	}
}

func TestBalanceBatchCancelledWhileQueuedStartsNoProviderRead(t *testing.T) {
	var reads atomic.Int32
	c, path, ids, progress := batchCoordinator(t, []model.Network{batchNetwork("unit-mainnet", "https://provider.example")}, func(_ context.Context, account model.Account) ([]model.Balance, error) {
		reads.Add(1)
		return batchValue(account.Network), nil
	})
	request := batchMessage(map[string]any{"accountIds": ids})
	if err := c.registerHistoryControl(request, c.epoch); err != nil {
		t.Fatal(err)
	}
	if err := c.cancelHistory(historyControlMessage(request)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.handle(request); ipc.PublicErrorCode(err) != "REQUEST_CANCELLED" {
		t.Fatal("queued cancellation was not retained", err)
	}
	if reads.Load() != 0 || len(readPublicState(t, path)["ops"].([]any)) != 0 {
		t.Fatal("preempted read contacted provider or modified durable operations")
	}
	select {
	case message := <-progress:
		t.Fatal("preempted request emitted a balance", message)
	default:
	}
}

func TestBalanceBatchKeepsCheckedNativeWhenLaterTokenReadReachesDeadline(t *testing.T) {
	network := batchNetwork("unit-mainnet", "https://provider.example")
	network.Assets = append(network.Assets, model.Asset{ID: "unit-mainnet:token"})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	adapter := &batchAdapter{publicAdapter: &publicAdapter{networks: []model.Network{network}}, read: func(ctx context.Context, account model.Account) ([]model.Balance, error) {
		if ctx.Err() != context.DeadlineExceeded {
			t.Fatal("fixture did not reach the token-read deadline")
		}
		return []model.Balance{{Asset: "unit-mainnet:native", Atomic: "1000000000000000001", State: "provider-observed", At: "checked-head"}, {Asset: "unit-mainnet:token", State: "unavailable", At: "checked-head"}}, nil
	}}
	result := observeBalance(ctx, balanceJob{account: model.Account{ID: "owned", Network: network.ID}, network: network, adapter: adapter})
	if result.Error != "" || len(result.Values) != 2 || result.Values[0].Atomic != "1000000000000000001" || result.Values[1].Atomic != "" {
		t.Fatal("token timeout hid an already checked native observation or invented token zero", result)
	}
}

func mustBatchJSON(value any) []byte { body, _ := json.Marshal(value); return body }
