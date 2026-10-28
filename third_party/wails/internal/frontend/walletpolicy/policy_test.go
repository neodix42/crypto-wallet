package walletpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

func encoded(name string, args ...any) string {
	b, _ := json.Marshal(map[string]any{"name": name, "args": args, "callbackID": name + "-123.45"})
	return "C" + string(b)
}
func TestFacadeAllowlist(t *testing.T) {
	for _, name := range []string{"PublicBridge.Snapshot", "PrivateBridge.Prompt", "PrivateBridge.Copy", "PrivateBridge.Activity"} {
		t.Run(name, func(t *testing.T) {
			if !BrowserMessage(encoded("webui." + name)) {
				t.Fatal("required method rejected")
			}
			if BrowserMessage(encoded("webui."+name, "unexpected")) {
				t.Fatal("unexpected argument accepted")
			}
		})
	}
	for _, name := range []string{"PublicBridge.Event", "PrivateBridge.Reply"} {
		t.Run(name, func(t *testing.T) {
			if !BrowserMessage(encoded("webui."+name, map[string]any{"kind": "cancel"})) {
				t.Fatal("required method rejected")
			}
			if BrowserMessage(encoded("webui." + name)) {
				t.Fatal("missing argument accepted")
			}
		})
	}
	if !BrowserMessage("runtime:ready") {
		t.Fatal("initialization blocked")
	}
}
func TestRejectRuntimeEscapes(t *testing.T) {
	for _, message := range []string{"", "Q", "S", "H", "DomReady", "drag", "resize:n", "wails:showInspector", "LIsensitive", "EE{}", "Bhttps://example.com", "Wclose", "Ddata", "c{}", encoded(":wails:ClipboardGetText"), encoded(":wails:ClipboardSetText", "secret"), encoded(":wails:Environment"), encoded("webui.PrivateBridge.Dump"), encoded("webui.PublicBridge.Prompt"), encoded("webui.PrivateBridge.Event", map[string]any{}), encoded("webui.OtherBridge.Event", map[string]any{}), encoded("webui.PublicBridge.Event.secret", map[string]any{})} {
		t.Run(message[:min(len(message), 70)], func(t *testing.T) {
			if BrowserMessage(message) {
				t.Fatal("forbidden message accepted")
			}
		})
	}
}
func TestRejectMalformedEnvelopes(t *testing.T) {
	good := encoded("webui.PublicBridge.Snapshot")
	for _, message := range []string{"C", "C{", good + " {}", good + " garbage", strings.Replace(good, "\"name\":", "\"extra\":0,\"name\":", 1), strings.Replace(good, "123.45", "secret", 1), strings.Replace(good, "webui.PublicBridge.Snapshot-123.45", "123.45", 1), encoded("web-ui.PublicBridge.Snapshot"), "C" + strings.Repeat(" ", MaxMessageBytes)} {
		if BrowserMessage(message) {
			t.Fatal("invalid envelope accepted")
		}
	}
}
func FuzzBrowserMessage(f *testing.F) {
	f.Add(encoded("webui.PublicBridge.Snapshot"))
	f.Add("Bhttps://example.com")
	f.Add("C{")
	f.Fuzz(func(t *testing.T, message string) {
		allowed := BrowserMessage(message)
		if allowed && message != "runtime:ready" {
			if _, ok := BindingCall(message); !ok {
				t.Fatal("non-binding allowed")
			}
		}
	})
}

func TestRejectDuplicateEnvelopeAndTypedReplyKeys(t *testing.T) {
	good := encoded("webui.PrivateBridge.Reply", map[string]any{"promptId": "fixture", "accepted": false})
	for _, message := range []string{
		strings.Replace(good, `"name":`, `"name":"webui.PublicBridge.Snapshot","name":`, 1),
		strings.Replace(good, `"accepted":false`, `"accepted":false,"accepted":true`, 1),
		strings.Replace(good, `"accepted":false`, `"accepted":false,"values":["first"],"values":["second"]`, 1),
		strings.Replace(good, `"accepted":false`, `"accepted":false,"nested":{"x":1,"x":2}`, 1),
		"C" + strings.Repeat("[", 65) + strings.Repeat("]", 65),
	} {
		if BrowserMessage(message) {
			t.Fatal("duplicate or excessively nested raw envelope accepted")
		}
	}
}
