package webui

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPublicAccountParentsAndValidationMetadataAreBounded(t *testing.T) {
	v := publicFixture()
	v.AccountGroupID = "group/0"
	v.Pages[1].ParentID = "group/0"
	v.Pages[0].Nodes = append(v.Pages[0].Nodes, Node{Kind: "entry", ID: "field/amount/a", InputMode: "decimal", Error: "Enter a positive amount."})
	if !validateView(v) {
		t.Fatal("new valid presentation rejected")
	}
	for _, id := range []string{"group/-1", "group/01", "group/4294967296", "group/", "group/0\x00", "other/0"} {
		copy := cloneView(v)
		copy.AccountGroupID = id
		if validateView(copy) {
			t.Fatal("malformed parent accepted", id)
		}
	}
	for _, change := range []func(*View){
		func(v *View) { v.AccountGroupID = "group/99" },
		func(v *View) { v.Pages[0].ParentID = "group/0" },
		func(v *View) { v.Pages[1].ParentID = "group/01" },
		func(v *View) { v.Pages[0].Nodes[0].Error = "error on non-entry" },
		func(v *View) { v.Pages[0].Nodes[len(v.Pages[0].Nodes)-1].InputMode = "url" },
		func(v *View) { v.Pages[0].Nodes[len(v.Pages[0].Nodes)-1].Error = strings.Repeat("e", 1025) },
	} {
		copy := cloneView(v)
		change(&copy)
		if validateView(copy) {
			t.Fatal("invalid metadata accepted")
		}
	}
	// Retained tabs outside the current eight-group page remain bounded identities.
	v.Pages[1].ParentID = "group/4294967295"
	if !validateView(v) {
		t.Fatal("inactive wallet parent must not require materializing all account pages")
	}
}

// Bind the frontend regression input to Go's actual wire encoding, including
// serialized empty optional fields. This prevents drift across the bridge.
func TestAccountLayoutFrontendFixtureMatchesGoEncoding(t *testing.T) {
	v := View{Title: "CryptoWallet", Active: "group/0", Appearance: DefaultAppearance(), Pages: []Page{
		{ID: "group/0", Title: "Main account", Nodes: []Node{{Kind: "label", ID: "networkCount", Text: "8 networks"}, {Kind: "button", ID: "lock", Text: "Lock wallet"}}},
		{ID: "wallet/ethereum", Title: "Ethereum", Nodes: []Node{{Kind: "entry", ID: "field/amount/ethereum", Text: "Amount", Value: "1e3"}}},
	}}
	out := []View{v}
	v.Pages = append([]Page(nil), v.Pages...)
	v.Pages[1].Nodes = append([]Node(nil), v.Pages[1].Nodes...)
	v.AccountGroupID = "group/0"
	v.Pages[1].ParentID = "group/0"
	v.Pages[1].Nodes[0].InputMode = "decimal"
	v.Pages[1].Nodes[0].Error = "Enter a positive decimal amount."
	out = append(out, v)
	for _, view := range out {
		if !validateView(view) {
			t.Fatal("wire fixture violates Go view model")
		}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	fixture, err := os.ReadFile("frontend/src/testdata/go-account-layout.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, fixture) {
		t.Fatal("frontend account-layout fixture differs from actual Go JSON encoding")
	}
}

func TestContextActionsBindToExactEnabledInactiveTargetButtons(t *testing.T) {
	v := publicFixture()
	v.AccountGroupID = "group/0"
	v.Active = "group/0"
	v.Pages[0].Nodes = append(v.Pages[0].Nodes, Node{Kind: "button", ID: "addNextAccount"}, Node{Kind: "button", ID: "renameGroup/0"})
	wallet := v.Pages[1].ID
	id := "renameWallet/" + strings.TrimPrefix(wallet, "wallet/")
	v.Pages[1].Nodes = append(v.Pages[1].Nodes, Node{Kind: "button", ID: id})
	for _, e := range []Event{{ID: id}, {ID: "addNextAccount"}, {ID: "renameGroup/0"}} {
		if !validEvent(v, e, false) || validEvent(v, e, true) {
			t.Fatal("exact inactive context binding lost", e)
		}
	}
	for _, e := range []Event{{ID: id, Value: "forged"}, {ID: "renameWallet/foreign"}, {ID: "deleteGroup/0"}, {ID: "renameGroup/1"}} {
		if validEvent(v, e, false) {
			t.Fatal("foreign or missing context action accepted", e)
		}
	}
	for _, kind := range []string{"entry", "row", "button"} {
		copy := cloneView(v)
		n := &copy.Pages[1].Nodes[len(copy.Pages[1].Nodes)-1]
		n.Kind = kind
		n.Disabled = kind == "button"
		if validEvent(copy, Event{ID: id}, false) {
			t.Fatal("malformed/disabled target accepted", kind)
		}
	}
}
