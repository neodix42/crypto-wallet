// Package webui renders wallet pages and dialogs with Wails and system WebKitGTK.
// View contains page data; DialogView represents the current in-window dialog.
package webui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type Option struct{ ID, Text string }
type Node struct {
	Kind, ID, Text, Value, Tooltip string
	Error, InputMode               string
	Disabled                       bool
	Options                        []Option
	Children                       []Node
}
type Page struct {
	ID, Title, ParentID string
	Nodes               []Node
}
type ErrorNotice struct {
	ID, Title, Operation, Code, Message string
}

type View struct {
	Title, Status, Active, AccountGroupID string
	StatusRevision                        uint64
	Pages                                 []Page
	Appearance                            Appearance
	ErrorNotice                           *ErrorNotice `json:"ErrorNotice,omitempty"`
}
type Event struct{ ID, Value string }

func cloneView(v View) View {
	if v.ErrorNotice != nil {
		copy := *v.ErrorNotice
		v.ErrorNotice = &copy
	}
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
	if n := v.ErrorNotice; n != nil && (n.ID == "" || !plain(n.ID, 64) || n.Title == "" || !plain(n.Title, 256) || !plain(n.Operation, 256) || n.Code == "" || !plain(n.Code, 80) || n.Message == "" || !plain(n.Message, 8192)) {
		return false
	}
	if v.Appearance != (Appearance{}) && !v.Appearance.Valid() {
		return false
	}
	if !plain(v.Title, 256) || !plain(v.Status, 65536) || !plain(v.Active, 256) || v.AccountGroupID != "" && !validGroupID(v.AccountGroupID) || len(v.Pages) > 1000 {
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
			if remaining < 0 || !plain(n.ID, 256) || !plain(n.Text, 65536) || !plain(n.Tooltip, 65536) || !plain(n.Value, 4096) || !plain(n.Error, 1024) || n.InputMode != "" && (n.Kind != "entry" || n.InputMode != "decimal") || n.Error != "" && n.Kind != "entry" || len(n.Options) > 10000 {
				return false
			}
			switch n.Kind {
			case "label", "heading", "separator", "address", "button", "combo", "check", "row", "column", "explorer":
			case "transactionTable":
				if !validTransactionRows(n.Children) {
					return false
				}
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
		if page.ID == "" || ids[page.ID] || !plain(page.ID, 256) || !plain(page.Title, 256) || page.ParentID != "" && (!strings.HasPrefix(page.ID, "wallet/") || !validGroupID(page.ParentID)) || !nodes(page.Nodes, 0) {
			return false
		}
		ids[page.ID] = true
	}
	return v.AccountGroupID == "" || ids[v.AccountGroupID]
}

func validGroupID(id string) bool {
	if len(id) < 7 || len(id) > 16 || !strings.HasPrefix(id, "group/") {
		return false
	}
	raw := strings.TrimPrefix(id, "group/")
	n, err := strconv.ParseUint(raw, 10, 32)
	return err == nil && strconv.FormatUint(n, 10) == raw
}

func validTransactionRows(rows []Node) bool {
	kinds := [...]string{"label", "address", "address", "label", "explorer", "label", "label"}
	for _, row := range rows {
		if row.Kind != "row" || len(row.Children) != len(kinds) {
			return false
		}
		for i, cell := range row.Children {
			if cell.Kind != kinds[i] || len(cell.Children) != 0 {
				return false
			}
		}
	}
	return true
}
