package nativeui

import (
	"bytes"
	"errors"
	"localwallet/internal/uiprompt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPublicViewIsBoundedPlainTextAndNeverASecretInput(t *testing.T) {
	v := View{Title: "CryptoWallet", Active: "main", Pages: []Page{{ID: "main", Title: "Main account", Nodes: []Node{
		{Kind: "heading", Text: "Plain <b>text</b> & diagnostics"},
		{Kind: "address", Value: "0x0000000000000000000000000000000000000001"},
		{Kind: "entry", ID: "sendAmount", Value: "1.25"},
		{Kind: "button", ID: "backupPhrase", Text: "Back up recovery phrase"},
		{Kind: "label", Text: "1.5", Tooltip: "Provider observed at public block 7"},
	}}}}
	if !validateView(v) {
		t.Fatal("valid public presentation rejected")
	}
	for _, id := range []string{"password", "appPassword", "mnemonic", "phraseWords", "API_key", "apiSecret", "credentialValue", "bip39Passphrase"} {
		bad := cloneView(v)
		bad.Pages[0].Nodes[2].ID = id
		if validateView(bad) {
			t.Fatal("secret input could reach public events")
		}
	}
	for _, mutate := range []func(*View){
		func(v *View) { v.Title = "bad\x00title" },
		func(v *View) { v.Title = string([]byte{255}) },
		func(v *View) { v.Pages = append(v.Pages, v.Pages[0]) },
		func(v *View) { v.Pages[0].Nodes[0].Kind = "html" },
		func(v *View) { v.Pages[0].Nodes[0].Text = strings.Repeat("x", 65537) },
		func(v *View) { v.Pages[0].Nodes[0].Tooltip = "bad\x00tooltip" },
		func(v *View) { v.Pages[0].Nodes[2].Value = strings.Repeat("x", 4097) },
	} {
		bad := cloneView(v)
		mutate(&bad)
		if validateView(bad) {
			t.Fatal("unsafe/unbounded view accepted")
		}
	}
}

func TestPublicTreeRejectsResourceExhaustionAndInvalidNestedOptions(t *testing.T) {
	base := View{Pages: []Page{{ID: "main", Nodes: []Node{{Kind: "combo", Options: []Option{{ID: "ok", Text: "ok"}}}}}}}
	bad := cloneView(base)
	bad.Pages[0].Nodes[0].Options[0].Text = "bad\x00option"
	if validateView(bad) {
		t.Fatal("invalid option reached GTK")
	}
	deep := Node{Kind: "label", Text: "public leaf"}
	for range 34 {
		deep = Node{Kind: "column", Children: []Node{deep}}
	}
	bad = cloneView(base)
	bad.Pages[0].Nodes = []Node{deep}
	if validateView(bad) {
		t.Fatal("unbounded recursive tree accepted")
	}
	bad = cloneView(base)
	bad.Pages[0].Nodes = make([]Node, 20001)
	for i := range bad.Pages[0].Nodes {
		bad.Pages[0].Nodes[i].Kind = "label"
	}
	if validateView(bad) {
		t.Fatal("unbounded widget count accepted")
	}
	bad = cloneView(base)
	bad.Pages[0].Nodes[0].Options = make([]Option, 10001)
	if validateView(bad) {
		t.Fatal("unbounded choice count accepted")
	}
	bad = cloneView(base)
	bad.Pages = make([]Page, 1001)
	if validateView(bad) {
		t.Fatal("unbounded page count accepted")
	}
}

func TestClosedAndInvalidPrivateRequestsCannotReachGTK(t *testing.T) {
	w := &Window{closed: true, thread: -1}
	if _, err := w.Prompt(uiprompt.Prompt{}); !errors.Is(err, uiprompt.ErrProtocol) {
		t.Fatal("invalid private prompt accepted")
	}
	p := uiprompt.Prompt{ID: "0123456789abcdef0123456789abcdef", Kind: "confirm", Title: "Public fixture", Summary: "Public approval"}
	if _, err := w.Prompt(p); !errors.Is(err, uiprompt.ErrCancelled) {
		t.Fatal("closed window queued a private prompt")
	}
	w.promptBusy.Store(true)
	if _, err := w.Prompt(p); !errors.Is(err, uiprompt.ErrProtocol) {
		t.Fatal("concurrent prompt accepted")
	}
	w.promptBusy.Store(false)
	w.Dispatch(nil)
	w.Dispatch(func() { t.Error("closed window dispatched public work") })
	w.DispatchControl(nil)
	w.control(func() { t.Error("closed window dispatched lifecycle work") })
	w.Render(View{})
	w.render(View{})
	w.Run()
	if len(w.queue) != 0 || len(w.controls) != 0 {
		t.Fatal("closed window retained callbacks")
	}
}

func TestNativeInitFailsClosedWithoutAnAvailableDisplay(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_NO_DISPLAY_CHILD") == "1" {
		if _, err := New(nil); err == nil {
			t.Fatal("native initialization accepted missing display")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeInitFailsClosedWithoutAnAvailableDisplay$")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "DISPLAY=", "GDK_BACKEND=x11", "CRYPTOWALLET_NO_DISPLAY_CHILD=1"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("display-unavailable subprocess failed: %s", out)
	}
}

func TestRenderSnapshotDoesNotShareControllerTrees(t *testing.T) {
	v := View{Pages: []Page{{ID: "one", Nodes: []Node{{Kind: "column", Children: []Node{{Kind: "combo", ID: "asset", Options: []Option{{ID: "eth", Text: "ETH"}}}}}}}}}
	copy := cloneView(v)
	v.Pages[0].ID = "changed"
	v.Pages[0].Nodes[0].Children[0].Options[0].ID = "changed"
	if copy.Pages[0].ID != "one" || copy.Pages[0].Nodes[0].Children[0].Options[0].ID != "eth" {
		t.Fatal("render snapshot retained mutable controller slices")
	}
}

func TestNumberedPhraseClearsTemporarySourceAndPreservesWords(t *testing.T) {
	phrase := []byte("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about")
	numbered := numberPhrase(phrase)
	defer clear(numbered)
	if !bytes.Equal(phrase, make([]byte, len(phrase))) || !bytes.HasPrefix(numbered, []byte("1. abandon\n")) || !bytes.HasSuffix(numbered, []byte("12. about\n")) || bytes.Count(numbered, []byte("\n")) != 12 {
		t.Fatal("numbering or temporary phrase cleanup failed")
	}
}
