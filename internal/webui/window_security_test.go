package webui

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublicModelRejectsMalformedAndExcessiveContentAtomically(t *testing.T) {
	cases := []struct {
		name   string
		change func(*View)
	}{
		{"title length", func(v *View) { v.Title = strings.Repeat("a", 257) }},
		{"status length", func(v *View) { v.Status = strings.Repeat("a", 65537) }},
		{"active length", func(v *View) { v.Active = strings.Repeat("a", 257) }},
		{"invalid UTF8", func(v *View) { v.Title = string([]byte{0xff}) }},
		{"embedded NUL", func(v *View) { v.Status = "public\x00hidden" }},
		{"page count", func(v *View) { v.Pages = make([]Page, 1001) }},
		{"empty page ID", func(v *View) { v.Pages[0].ID = "" }},
		{"duplicate page ID", func(v *View) { v.Pages[1].ID = v.Pages[0].ID }},
		{"page ID length", func(v *View) { v.Pages[0].ID = strings.Repeat("a", 257) }},
		{"page title length", func(v *View) { v.Pages[0].Title = strings.Repeat("a", 257) }},
		{"node ID length", func(v *View) { v.Pages[0].Nodes[0].ID = strings.Repeat("a", 257) }},
		{"node text length", func(v *View) { v.Pages[0].Nodes[0].Text = strings.Repeat("a", 65537) }},
		{"node tooltip length", func(v *View) { v.Pages[0].Nodes[0].Tooltip = strings.Repeat("a", 65537) }},
		{"node value length", func(v *View) { v.Pages[0].Nodes[0].Value = strings.Repeat("a", 4097) }},
		{"unsupported widget", func(v *View) { v.Pages[0].Nodes[0].Kind = "password" }},
		{"option count", func(v *View) { v.Pages[0].Nodes[2].Options = make([]Option, 10001) }},
		{"option ID length", func(v *View) { v.Pages[0].Nodes[2].Options[0].ID = strings.Repeat("a", 4097) }},
		{"option text length", func(v *View) { v.Pages[0].Nodes[2].Options[0].Text = strings.Repeat("a", 4097) }},
		{"node count", func(v *View) {
			v.Pages[0].Nodes = make([]Node, 20001)
			for i := range v.Pages[0].Nodes {
				v.Pages[0].Nodes[i].Kind = "label"
			}
		}},
		{"nesting depth", func(v *View) {
			n := Node{Kind: "label"}
			for i := 0; i < 32; i++ {
				n = Node{Kind: "column", Children: []Node{n}}
			}
			v.Pages[0].Nodes = []Node{n}
		}},
	}
	for _, secret := range []string{"PASSWORD", "phrase", "mnemonic", "secret", "apikey", "api_key", "credential", "passphrase"} {
		cases = append(cases, struct {
			name   string
			change func(*View)
		}{"private entry " + secret, func(v *View) {
			v.Pages[0].Nodes = append(v.Pages[0].Nodes, Node{Kind: "entry", ID: "field/" + secret})
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := New(nil)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			w.Render(publicFixture())
			bridge := &PublicBridge{window: w}
			before := bridge.Snapshot()
			malformed := publicFixture()
			tc.change(&malformed)
			w.Render(malformed)
			if after := bridge.Snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatal("invalid model replaced or partially modified the current public view")
			}
		})
	}
}

