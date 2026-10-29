package webui

import (
	"bytes"
	"encoding/json"
	"io"
	"localwallet/internal/uiprompt"
	"os"
	"strings"
	"testing"
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

func TestDialogDecoderRejectsAmbiguityAndCancellationPayload(t *testing.T) {
	p := promptFixture()
	for _, raw := range []string{
		`{"promptId":"` + p.ID + `","accepted":false,"accepted":true}`,
		`{"promptId":"` + p.ID + `","accepted":false,"values":["private"]}`,
		`{"promptId":"` + p.ID + `","accepted":true,"values":null}`,
	} {
		if _, err := decodeDialogReply([]byte(raw), p); err == nil {
			t.Fatal("ambiguous or unbound reply accepted")
		}
	}
	raw, _ := json.Marshal(uiprompt.Reply{PromptID: p.ID})
	if _, err := decodeDialogReply(raw, p); err != nil {
		t.Fatal("clean cancellation rejected")
	}
}

func TestClosedWindowNeverStartsDialogAndInvalidReceiveRejected(t *testing.T) {
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

func TestSingleDocumentAssetsKeepEmbeddedOriginAndNormalContextMenu(t *testing.T) {
	assets := roleAssets{}
	file, err := assets.Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(file)
	file.Close()
	if !bytes.Contains(raw, []byte("Content-Security-Policy")) {
		t.Fatal("single entry lost content policy")
	}
	for _, name := range []string{"secret/index.html", "../README.md", "/index.html", "assets/../secret/index.html", "frontend/dist/index.html", "index.html/.."} {
		if file, err := assets.Open(name); err == nil {
			file.Close()
			t.Fatal("untrusted asset path accepted", name)
		}
	}
	opts := appOptions()
	if !opts.EnableDefaultContextMenu || opts.EnableFraudulentWebsiteDetection || !opts.DragAndDrop.DisableWebViewDrop || len(opts.Bind) != 0 {
		t.Fatal("single UI options changed unintended browser features")
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
