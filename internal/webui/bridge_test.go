package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"localwallet/internal/ipc"
	"localwallet/internal/uiprompt"
	"os"
	"strings"
	"testing"
	"time"
)

func publicFixture() View {
	return View{Title: "CryptoWallet", Active: "group/0", Pages: []Page{
		{ID: "group/0", Title: "Main account", Nodes: []Node{
			{Kind: "row", ID: "open/ethereum", Children: []Node{{Kind: "button", ID: "copy/ethereum", Text: "Copy"}}},
			{Kind: "entry", ID: "field/amount/ethereum", Value: "1"},
			{Kind: "combo", ID: "field/asset", Options: []Option{{ID: "eth", Text: "ETH"}}},
			{Kind: "check", ID: "field/showHidden"},
			{Kind: "column", Disabled: true, Children: []Node{{Kind: "button", ID: "unavailable"}}},
		}},
		{ID: "wallet/ethereum", Title: "Ethereum", Nodes: []Node{{Kind: "button", ID: "send/ethereum"}}},
	}}
}

func TestPublicBridgeAuthorizesOnlyCurrentEnabledControls(t *testing.T) {
	v := publicFixture()
	for _, e := range []Event{{ID: "open/ethereum"}, {ID: "copy/ethereum"}, {ID: "field/amount/ethereum", Value: "0.123456789123456789"}, {ID: "field/asset", Value: "eth"}, {ID: "field/showHidden", Value: "true"}, {ID: "tab", Value: "wallet/ethereum"}, {ID: "closeTab", Value: "wallet/ethereum"}, {ID: "lock"}, {ID: "close"}, {ID: "activity"}} {
		if !validEvent(v, e, false) {
			t.Fatalf("current public action rejected: %s", e.ID)
		}
	}
	for _, e := range []Event{{ID: "send/ethereum"}, {ID: "unavailable"}, {ID: "field/password", Value: "canary"}, {ID: "field/amount/ethereum", Value: strings.Repeat("x", 4097)}, {ID: "field/asset", Value: "foreign"}, {ID: "field/showHidden", Value: "yes"}, {ID: "copy/ethereum", Value: "foreign address"}, {ID: "tab", Value: "secret"}, {ID: "closeTab", Value: "group/0"}, {ID: "activity", Value: "typed private value"}, {ID: "lock", Value: "arg"}, {ID: "\x00"}} {
		if validEvent(v, e, false) {
			t.Fatalf("unknown/invalid public action accepted: %s", e.ID)
		}
	}
	if validEvent(v, Event{ID: "open/ethereum"}, true) || validEvent(v, Event{ID: "tab", Value: "wallet/ethereum"}, true) {
		t.Fatal("public navigation remained authorized during private modal")
	}
	if !validEvent(v, Event{ID: "lock"}, true) {
		t.Fatal("private modal blocked explicit lock")
	}
}

func TestStakingBridgeAcceptsOnlyVisibleTypedControlsAndNeverPromptPayloads(t *testing.T) {
	page := Page{ID: "wallet/near-mainnet:0", Title: "NEAR", Nodes: []Node{
		{Kind: "entry", ID: "field/stakingPool/near-mainnet:0", Value: "astro-stakers.poolv1.near"},
		{Kind: "entry", ID: "field/stakingAmount/near-mainnet:0", Value: "1.25"},
		{Kind: "button", ID: "stakingState/near-mainnet:0"},
		{Kind: "button", ID: "stakingPrepare/near-mainnet:0/stake"},
		{Kind: "button", ID: "stakingPrepare/near-mainnet:0/withdraw_all", Disabled: true},
	}}
	v := View{Active: page.ID, Pages: []Page{page}}
	for _, e := range []Event{{ID: "stakingState/near-mainnet:0"}, {ID: "stakingPrepare/near-mainnet:0/stake"}, {ID: "field/stakingPool/near-mainnet:0", Value: "other.poolv1.near"}, {ID: "field/stakingAmount/near-mainnet:0", Value: "0.000000000000000000000001"}} {
		if !validEvent(v, e, false) || validEvent(v, e, true) {
			t.Fatal("staking control did not follow current public/modal authority:", e.ID)
		}
	}
	for _, e := range []Event{{ID: "stakingPrepare/near-mainnet:0/withdraw_all"}, {ID: "stakingPrepare/near-mainnet:0/arbitrary"}, {ID: "stakingSend/near-mainnet:0"}, {ID: "stakingPrepare/near-mainnet:0/stake", Value: `{"gas":"1","method":"call","accepted":true}`}, {ID: "field/stakingPassword/near-mainnet:0", Value: "canary"}} {
		if validEvent(v, e, false) {
			t.Fatal("unavailable or payload-bearing staking authority accepted:", e.ID)
		}
	}
	v.Pages[0].Nodes = nil
	if validEvent(v, Event{ID: "stakingPrepare/near-mainnet:0/stake"}, false) {
		t.Fatal("stale staking action survived removal from current page")
	}
}

