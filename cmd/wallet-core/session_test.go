package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains"
	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/near"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

func waitPublicFixture(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("public fixture did not reach response boundary")
}

func TestTP041AllHandlersRejectNonObjectBodiesWithoutStartingSigner(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	// The executable must never start for malformed top-level arguments.
	c.child.Path = filepath.Join(t.TempDir(), "unavailable-peer")
	for _, method := range []string{"networks", "accounts", "status", "history", "unlock", "import", "restore", "backup", "receive", "configure", "hideAccount", "discoverAccounts", "cancelPrepared", "discovery", "balances", "prepare", "send"} {
		for _, body := range []string{"null", "[]", "false", `"public synthetic value"`, "123", "", " \n null \t"} {
			out, err := c.handle(ipc.Message{Method: method, Body: []byte(body)})
			if err == nil || out != nil || err.Error() != "request body must be an object" {
				t.Fatal("non-object body reached signer", method, body, out, err)
			}
		}
	}
}

func TestTP043NativeCompletionsCannotReactivateChangedSession(t *testing.T) {
	for _, method := range []string{"create", "import", "unlock", "backup", "restore", "password", "addAccount", "hideAccount", "discoverAccounts", "cancelPrepared", "receive", "copyReceive", "saveQR", "scanQR", "configure", "discovery"} {
		t.Run(method, func(t *testing.T) {
			c, path := fakeCoordinator(t, &publicAdapter{}, nil)
			state := readPublicState(t, path)
			state["delayMethod"] = method
			writePublicState(t, path, state)
			done := make(chan error, 1)
			go func() {
				out, err := c.handle(message(method, struct{}{}))
				if err == nil || out != nil {
					done <- errors.New("stale native output was returned")
					return
				}
				done <- nil
			}()
			dir := filepath.Dir(path)
			waitPublicFixture(t, filepath.Join(dir, "native-ready"))
			// Reproduce the epoch transition at a response boundary without
			// killing the peer, so the late activation guard itself is exercised.
			c.mu.Lock()
			c.epoch++
			c.active = false
			c.mu.Unlock()
			if err := os.WriteFile(filepath.Join(dir, "native-release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			active, ctx := c.active, c.sessionCtx
			c.mu.Unlock()
			if active || ctx != nil {
				t.Fatal("stale native result restored the session")
			}
		})
	}
}

func TestTP043NativeActivityRefreshesPendingDialogWithoutUnlocking(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	state["delayMethod"], state["activityMethod"] = "password", "password"
	writePublicState(t, path, state)
	c.last = time.Now().Add(-6 * time.Minute)
	c.child.OnActivity = c.activity
	done := make(chan error, 1)
	go func() { _, err := c.handle(message("password", struct{}{})); done <- err }()
	waitPublicFixture(t, filepath.Join(filepath.Dir(path), "native-ready"))
	refreshed := false
	for i := 0; i < 3000; i++ {
		c.mu.Lock()
		refreshed = time.Since(c.last) < time.Second
		c.mu.Unlock()
		if refreshed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !refreshed || c.lockIfIdle(time.Now()) {
		t.Fatal("real private activity did not preserve active native request")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "native-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	c.lock()
	c.activity()
	c.mu.Lock()
	active, ctx := c.active, c.sessionCtx
	c.mu.Unlock()
	if active || ctx != nil {
		t.Fatal("activity unlocked an ended session")
	}
}

func TestTP043IdleThresholdUsesLatestInputAndPollingDoesNotRefresh(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	c.last = time.Now().Add(-4 * time.Minute)
	last := c.last
	if _, err := c.current(); err != nil {
		t.Fatal(err)
	}
	if !c.last.Equal(last) || c.lockIfIdle(last.Add(5*time.Minute-time.Nanosecond)) {
		t.Fatal("background polling changed inactivity threshold")
	}
	c.activity()
	if c.lockIfIdle(last.Add(5 * time.Minute)) {
		t.Fatal("idle check discarded more recent native input")
	}
	ctx, cancel := c.networkContext(time.Minute)
	defer cancel()
	if !c.lockIfIdle(c.last.Add(5*time.Minute)) || ctx.Err() != context.Canceled {
		t.Fatal("five-minute idle boundary did not cancel session")
	}
	if c.lockIfIdle(time.Now().Add(24 * time.Hour)) {
		t.Fatal("already locked session was repeatedly ended")
	}
}

func TestTP043OldQueriesCannotJoinNewUnlockedSession(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	oldEpoch := c.epoch
	old, stopOld := c.networkContext(time.Minute)
	defer stopOld()
	c.lock()
	c.mu.Lock()
	c.active = true
	c.mu.Unlock()
	fresh, stopFresh := c.networkContext(time.Minute)
	defer stopFresh()
	late, stopLate := c.networkContextAt(time.Minute, oldEpoch)
	defer stopLate()
	if old.Err() != context.Canceled || late.Err() != context.Canceled || fresh.Err() != nil {
		t.Fatal("network context crossed lock epochs")
	}
	state := readPublicState(t, path)
	state["locked"] = true
	writePublicState(t, path, state)
	if snapshot, err := c.current(); err != nil || !snapshot.Locked || fresh.Err() != context.Canceled {
		t.Fatal("signer lock observation did not cancel current queries", err)
	}
	if _, err := c.handleAt(message("unlock", struct{}{}), oldEpoch); err == nil {
		t.Fatal("queued old-epoch unlock started a new native prompt")
	}
	if err := c.childCallAt(oldEpoch, "unlock", struct{}{}, nil); err == nil {
		t.Fatal("old handler started a new child call")
	}
}

func TestTP043LockDropsConfiguredAdaptersAndStaleSettingsCannotRestoreThem(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = chains.All()
	state := readPublicState(t, path)
	state["settings"] = map[string]any{
		"near-testnet":    map[string]string{"url": "http://127.0.0.1:3030"},
		"decred-testnet3": map[string]string{"url": "https://127.0.0.1:19109", "user": "synthetic-rpc-user", "password": "public-synthetic-rpc-password"},
		"ton-testnet":     map[string]string{"url": "http://127.0.0.1:8089", "apiKey": "public-synthetic-api-key"},
	}
	writePublicState(t, path, state)
	if err := c.applySettings(); err != nil {
		t.Fatal(err)
	}
	old, err := c.adapter("near-testnet")
	if err != nil {
		t.Fatal(err)
	}
	c.lock()
	fresh, err := c.adapter("near-testnet")
	if err != nil || old == fresh {
		t.Fatal("lock retained configured adapter references", err)
	}
	for _, a := range c.adaptersSnapshot() {
		for _, n := range a.Networks() {
			if (n.ID == "near-testnet" || n.ID == "decred-testnet3" || n.ID == "ton-testnet") && strings.Contains(n.RPC, "127.0.0.1") {
				t.Fatal("locked coordinator retained custom RPC endpoints")
			}
		}
	}
	state = readPublicState(t, path)
	state["delayMethod"] = "settings"
	writePublicState(t, path, state)
	done := make(chan error, 1)
	go func() { done <- c.applySettings() }()
	dir := filepath.Dir(path)
	waitPublicFixture(t, filepath.Join(dir, "native-ready"))
	c.mu.Lock()
	c.epoch++
	c.mu.Unlock()
	if err = os.WriteFile(filepath.Join(dir, "native-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err == nil {
		t.Fatal("stale settings response restored unlocked credentials")
	}
	current, _ := c.adapter("near-testnet")
	if current != fresh {
		t.Fatal("stale settings changed current adapters")
	}
}

func TestTP041InvalidEndpointChangeIsAtomic(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{near.New(), aptos.New()}
	before, _ := c.adapter("near-testnet")
	state := readPublicState(t, path)
	state["settings"] = map[string]any{"near-testnet": map[string]string{"url": "http://127.0.0.1:3030"}, "aptos-testnet": map[string]string{"url": "http://remote.invalid/v1"}}
	writePublicState(t, path, state)
	if c.applySettings() == nil {
		t.Fatal("invalid endpoint settings accepted")
	}
	after, _ := c.adapter("near-testnet")
	if before != after {
		t.Fatal("failed settings application partially replaced adapter")
	}
}
