package uiprompt

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const fixturePhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
const fixtureID = "0123456789abcdef0123456789abcdef"

func encoded(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func prompt(kind string) Prompt {
	p := Prompt{ID: fixtureID, Kind: kind, Title: "Public fixture"}
	switch kind {
	case "input":
		p.Labels = []string{"Password"}
	case "confirm", "notice":
		p.Summary = ""
	case "receive":
		p.Address = "0xPublic"
		p.PNG = []byte("public fixture PNG")
	case "recovery":
		p.Phrase = fixturePhrase
	case "file":
		v := false
		p.Save = &v
	case "copy":
		p.Address = "0xPublic"
	}
	return p
}

func TestPromptSchemasAreExactAndBounded(t *testing.T) {
	for _, kind := range []string{"input", "confirm", "notice", "receive", "recovery", "file", "copy"} {
		p := prompt(kind)
		if _, e := DecodePrompt(encoded(t, p)); e != nil {
			t.Fatalf("valid %s denied", kind)
		}
		var f map[string]any
		if e := json.Unmarshal(encoded(t, p), &f); e != nil {
			t.Fatal(e)
		}
		for _, bad := range []string{`{"id":"x"}`, `null`, `[]`, `{"id":"x","id":"y"}`, `{"extra":true}`} {
			if _, e := DecodePrompt([]byte(bad)); e == nil {
				t.Fatal("malformed prompt accepted")
			}
		}
		for _, name := range []string{"id", "kind", "title"} {
			copy := map[string]any{}
			for k, v := range f {
				copy[k] = v
			}
			delete(copy, name)
			if _, e := DecodePrompt(encoded(t, copy)); e == nil {
				t.Fatal("missing common field accepted")
			}
		}
		f["unexpected"] = "CANARY-must-not-appear"
		if _, e := DecodePrompt(encoded(t, f)); e == nil {
			t.Fatal("unknown prompt field accepted")
		}
	}
	for _, mutate := range []func(*Prompt){
		func(p *Prompt) { p.ID = "ABCDEF0123456789abcdef0123456789ab" }, func(p *Prompt) { p.ID = "" }, func(p *Prompt) { p.Title = "" }, func(p *Prompt) { p.Title = strings.Repeat("x", 257) }, func(p *Prompt) { p.Title = "a\x00b" }, func(p *Prompt) { p.Kind = "eval" },
		func(p *Prompt) { p.Labels = nil }, func(p *Prompt) { p.Labels = make([]string, 9) }, func(p *Prompt) { p.Labels = []string{""} }, func(p *Prompt) { p.Labels = []string{strings.Repeat("x", 257)} },
	} {
		p := prompt("input")
		mutate(&p)
		if _, e := DecodePrompt(encoded(t, p)); e == nil {
			t.Fatal("invalid input metadata accepted")
		}
	}
	for _, tc := range []struct {
		kind   string
		change func(*Prompt)
	}{
		{"confirm", func(p *Prompt) { p.Summary = strings.Repeat("x", 65537) }},
		{"receive", func(p *Prompt) { p.Address = "" }}, {"receive", func(p *Prompt) { p.PNG = nil }}, {"receive", func(p *Prompt) { p.PNG = make([]byte, 262145) }},
		{"recovery", func(p *Prompt) { p.Phrase = "abandon about" }}, {"file", func(p *Prompt) { p.Save = nil }}, {"copy", func(p *Prompt) { p.Address = "" }},
	} {
		p := prompt(tc.kind)
		tc.change(&p)
		if _, e := DecodePrompt(encoded(t, p)); e == nil {
			t.Fatal("invalid kind-specific metadata accepted")
		}
	}
	for _, bad := range []string{`{"id":"` + fixtureID + `","kind":"confirm","title":"x"}`, `{"id":"` + fixtureID + `","kind":"confirm","title":"x","summary":null}`, `{"id":"` + fixtureID + `","kind":"input","title":"x","labels":["x"],"phrase":""}`} {
		if _, e := DecodePrompt([]byte(bad)); e == nil {
			t.Fatal("missing/null/cross-kind field accepted")
		}
	}
	if text(string([]byte{255}), 2, true) || ValidID(strings.Repeat("z", 32)) {
		t.Fatal("invalid UTF8 or ID accepted")
	}
	invalidUTF8 := []byte(`{"id":"` + fixtureID + `","kind":"confirm","title":"`)
	invalidUTF8 = append(invalidUTF8, 255)
	invalidUTF8 = append(invalidUTF8, []byte(`","summary":""}`)...)
	if _, e := DecodePrompt(invalidUTF8); e == nil {
		t.Fatal("malformed UTF8 prompt repaired silently")
	}
}

func TestRepliesAreSingleKindStrictAndLengthBounded(t *testing.T) {
	invalidUTF8 := []byte(`{"promptId":"` + fixtureID + `","accepted":true,"values":["`)
	invalidUTF8 = append(invalidUTF8, 255)
	invalidUTF8 = append(invalidUTF8, []byte(`"]}`)...)
	if _, e := DecodeReply(invalidUTF8, prompt("input")); e == nil {
		t.Fatal("malformed UTF8 secret input repaired silently")
	}
	for _, kind := range []string{"input", "confirm", "notice", "receive", "recovery", "file", "copy"} {
		p := prompt(kind)
		r := Reply{PromptID: p.ID, Accepted: true}
		if kind == "input" {
			r.Values = []string{"public synthetic password"}
		}
		if kind == "file" {
			r.Path = "/tmp/public-fixture"
		}
		if _, e := DecodeReply(encoded(t, r), p); e != nil {
			t.Fatalf("valid %s reply denied", kind)
		}
		if _, e := DecodeReply(encoded(t, Reply{PromptID: p.ID}), p); e != nil {
			t.Fatal("cancel denied")
		}
		for _, bad := range []string{`null`, `{"promptId":"` + fixtureID + `"}`, `{"promptId":"` + fixtureID + `","accepted":null}`, `{"promptId":"foreign","accepted":true}`, `{"promptId":"` + fixtureID + `","accepted":false,"values":[]}`, `{"promptId":"` + fixtureID + `","accepted":false,"path":""}`, `{"promptId":"` + fixtureID + `","accepted":false,"extra":true}`} {
			if _, e := DecodeReply([]byte(bad), p); e == nil {
				t.Fatal("ambiguous reply accepted")
			}
		}
	}
	p := prompt("input")
	for _, values := range [][]string{nil, {}, {"one", "two"}} {
		if _, e := DecodeReply(encoded(t, Reply{PromptID: p.ID, Accepted: true, Values: values}), p); e == nil {
			t.Fatal("incorrect input arity accepted")
		}
	}
	for _, value := range []string{strings.Repeat("x", 4097), "a\x00b"} {
		if _, e := DecodeReply(encoded(t, Reply{PromptID: p.ID, Accepted: true, Values: []string{value}}), p); !errors.Is(e, ErrInputTooLong) {
			t.Fatal("unbounded input accepted")
		}
	}
	for _, path := range []string{"", strings.Repeat("x", 4097), "a\x00b"} {
		if _, e := DecodeReply(encoded(t, Reply{PromptID: p.ID, Accepted: true, Path: path}), prompt("file")); e == nil {
			t.Fatal("invalid selected file accepted")
		}
	}
}

func TestUIRoutesOnlySignerOwnedInstructionsAndHandlesCancellation(t *testing.T) {
	seen := map[string]bool{}
	u := &UI{Exchange: func(p Prompt) (Reply, error) {
		if !ValidID(p.ID) || seen[p.ID] || strings.Contains(p.Title, "Go signer") {
			t.Fatal("prompt identity or title invalid")
		}
		seen[p.ID] = true
		r := Reply{PromptID: p.ID, Accepted: true}
		if p.Kind == "input" {
			r.Values = []string{"PUBLIC-input"}
		}
		if p.Kind == "file" {
			r.Path = "/tmp/PUBLIC-file"
		}
		return r, nil
	}}
	in, e := u.Input("Unlock — Go signer", "Password")
	if e != nil || string(in[0]) != "PUBLIC-input" {
		t.Fatal("input lost")
	}
	clear(in[0])
	if !u.Confirm("Approve", "Go validated summary") {
		t.Fatal("confirm lost")
	}
	u.Notice("Notice", "")
	if path, e := u.File("Select file", false); e != nil || path != "/tmp/PUBLIC-file" {
		t.Fatal("file selection lost")
	}
	if !u.ReceiveAddress("Receive", "Public summary", "0xPublic", []byte("png")) {
		t.Fatal("receive lost")
	}
	u.CopyPublic("0xPublic")
	if u.Receive("Receive", "Public summary", []byte("png")) {
		t.Fatal("receive fallback accepted missing exact address")
	}
	u.Exchange = func(p Prompt) (Reply, error) { return Reply{PromptID: p.ID}, nil }
	if _, e := u.Input("Cancel", "Password"); !errors.Is(e, ErrCancelled) {
		t.Fatal("cancel lost")
	}
	if u.Confirm("Cancel", "") {
		t.Fatal("cancel approved")
	}
	u.Exchange = func(Prompt) (Reply, error) { return Reply{}, ErrProtocol }
	if _, e := u.Input("Failed", "Password"); !errors.Is(e, ErrProtocol) {
		t.Fatal("transport failure lost")
	}
	u.Exchange = func(Prompt) (Reply, error) { return Reply{PromptID: "wrong", Accepted: true}, nil }
	if _, e := u.Input("Replay", "Password"); !errors.Is(e, ErrProtocol) {
		t.Fatal("foreign reply accepted")
	}
	u.Exchange = nil
	if _, e := u.Input("Unbound", "Password"); !errors.Is(e, ErrProtocol) {
		t.Fatal("unbound UI accepted")
	}
}

func TestRecoveryRequiresActualRecordedWordsInGo(t *testing.T) {
	for _, mode := range []string{"valid", "wrong", "cancel-record", "cancel-input", "transport"} {
		t.Run(mode, func(t *testing.T) {
			phrase := []byte(fixturePhrase)
			original := bytes.Clone(phrase)
			defer clear(phrase)
			defer clear(original)
			notices, stages := 0, 0
			u := &UI{Exchange: func(p Prompt) (Reply, error) {
				stages++
				r := Reply{PromptID: p.ID, Accepted: true}
				switch p.Kind {
				case "recovery":
					if p.Phrase != fixturePhrase {
						t.Fatal("backend-generated phrase changed")
					}
					if mode == "cancel-record" {
						r.Accepted = false
					}
					if mode == "transport" {
						return Reply{}, ErrProtocol
					}
				case "input":
					if strings.Join(p.Labels, ",") != "Recorded word 2,Recorded word 6,Recorded word 12" {
						t.Fatal("wrong challenge")
					}
					r.Values = []string{"abandon", "abandon", "about"}
					if mode == "wrong" {
						r.Values[1] = "incorrect"
					}
					if mode == "cancel-input" {
						r.Accepted = false
						r.Values = nil
					}
				case "notice":
					notices++
				default:
					t.Fatal("unexpected recovery prompt")
				}
				return r, nil
			}}
			if got := u.Recovery("Record recovery phrase — Go signer", phrase); got != (mode == "valid") {
				t.Fatal("recovery accepted unverified words")
			}
			if !bytes.Equal(phrase, original) {
				t.Fatal("provider altered caller phrase")
			}
			if (mode == "wrong") != (notices == 1) || stages == 0 {
				t.Fatal("failed-word notice missing")
			}
		})
	}
	for _, bad := range []string{"", strings.Repeat("abandon ", 11) + "ABOUT", strings.Repeat("abandon ", 11) + "about ", strings.Repeat("a", 513), "abandon  " + strings.Repeat("abandon ", 10) + "about"} {
		if (&UI{}).Recovery("Record", []byte(bad)) {
			t.Fatal("noncanonical phrase accepted")
		}
	}
	for _, n := range []int{15, 18, 21, 24} {
		phrase := []byte(strings.Repeat("abandon ", n-1) + "about")
		if !validPhrase(phrase) {
			t.Fatal("supported word length denied")
		}
	}
}

// Fuzz only public protocol fixtures. No UI is instantiated and no entropy,
// phrase generation, filesystem or network operation can occur in this target.
func FuzzPromptProtocol(f *testing.F) {
	f.Add([]byte(`{"id":"` + fixtureID + `","kind":"input","title":"Public fixture","labels":["Password"]}`))
	f.Add([]byte(`{"promptId":"` + fixtureID + `","accepted":false}`))
	f.Add([]byte(`{"promptId":"` + fixtureID + `","accepted":true,"values":["PUBLIC-fixture"]}`))
	f.Add([]byte(`{"id":"x","id":"y"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			return
		}
		if p, e := DecodePrompt(raw); e == nil {
			encoded, e := json.Marshal(p)
			if e != nil {
				t.Fatal("accepted prompt cannot serialize")
			}
			if _, e = DecodePrompt(encoded); e != nil {
				t.Fatal("accepted prompt does not roundtrip")
			}
			clear(encoded)
		}
		p := prompt("input")
		if r, e := DecodeReply(raw, p); e == nil {
			encoded, e := json.Marshal(r)
			if e != nil {
				t.Fatal("accepted reply cannot serialize")
			}
			if _, e = DecodeReply(encoded, p); e != nil {
				t.Fatal("accepted reply does not roundtrip")
			}
			clear(encoded)
		}
	})
}
