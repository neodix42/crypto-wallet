// Package uiprompt implements the bounded, signer-owned Electron prompt protocol.
// Only transient input/phrase prompts may contain secrets; never log a prompt or
// reply, include it in an error, or persist it outside the encrypted vault.
package uiprompt

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"localwallet/internal/strictjson"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrProtocol     = errors.New("invalid wallet prompt protocol")
	ErrCancelled    = errors.New("wallet prompt cancelled")
	ErrInputTooLong = errors.New("wallet input exceeds limit")
)

type Prompt struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Labels  []string `json:"labels,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Phrase  string   `json:"phrase,omitempty"`
	PNG     []byte   `json:"png,omitempty"`
	Address string   `json:"address,omitempty"`
	Save    *bool    `json:"save,omitempty"`
}

type Reply struct {
	PromptID string   `json:"promptId"`
	Accepted bool     `json:"accepted"`
	Values   []string `json:"values,omitempty"`
	Path     string   `json:"path,omitempty"`
}

func (p Prompt) MarshalJSON() ([]byte, error) {
	type plain Prompt
	raw, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	if p.Kind != "confirm" && p.Kind != "notice" && p.Kind != "receive" {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["summary"], err = json.Marshal(p.Summary)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func text(s string, limit int, empty bool) bool {
	return (empty || len(s) > 0) && len(s) <= limit && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func ValidID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// DecodePrompt also rejects fields belonging to a different prompt kind, even
// when they contain null/empty values. This prevents ambiguous UI instructions.
func DecodePrompt(raw []byte) (Prompt, error) {
	var p Prompt
	if !utf8.Valid(raw) || strictjson.Decode(raw, &p) != nil || !ValidID(p.ID) || !text(p.Title, 256, false) {
		return Prompt{}, ErrProtocol
	}
	fields := map[string]json.RawMessage{}
	if strictjson.Decode(raw, &fields) != nil {
		return Prompt{}, ErrProtocol
	}
	allowed := "id kind title"
	required := "id kind title"
	switch p.Kind {
	case "input":
		allowed += " labels"
		required += " labels"
		if len(p.Labels) < 1 || len(p.Labels) > 8 {
			return Prompt{}, ErrProtocol
		}
		for _, label := range p.Labels {
			if !text(label, 256, false) {
				return Prompt{}, ErrProtocol
			}
		}
	case "confirm", "notice":
		allowed += " summary"
		required += " summary"
		if !text(p.Summary, 65536, true) {
			return Prompt{}, ErrProtocol
		}
	case "receive":
		allowed += " summary address png"
		required += " summary address png"
		if !text(p.Summary, 65536, true) || !text(p.Address, 512, false) || len(p.PNG) == 0 || len(p.PNG) > 262144 {
			return Prompt{}, ErrProtocol
		}
	case "recovery", "recoveryBackup":
		allowed += " phrase"
		required += " phrase"
		if !validPhrase([]byte(p.Phrase)) {
			return Prompt{}, ErrProtocol
		}
	case "file":
		allowed += " save"
		required += " save"
		if p.Save == nil {
			return Prompt{}, ErrProtocol
		}
	case "copy":
		allowed += " address"
		required += " address"
		if !text(p.Address, 512, false) {
			return Prompt{}, ErrProtocol
		}
	default:
		return Prompt{}, ErrProtocol
	}
	if !fieldsMatch(fields, allowed, required) {
		return Prompt{}, ErrProtocol
	}
	return p, nil
}
func fieldsMatch(fields map[string]json.RawMessage, allowed, required string) bool {
	for field, value := range fields {
		if !strings.Contains(" "+allowed+" ", " "+field+" ") || bytes.Equal(value, []byte("null")) {
			return false
		}
	}
	for _, field := range strings.Fields(required) {
		if _, ok := fields[field]; !ok {
			return false
		}
	}
	return true
}

func DecodeReply(raw []byte, p Prompt) (Reply, error) {
	var r Reply
	if !utf8.Valid(raw) || strictjson.Decode(raw, &r) != nil || r.PromptID != p.ID {
		return Reply{}, ErrProtocol
	}
	fields := map[string]json.RawMessage{}
	if strictjson.Decode(raw, &fields) != nil {
		return Reply{}, ErrProtocol
	}
	allowed := "promptId accepted"
	if r.Accepted {
		switch p.Kind {
		case "input":
			allowed += " values"
			if len(r.Values) != len(p.Labels) {
				return Reply{}, ErrProtocol
			}
			for _, v := range r.Values {
				if !text(v, 4096, true) {
					return Reply{}, ErrInputTooLong
				}
			}
		case "file":
			allowed += " path"
			if !text(r.Path, 4096, false) {
				return Reply{}, ErrProtocol
			}
		}
	}
	if !fieldsMatch(fields, allowed, "promptId accepted") {
		return Reply{}, ErrProtocol
	}
	return r, nil
}

type UI struct{ Exchange func(Prompt) (Reply, error) }

func (u *UI) ask(p Prompt) (Reply, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Reply{}, ErrProtocol
	}
	p.ID = hex.EncodeToString(id[:])
	p.Title = strings.TrimSuffix(p.Title, " — Go signer")
	raw, err := json.Marshal(p)
	if err != nil {
		return Reply{}, ErrProtocol
	}
	defer clear(raw)
	if _, err = DecodePrompt(raw); err != nil || u.Exchange == nil {
		return Reply{}, ErrProtocol
	}
	r, err := u.Exchange(p)
	if err != nil {
		return Reply{}, err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return Reply{}, ErrProtocol
	}
	defer clear(encoded)
	r, err = DecodeReply(encoded, p)
	if err != nil {
		return Reply{}, err
	}
	if !r.Accepted {
		return Reply{}, ErrCancelled
	}
	return r, nil
}

func (u *UI) Input(title string, labels ...string) ([][]byte, error) {
	r, err := u.ask(Prompt{Kind: "input", Title: title, Labels: labels})
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(r.Values))
	for i, v := range r.Values {
		out[i] = []byte(v)
		r.Values[i] = ""
	}
	return out, nil
}
func (u *UI) Confirm(title, summary string) bool {
	_, err := u.ask(Prompt{Kind: "confirm", Title: title, Summary: summary})
	return err == nil
}
func (u *UI) Notice(title, summary string) {
	_, _ = u.ask(Prompt{Kind: "notice", Title: title, Summary: summary})
}
func (u *UI) File(title string, save bool) (string, error) {
	r, err := u.ask(Prompt{Kind: "file", Title: title, Save: &save})
	return r.Path, err
}
func (u *UI) Receive(title, summary string, png []byte) bool {
	return u.ReceiveAddress(title, summary, "", png)
}
func (u *UI) ReceiveAddress(title, summary, address string, png []byte) bool {
	_, err := u.ask(Prompt{Kind: "receive", Title: title, Summary: summary, Address: address, PNG: png})
	return err == nil
}
func (u *UI) CopyPublic(address string) error {
	_, err := u.ask(Prompt{Kind: "copy", Title: "Copy wallet address", Address: address})
	return err
}

func validPhrase(phrase []byte) bool {
	words := bytes.Fields(phrase)
	n := len(words)
	if n != 12 && n != 15 && n != 18 && n != 21 && n != 24 || len(phrase) > 512 {
		return false
	}
	size := n - 1
	for _, word := range words {
		size += len(word)
		for _, c := range word {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	return size == len(phrase)
}

// Recovery keeps authorization in Go. A recording click alone cannot enable
// the wallet: three separately entered words must match the generated phrase.
func (u *UI) Recovery(title string, phrase []byte) bool {
	if !validPhrase(phrase) {
		return false
	}
	if _, err := u.ask(Prompt{Kind: "recovery", Title: title, Phrase: string(phrase)}); err != nil {
		return false
	}
	words := bytes.Fields(phrase)
	positions := []int{1, 5, len(words) - 1}
	labels := make([]string, 3)
	for i, p := range positions {
		labels[i] = "Recorded word " + strconv.Itoa(p+1)
	}
	values, err := u.Input("Confirm recovery phrase", labels...)
	if err != nil {
		return false
	}
	matched := len(values) == 3
	for i, value := range values {
		if i >= 3 || subtle.ConstantTimeCompare(value, words[positions[i]]) != 1 {
			matched = false
		}
		clear(value)
	}
	if !matched {
		u.Notice("Recovery phrase not confirmed", "The recorded words did not match. Record the complete phrase in order, then try again. Your wallet remains unavailable for payments until this is complete.")
	}
	return matched
}

// BackupRecovery displays an already-confirmed root after Engine authorizes
// this particular view. Its acknowledgment grants no authority and does not
// repeat or alter the initial recorded-word challenge.
func (u *UI) BackupRecovery(title string, phrase []byte) bool {
	if !validPhrase(phrase) {
		return false
	}
	_, err := u.ask(Prompt{Kind: "recoveryBackup", Title: title, Phrase: string(phrase)})
	return err == nil
}