func TestQueuedEventsRecheckCurrentViewAndModalState(t *testing.T) {
	var received []Event
	w, _ := New(func(e Event) { received = append(received, e) })
	w.Render(publicFixture())
	b := &PublicBridge{window: w}
	if !b.Event(Event{ID: "copy/ethereum"}) {
		t.Fatal("enabled copy rejected")
	}
	w.mu.Lock()
	w.modal = true
	w.mu.Unlock()
	(<-w.queue)()
	if len(received) != 0 {
		t.Fatal("queued public action crossed private modal boundary")
	}
	if !b.Event(Event{ID: "lock"}) {
		t.Fatal("lock not queued as lifecycle control")
	}
	(<-w.controls)()
	if len(received) != 1 || received[0].ID != "lock" {
		t.Fatal("lock lifecycle event missing")
	}
	w.Close()
	if b.Event(Event{ID: "activity"}) {
		t.Fatal("closed view accepted event")
	}
}

func TestSnapshotOwnsMutablePublicTreeAndRetainsNoInvalidView(t *testing.T) {
	w, _ := New(nil)
	v := publicFixture()
	w.Render(v)
	v.Pages[0].Nodes[0].Children[0].Text = "changed caller"
	b := &PublicBridge{window: w}
	a := b.Snapshot()
	a.Pages[0].Nodes[0].Children[0].Text = "changed response"
	if b.Snapshot().Pages[0].Nodes[0].Children[0].Text != "Copy" {
		t.Fatal("caller/renderer mutated public state")
	}
	bad := publicFixture()
	bad.Pages[0].Nodes = append(bad.Pages[0].Nodes, Node{Kind: "entry", ID: "field/api_key"})
	w.Render(bad)
	if len(b.Snapshot().Pages[0].Nodes) != 5 {
		t.Fatal("secret-like field entered public representation")
	}
	w.Close()
}

func TestPublicQueueBoundsAndActivityThrottle(t *testing.T) {
	w, _ := New(nil)
	w.Render(publicFixture())
	b := &PublicBridge{window: w}
	if !b.Event(Event{ID: "activity"}) || b.Event(Event{ID: "activity"}) {
		t.Fatal("activity was absent or unbounded")
	}
	for len(w.queue) < cap(w.queue) {
		if !b.Event(Event{ID: "copy/ethereum"}) {
			t.Fatal("queue rejected before capacity")
		}
	}
	if b.Event(Event{ID: "copy/ethereum"}) {
		t.Fatal("public queue grew without bound")
	}
	if !b.Event(Event{ID: "lock"}) {
		t.Fatal("full ordinary queue prevented lifecycle lock")
	}
	w.Close()
}

func promptFixture() uiprompt.Prompt {
	return uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "input", Title: "Private input", Labels: []string{"Password"}}
}

func TestPrivateBridgeDeliveryIdentityReplayAndClear(t *testing.T) {
	var output bytes.Buffer
	b := &PrivateBridge{prompt: promptFixture(), channel: &ipc.Channel{W: &output}, session: strings.Repeat("b", 32)}
	if b.Copy() || b.Activity() || b.Reply(uiprompt.Reply{PromptID: b.prompt.ID}) {
		t.Fatal("undelivered private view accepted privileged action")
	}
	p, err := b.Prompt()
	if err != nil || p.ID != b.prompt.ID {
		t.Fatal("private request not delivered")
	}
	p.Labels[0] = "mutated"
	if b.prompt.Labels[0] != "Password" {
		t.Fatal("private response aliases stored mutable prompt")
	}
	if _, err = b.Prompt(); err == nil {
		t.Fatal("private view delivered twice")
	}
	if b.Reply(uiprompt.Reply{PromptID: strings.Repeat("c", 32), Accepted: true, Values: []string{"synthetic"}}) {
		t.Fatal("foreign reply accepted")
	}
	if b.Reply(uiprompt.Reply{PromptID: b.prompt.ID, Accepted: true, Values: []string{strings.Repeat("x", 4097)}}) {
		t.Fatal("oversize private value accepted")
	}
	if !b.Reply(uiprompt.Reply{PromptID: b.prompt.ID, Accepted: true, Values: []string{"synthetic-private-value"}}) {
		t.Fatal("valid private reply rejected")
	}
	if b.Copy() || b.Activity() || b.Reply(uiprompt.Reply{PromptID: p.ID}) {
		t.Fatal("completed private view replayed")
	}
	m, err := (&ipc.Channel{R: &output}).Read()
	if err != nil || m.ID != p.ID || m.Session != b.session || m.Method != "reply" {
		t.Fatal("private reply identity changed")
	}
	r, err := uiprompt.DecodeReply(m.Body, p.Prompt)
	clear(m.Body)
	if err != nil || len(r.Values) != 1 || r.Values[0] != "synthetic-private-value" {
		t.Fatal("private pipe reply corrupted")
	}
	if b.prompt.Labels != nil || b.prompt.Phrase != "" || b.prompt.PNG != nil {
		t.Fatal("completed prompt retained adapter buffers")
	}
}

