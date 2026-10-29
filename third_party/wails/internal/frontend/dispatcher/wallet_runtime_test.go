package dispatcher

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/wailsapp/wails/v2/internal/frontend"
)

type clipboardFrontend struct {
	frontend.Frontend
	text string
	err  error
}

func (f *clipboardFrontend) ClipboardSetText(text string) error { f.text = text; return f.err }
func (f *clipboardFrontend) ClipboardGetText() (string, error)  { return f.text, f.err }

func TestTrustedUIUsesOrdinaryWailsClipboardRuntime(t *testing.T) {
	sender := &clipboardFrontend{}
	d := &Dispatcher{}
	for _, name := range []string{"ClipboardSetText", "ClipboardGetText"} {
		args := []any{}
		if name == "ClipboardSetText" {
			args = []any{"public-normal-clipboard-fixture"}
		}
		raw, _ := json.Marshal(map[string]any{"name": ":wails:" + name, "args": args, "callbackID": "normal-runtime-call"})
		reply, err := d.ProcessMessage("C"+string(raw), sender)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Result any `json:"result"`
			Error  any `json:"error"`
		}
		if json.Unmarshal([]byte(reply[1:]), &result) != nil || result.Error != nil {
			t.Fatal("ordinary runtime call failed")
		}
		if name == "ClipboardSetText" && result.Result != true {
			t.Fatal("copy not acknowledged")
		}
		if name == "ClipboardGetText" && result.Result != "public-normal-clipboard-fixture" {
			t.Fatal("paste returned another value")
		}
	}
	sender.err = errors.New("synthetic-native-failure")
	raw := `C{"name":":wails:ClipboardSetText","args":["public"],"callbackID":"normal-runtime-call"}`
	reply, err := d.ProcessMessage(raw, sender)
	if err != nil || reply == "" {
		t.Fatal("native error did not return a callback")
	}
}
