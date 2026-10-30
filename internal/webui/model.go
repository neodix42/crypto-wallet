// Package webui renders wallet pages and dialogs with Wails and system WebKitGTK.
// View contains page data; DialogView represents the current in-window dialog.
package webui

import (
	"strings"
	"unicode/utf8"
)

type Option struct{ ID, Text string }
type Node struct {
	Kind, ID, Text, Value, Tooltip string
	Disabled                       bool
	Options                        []Option
	Children                       []Node
}
type Page struct {
	ID, Title string
	Nodes     []Node
}
type View struct {
	Title, Status, Active string
	StatusRevision        uint64
	Pages                 []Page
	Appearance            Appearance
}
type Event struct{ ID, Value string }

func cloneView(v View) View {
	pages := make([]Page, len(v.Pages))
	copy(pages, v.Pages)
	for i := range pages {
		pages[i].Nodes = cloneNodes(pages[i].Nodes)
	}
	v.Pages = pages
	return v
}
func cloneNodes(nodes []Node) []Node {
	out := make([]Node, len(nodes))
	copy(out, nodes)
	for i := range out {
		out[i].Options = append([]Option(nil), out[i].Options...)
		out[i].Children = cloneNodes(out[i].Children)
	}
	return out
}

func plain(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// validateView bounds GTK allocation and prevents accidentally introducing a
// password/mnemonic entry into the public event channel. Secrets belong only
// to the separately typed Prompt API, never a public declarative form.
func validateView(v View) bool {
	if v.Appearance != (Appearance{}) && !v.Appearance.Valid() {
		return false
	}
	if !plain(v.Title, 256) || !plain(v.Status, 65536) || !plain(v.Active, 256) || len(v.Pages) > 1000 {
		return false
	}
	remaining := 20000
	var nodes func([]Node, int) bool
	nodes = func(list []Node, depth int) bool {
		if depth > 32 {
			return false
		}
		for _, n := range list {
			remaining--
			if remaining < 0 || !plain(n.ID, 256) || !plain(n.Text, 65536) || !plain(n.Tooltip, 65536) || !plain(n.Value, 4096) || len(n.Options) > 10000 {
				return false
			}
			switch n.Kind {
			case "label", "heading", "separator", "address", "button", "combo", "check", "row", "column":
			case "entry":
				id := strings.ToLower(n.ID)
				for _, secret := range []string{"password", "phrase", "mnemonic", "secret", "apikey", "api_key", "credential", "passphrase"} {
					if strings.Contains(id, secret) {
						return false
					}
				}
			default:
				return false
			}
			for _, option := range n.Options {
				if !plain(option.ID, 4096) || !plain(option.Text, 4096) {
					return false
				}
			}
			if !nodes(n.Children, depth+1) {
				return false
			}
		}
		return true
	}
	ids := map[string]bool{}
	for _, page := range v.Pages {
		if page.ID == "" || ids[page.ID] || !plain(page.ID, 256) || !plain(page.Title, 256) || !nodes(page.Nodes, 0) {
			return false
		}
		ids[page.ID] = true
	}
	return true
}