func TestPrivateDecoderRejectsAmbiguityAndCancellationPayload(t *testing.T) {
	p := promptFixture()
	for _, raw := range []string{
		`{"promptId":"` + p.ID + `","accepted":false,"accepted":true}`,
		`{"promptId":"` + p.ID + `","accepted":false,"values":["private"]}`,
		`{"promptId":"` + p.ID + `","accepted":true,"values":null}`,
	} {
		if _, err := decodePrivate([]byte(raw), p); err == nil {
			t.Fatal("ambiguous or unbound reply accepted")
		}
	}
	raw, _ := json.Marshal(uiprompt.Reply{PromptID: p.ID})
	if _, err := decodePrivate(raw, p); err != nil {
		t.Fatal("clean cancellation rejected")
	}
}

func TestClosedWindowNeverStartsPrivateChildAndInvalidReceiveRejected(t *testing.T) {
	w, _ := New(nil)
	w.Close()
	if _, err := w.Prompt(promptFixture()); err == nil {
		t.Fatal("closed desktop started private prompt")
	}
	p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "receive", Title: "Receive", Address: "public", PNG: []byte("not PNG")}
	if _, err := checkedPrompt(p); err == nil {
		t.Fatal("malformed receive pixels entered engine")
	}
	if _, err := checkedPrompt(uiprompt.Prompt{}); err == nil {
		t.Fatal("unbound private request accepted")
	}
}

func TestRoleAssetsRestrictDocumentsAndOptionsDisableDebugFeatures(t *testing.T) {
	for _, private := range []bool{false, true} {
		assets := roleAssets{private: private}
		root, err := assets.Open(".")
		if err != nil {
			t.Fatal("embedded role root absent")
		}
		info, err := root.Stat()
		root.Close()
		if err != nil || !info.IsDir() {
			t.Fatal("asset server root must be a directory, not the entry document")
		}
		f, err := assets.Open("index.html")
		if err != nil {
			t.Fatal("compiled role entry absent")
		}
		raw, _ := io.ReadAll(f)
		f.Close()
		if !bytes.Contains(raw, []byte("Content-Security-Policy")) {
			t.Fatal("compiled entry lacks CSP")
		}
		for _, name := range []string{"secret/index.html", "../README.md", "/index.html", "assets/../secret/index.html", "frontend/dist/index.html", "index.html/.."} {
			if f, err := assets.Open(name); err == nil {
				f.Close()
				t.Fatalf("role asset path escaped: %s", name)
			}
		}
		opts := appOptions(private)
		if opts.EnableDefaultContextMenu || opts.EnableFraudulentWebsiteDetection || !opts.DragAndDrop.DisableWebViewDrop || opts.BindingsAllowedOrigins != "" {
			t.Fatal("unsafe Wails feature enabled")
		}
		if len(opts.Bind) != 0 {
			t.Fatal("common options exposed privileged bridge")
		}
	}
}

func TestPrivateChildEnvironmentExcludesInjectionAndSecrets(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "synthetic-injection")
	t.Setenv("LD_PRELOAD", "synthetic-injection")
	t.Setenv("APP_PASSWORD", "synthetic-private")
	t.Setenv("WEBKIT_INSPECTOR_SERVER", "127.0.0.1:9222")
	t.Setenv("GDK_BACKEND", "arbitrary")
	for _, entry := range promptEnvironment() {
		if strings.Contains(entry, "synthetic") || strings.HasPrefix(entry, "GDK_BACKEND=") || strings.HasPrefix(entry, "WEBKIT_INSPECTOR_SERVER=") {
			t.Fatal("injection or secret inherited by private child")
		}
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	image, err := os.Open("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if checkPrivatePipes(read, write, image) != nil {
		t.Fatal("owned inherited private handles rejected")
	}
	if checkPrivatePipes(image, write, image) == nil || checkPrivatePipes(read, write, read) == nil {
		t.Fatal("nonpipe input or foreign image accepted")
	}
}

