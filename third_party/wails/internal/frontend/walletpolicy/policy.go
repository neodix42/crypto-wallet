// Package walletpolicy is the wallet-specific, default-deny browser bridge policy.
// It contains no GTK dependency, so every allowed transport message is testable.
package walletpolicy

import (
	"encoding/json"
	"io"
	"strings"
)

const MaxMessageBytes = 256 * 1024

type Call struct {
	Name       string            `json:"name"`
	Args       []json.RawMessage `json:"args"`
	CallbackID string            `json:"callbackID"`
}

// BrowserMessage permits only fixed initialization and named facade methods.
// In particular no generic Wails runtime, obfuscated call, log, clipboard,
// URL opener, notification, window, drag, file or event message reaches Go.
func BrowserMessage(message string) bool {
	if message == "runtime:ready" {
		return true
	}
	_, ok := BindingCall(message)
	return ok
}

// BindingCall validates the entire envelope before it reaches the dispatcher.
// Public and private facades are bound in separate application processes.
func BindingCall(message string) (Call, bool) {
	var call Call
	if len(message) < 2 || len(message) > MaxMessageBytes || message[0] != 'C' {
		return call, false
	}
	if !uniqueJSON(message[1:]) {
		return Call{}, false
	}
	decoder := json.NewDecoder(strings.NewReader(message[1:]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&call) != nil || decoder.More() {
		return Call{}, false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return Call{}, false
	}
	parts := strings.Split(call.Name, ".")
	if len(parts) != 3 || !identifier(parts[0]) {
		return Call{}, false
	}
	argCount := -1
	switch parts[1] + "." + parts[2] {
	case "PublicBridge.Snapshot", "PrivateBridge.Prompt", "PrivateBridge.Copy", "PrivateBridge.Activity":
		argCount = 0
	case "PublicBridge.Event", "PrivateBridge.Reply":
		argCount = 1
	}
	if argCount < 0 || len(call.Args) != argCount {
		return Call{}, false
	}
	if len(call.CallbackID) > 160 || !strings.HasPrefix(call.CallbackID, call.Name+"-") {
		return Call{}, false
	}
	suffix := strings.TrimPrefix(call.CallbackID, call.Name+"-")
	if suffix == "" {
		return Call{}, false
	}
	for _, char := range suffix {
		if !(char >= '0' && char <= '9') && char != '.' {
			return Call{}, false
		}
	}
	return call, true
}

func identifier(value string) bool {
	if value == "" {
		return false
	}
	for i, c := range value {
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && c != '_' && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// Reflection would otherwise discard duplicate keys before typed facade methods
// can validate them. Check the original envelope and all nested arguments here.
func uniqueJSON(raw string) bool {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			close, err := decoder.Token()
			return err == nil && close == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			close, err := decoder.Token()
			return err == nil && close == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