func TestPublicModelAcceptsBoundedPublicContentWithoutInterpretingMarkup(t *testing.T) {
	w, _ := New(nil)
	defer w.Close()
	view := publicFixture()
	view.Title = strings.Repeat("é", 128)
	view.Status = strings.Repeat("a", 65536)
	view.Pages[0].Nodes = []Node{{Kind: "combo", ID: "field/choice", Options: []Option{{ID: strings.Repeat("a", 4096), Text: "<script>inert public text</script>"}}}}
	w.Render(view)
	bridge := &PublicBridge{window: w}
	if snapshot := bridge.Snapshot(); !reflect.DeepEqual(snapshot, cloneView(view)) {
		t.Fatal("bounded UTF8/public text was altered or rejected")
	}
	view.Pages[0].Nodes[0].Options[0].Text = "caller mutation"
	snapshot := bridge.Snapshot()
	snapshot.Pages[0].Nodes[0].Options[0].ID = "renderer mutation"
	actual := bridge.Snapshot().Pages[0].Nodes[0].Options[0]
	if actual.Text != "<script>inert public text</script>" || len(actual.ID) != 4096 {
		t.Fatal("public option tree shares mutable storage across a boundary")
	}
	w.Close()
	w.Render(View{Title: "late render"})
	if bridge.Snapshot().Title != strings.Repeat("é", 128) {
		t.Fatal("render after close changed the last public state")
	}
}

func TestDispatcherSerializesCallbacksAndPrioritizesLifecycleWork(t *testing.T) {
	w, _ := New(nil)
	defer w.Close()
	w.Dispatch(nil)
	w.DispatchControl(nil)
	if len(w.queue) != 0 || len(w.controls) != 0 {
		t.Fatal("nil callback consumed queue capacity")
	}
	order := make(chan string, 4)
	paused, resume := make(chan struct{}), make(chan struct{})
	w.Dispatch(func() {
		order <- "ordinary"
		close(paused)
		<-resume
	})
	w.DispatchControl(func() { order <- "lifecycle" })
	stopped := make(chan struct{})
	go func() { w.loop(); close(stopped) }()
	if got := awaitWindowValue(t, order); got != "lifecycle" {
		t.Fatal("queued ordinary work ran before lifecycle control")
	}
	if got := awaitWindowValue(t, order); got != "ordinary" {
		t.Fatal("ordinary work was not dispatched")
	}
	<-paused
	w.Dispatch(func() { order <- "next" })
	select {
	case <-order:
		t.Fatal("controller callbacks ran concurrently")
	default:
	}
	close(resume)
	if got := awaitWindowValue(t, order); got != "next" {
		t.Fatal("queued callback was lost")
	}
	w.Close()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("closed desktop left its dispatcher running")
	}
}

func TestBlockedDispatchersRetireWhenWindowCloses(t *testing.T) {
	for _, lifecycle := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "lifecycle"}[lifecycle], func(t *testing.T) {
			w, _ := New(nil)
			defer w.Close()
			queue, dispatch := w.queue, w.Dispatch
			if lifecycle {
				queue, dispatch = w.controls, w.DispatchControl
			}
			for len(queue) < cap(queue) {
				queue <- func() { t.Error("queued work unexpectedly executed without a loop") }
			}
			returned := make(chan struct{})
			go func() { dispatch(func() {}); close(returned) }()
			select {
			case <-returned:
				t.Fatal("full queue did not apply backpressure")
			case <-time.After(10 * time.Millisecond):
			}
			w.Close()
			select {
			case <-returned:
			case <-time.After(3 * time.Second):
				t.Fatal("closed desktop stranded a blocked dispatcher")
			}
		})
	}
}

func TestIdleDispatcherWakesForLifecycleControlAndClose(t *testing.T) {
	w, _ := New(nil)
	defer w.Close()
	stopped := make(chan struct{})
	go func() { w.loop(); close(stopped) }()
	// Let the owned loop reach its blocking idle select. Lifecycle work must
	// wake it without requiring an ordinary public action to arrive first.
	time.Sleep(10 * time.Millisecond)
	processed := make(chan string, 1)
	w.DispatchControl(func() { processed <- "idle control" })
	if awaitWindowValue(t, processed) != "idle control" {
		t.Fatal("idle lifecycle control was not delivered")
	}
	time.Sleep(10 * time.Millisecond)
	w.Close()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("close failed to wake the idle dispatcher")
	}
	closed, _ := New(nil)
	closed.Close()
	retired := make(chan struct{})
	go func() { closed.loop(); close(retired) }()
	select {
	case <-retired:
	case <-time.After(3 * time.Second):
		t.Fatal("already-closed desktop started a persistent dispatcher")
	}
}

