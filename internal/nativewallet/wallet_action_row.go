package nativewallet

import (
	"localwallet/internal/webui"
)

// Keep management actions next to Send/Receive without changing their guards or
// the asset/status/discovery helpers used elsewhere.
func collectWalletActions(nodes []webui.Node, id string) []webui.Node {
	actionID := "walletActions/" + id
	var actions []webui.Node
	var gather func([]webui.Node) []webui.Node
	gather = func(list []webui.Node) []webui.Node {
		out := make([]webui.Node, 0, len(list))
		for _, n := range list {
			if n.ID == actionID {
				out = append(out, n)
				continue
			}
			management := false
			for _, prefix := range []string{"trackToken/", "discoverTokens/", "tonState/", "hideAccount/"} {
				if n.Kind == "button" && n.ID == prefix+id {
					management = true
					break
				}
			}
			if management {
				actions = append(actions, n)
				continue
			}
			if len(n.Children) > 0 {
				n.Children = gather(n.Children)
			}
			out = append(out, n)
		}
		return out
	}
	nodes = gather(nodes)
	for i := range nodes {
		if nodes[i].ID == actionID {
			nodes[i].Children = append(nodes[i].Children, actions...)
			break
		}
	}
	return nodes
}