func TestPrivateActivityIsValueFreeBoundedAndRevoked(t *testing.T) {
	var output bytes.Buffer
	b := &PrivateBridge{prompt: promptFixture(), delivered: true, channel: &ipc.Channel{W: &output}, session: strings.Repeat("b", 32)}
	if !b.Activity() || b.Activity() {
		t.Fatal("private activity pulse absent or unbounded")
	}
	m, err := (&ipc.Channel{R: &output}).Read()
	if err != nil || m.Method != "activity" || string(m.Body) != "{}" {
		t.Fatal("private activity carried values")
	}
	b.lastActivity = time.Now().Add(-2 * time.Second)
	b.finished = true
	if b.Activity() {
		t.Fatal("completed private view emitted activity")
	}
}

func TestWebKitStartupRemovesInspectorAndEngineInjectionBeforeInitialization(t *testing.T) {
	for _, key := range []string{"WEBKIT_INSPECTOR_SERVER", "WEBKIT_INSPECTOR_HTTP_SERVER", "WEBKIT_FORCE_SANDBOX", "JSC_SIGNAL_FOR_GC", "JSC_dumpOptions", "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "GIO_EXTRA_MODULES", "GIO_MODULE_DIR", "GTK_PATH", "GTK_MODULES"} {
		t.Setenv(key, "synthetic-runtime-injection")
	}
	t.Setenv("LANG", "en_US.UTF-8")
	prepareWebKitRuntime()
	for _, entry := range os.Environ() {
		if strings.Contains(entry, "synthetic-runtime-injection") {
			t.Fatal("runtime injection survived initialization boundary")
		}
	}
	if os.Getenv("LANG") != "en_US.UTF-8" {
		t.Fatal("runtime preparation changed ordinary locale")
	}
}

func TestPrivateCopyTransportsOnlyOneUseIntentAndChecksParentAcknowledgment(t *testing.T) {
	read, write := io.Pipe()
	defer read.Close()
	defer write.Close()
	phrase := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	b := &PrivateBridge{prompt: uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "recoveryBackup", Title: "Back up recovery phrase", Phrase: phrase}, ctx: context.Background(), delivered: true, session: strings.Repeat("b", 32), channel: &ipc.Channel{W: write}}
	result := make(chan bool, 1)
	go func() { result <- b.Copy() }()
	m, err := (&ipc.Channel{R: read}).Read()
	if err != nil || m.Method != "copy" || string(m.Body) != `{"sequence":1}` || m.ID != b.prompt.ID || m.Session != b.session || bytes.Contains(m.Body, []byte("abandon")) {
		t.Fatal("copy intent included secret text, parameters or wrong identity")
	}
	if b.Copy() {
		t.Fatal("second copy overlapped unresolved intent")
	}
	ack := ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: "copyResult", Body: []byte(`{"sequence":1,"copied":true}`)}
	for _, bad := range []ipc.Message{
		{ID: m.ID, Session: "foreign", Method: "copyResult", Body: ack.Body},
		{ID: m.ID, Session: m.Session, Method: "reply", Body: ack.Body},
		{ID: m.ID, Session: m.Session, Method: "copyResult", Body: []byte(`{"sequence":2,"copied":true}`)},
		{ID: m.ID, Session: m.Session, Method: "copyResult", Body: []byte(`{"sequence":1}`)},
		{ID: m.ID, Session: m.Session, Method: "copyResult", Body: []byte(`{"sequence":1,"copied":false,"copied":true}`)},
	} {
		if b.parentMessage(bad) {
			t.Fatal("copy acknowledgment was unbound or ambiguous")
		}
	}
	if !b.parentMessage(ack) {
		t.Fatal("matching private copy acknowledgment rejected")
	}
	if !<-result {
		t.Fatal("actual parent commit acknowledgment lost")
	}
	if b.parentMessage(ack) {
		t.Fatal("copy acknowledgment replayed")
	}
	go func() { result <- b.Copy() }()
	if _, err := (&ipc.Channel{R: read}).Read(); err != nil {
		t.Fatal("second sequential intent missing")
	}
	b.mu.Lock()
	b.revokeLocked()
	b.mu.Unlock()
	if <-result {
		t.Fatal("revoked pending copy succeeded")
	}
	if b.Copy() {
		t.Fatal("closed private view copied")
	}
}

func TestCancellationAndCloseWaitForPrivateProcessRetirement(t *testing.T) {
	for _, closing := range []bool{false, true} {
		w, _ := New(nil)
		cancelled, reaped, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		w.activeCancel = func() { close(cancelled) }
		w.activeDone = reaped
		go func() {
			if closing {
				w.Close()
			} else {
				w.CancelPrompt()
			}
			close(returned)
		}()
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("private cancellation not initiated")
		}
		select {
		case <-returned:
			t.Fatal("lock/close returned before private process was reaped")
		default:
		}
		close(reaped)
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("completed private retirement blocked close")
		}
		w.mu.Lock()
		w.activeCancel = nil
		w.activeDone = nil
		w.mu.Unlock()
		w.Close()
	}
}