func awaitWindowValue(t *testing.T, values <-chan string) string {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("desktop callback did not complete")
		return ""
	}
}

func TestPublicBridgeRechecksDisabledRemovedAndClosedTargetsAtDispatch(t *testing.T) {
	for _, boundary := range []string{"disabled", "removed", "inactive", "closed"} {
		t.Run(boundary, func(t *testing.T) {
			received := make(chan Event, 1)
			w, _ := New(func(e Event) { received <- e })
			defer w.Close()
			w.Render(publicFixture())
			bridge := &PublicBridge{window: w}
			if !bridge.Event(Event{ID: "copy/ethereum"}) {
				t.Fatal("current copy control was rejected")
			}
			view := publicFixture()
			switch boundary {
			case "disabled":
				view.Pages[0].Nodes[0].Disabled = true
			case "removed":
				view.Pages[0].Nodes = nil
			case "inactive":
				view.Active = "wallet/ethereum"
			case "closed":
				w.Close()
			}
			w.Render(view)
			(<-w.queue)()
			select {
			case <-received:
				t.Fatal("stale queued action crossed its authorization boundary")
			default:
			}
		})
	}
}

func TestPublicBridgeDeliversTypedValuesAndBoundsTheControlQueue(t *testing.T) {
	received := make(chan Event, 10)
	w, _ := New(func(e Event) { received <- e })
	defer w.Close()
	w.Render(publicFixture())
	bridge := &PublicBridge{window: w}
	for _, event := range []Event{{ID: "field/showHidden", Value: "false"}, {ID: "field/asset", Value: "eth"}, {ID: "field/amount/ethereum", Value: "0.25"}} {
		if !bridge.Event(event) {
			t.Fatal("enabled typed public input was rejected")
		}
		(<-w.queue)()
		if actual := <-received; actual != event {
			t.Fatal("public input was transformed or misrouted")
		}
	}
	view := publicFixture()
	view.Active = "missing/current-page"
	w.Render(view)
	if bridge.Event(Event{ID: "open/ethereum"}) {
		t.Fatal("missing active page exposed a control from another page")
	}
	for len(w.controls) < cap(w.controls) {
		if !bridge.Event(Event{ID: "lock"}) {
			t.Fatal("lifecycle queue rejected before its capacity")
		}
	}
	if bridge.Event(Event{ID: "close"}) {
		t.Fatal("renderer grew lifecycle work beyond the fixed queue bound")
	}
	w.Close()
	if bridge.Event(Event{ID: "lock"}) || bridge.Event(Event{ID: "field/asset", Value: "eth"}) {
		t.Fatal("closed desktop accepted a public action")
	}
	// A nil callback still consumes a valid action without acquiring authority.
	nilWindow, _ := New(nil)
	defer nilWindow.Close()
	nilWindow.Render(publicFixture())
	if !(&PublicBridge{window: nilWindow}).Event(Event{ID: "copy/ethereum"}) {
		t.Fatal("valid event was rejected solely because there is no subscriber")
	}
	(<-nilWindow.queue)()
}

func TestAssetErrorsAndFrameworkFormattingNeverExposeUnderlyingValues(t *testing.T) {
	for _, private := range []bool{false, true} {
		assets := roleAssets{private: private}
		for _, name := range []string{"assets/missing.js", "assets\\secret.js", "assets/./index.js", "secret", "../index.html"} {
			if file, err := assets.Open(name); !errors.Is(err, fs.ErrNotExist) {
				if file != nil {
					file.Close()
				}
				t.Fatal("missing or escaped asset did not fail as unavailable")
			}
		}
		opts := appOptions(private)
		if formatted := opts.ErrorFormatter(errors.New("synthetic-private-diagnostic")); formatted != "Wallet request unavailable." {
			t.Fatal("framework exposed an underlying diagnostic")
		}
	}
}
