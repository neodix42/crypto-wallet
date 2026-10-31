package webui

import "testing"

func publicHistoryFixture() Node {
	return Node{Kind: "transactionTable", ID: "history", Text: "Transaction history", Children: []Node{{Kind: "row", ID: "walletHistory/owned/tx/effect", Children: []Node{
		{Kind: "label", Text: "2026-10-30 10:01:02 UTC"}, {Kind: "address", Value: "source"}, {Kind: "address", Value: "destination"}, {Kind: "label", Text: "1.000001 TOKEN"}, {Kind: "explorer", ID: "explorer/readonly", Text: "transaction", Value: "https://unreviewed.invalid/tx/ignored"}, {Kind: "label", Text: "<script>inert</script>"}, {Kind: "label", Text: "OBSERVED"},
	}}}}
}

func TestPublicHistoryTableIsBoundedReadonlyData(t *testing.T) {
	v := publicFixture()
	v.Active = v.Pages[0].ID
	v.Pages[0].Nodes = []Node{publicHistoryFixture()}
	if !validateView(v) {
		t.Fatal("complete seven-cell readonly transaction table rejected")
	}
	for _, id := range []string{"history", "walletHistory/owned/tx/effect", "explorer/readonly"} {
		if validEvent(v, Event{ID: id}, false) {
			t.Fatal("readonly history identity became a controller action", id)
		}
	}
	for _, empty := range [][]Node{nil, {}} {
		v.Pages[0].Nodes[0].Children = empty
		if !validateView(v) {
			t.Fatal("empty history representation rejected")
		}
	}
	for _, mutate := range []func(*Node){
		func(n *Node) { n.Children[0].Kind = "button" },
		func(n *Node) { n.Children[0].Children = nil },
		func(n *Node) { n.Children[0].Children = n.Children[0].Children[1:] },
		func(n *Node) { n.Children[0].Children[4].Kind = "button" },
		func(n *Node) { n.Children[0].Children[5].Children = []Node{{Kind: "button", ID: "send/forbidden"}} },
	} {
		n := publicHistoryFixture()
		mutate(&n)
		v.Pages[0].Nodes = []Node{n}
		if validateView(v) {
			t.Fatal("malformed history row or nested action accepted")
		}
	}
}

func actionableHistoryFixture(state string, kinds ...string) Node {
	table := publicHistoryFixture()
	status := Node{Kind: "column", ID: "history-status", Text: state, Value: "original-plan", Children: []Node{{Kind: "label", ID: "status-label", Text: state + " · provider: OBSERVED"}}}
	for _, kind := range kinds {
		status.Children = append(status.Children, Node{Kind: "button", ID: kind + "/original-plan", Text: "Exact original operation"})
	}
	table.Children[0].Children[6] = status
	return table
}

func TestHistoryStatusAcceptsOnlyTypedOriginalActionSetAndExplicitActiveClick(t *testing.T) {
	for _, test := range []struct {
		state string
		kinds []string
	}{
		{"PREPARED", []string{"sendOperation", "cancelPrepared"}},
		{"SIGNED_NOT_SUBMITTED", []string{"retry", "reconcile"}},
		{"UNKNOWN", []string{"retry", "reconcile"}},
		{"SUBMITTED", []string{"reconcile"}}, {"SUBMITTING", []string{"reconcile"}}, {"REORGED", []string{"reconcile"}},
	} {
		t.Run(test.state, func(t *testing.T) {
			v := publicFixture()
			v.Active = v.Pages[0].ID
			v.Pages[0].Nodes = []Node{actionableHistoryFixture(test.state, test.kinds...)}
			if !validateView(v) {
				t.Fatal("typed original operation controls rejected")
			}
			for _, kind := range test.kinds {
				event := Event{ID: kind + "/original-plan"}
				if !validEvent(v, event, false) || validEvent(v, event, true) || validEvent(v, Event{ID: event.ID, Value: "replacement"}, false) {
					t.Fatal("explicit original action was lost or acquired extra input authority", kind)
				}
				for _, disable := range []string{"table", "row", "status", "button"} {
					blocked := cloneView(v)
					table := &blocked.Pages[0].Nodes[0]
					switch disable {
					case "table":
						table.Disabled = true
					case "row":
						table.Children[0].Disabled = true
					case "status":
						table.Children[0].Children[6].Disabled = true
					case "button":
						for i := range table.Children[0].Children[6].Children {
							if table.Children[0].Children[6].Children[i].ID == event.ID {
								table.Children[0].Children[6].Children[i].Disabled = true
							}
						}
					}
					if validEvent(blocked, event, false) {
						t.Fatal("disabled history action accepted", disable)
					}
				}
				v.Active = "other-wallet"
				if validEvent(v, event, false) {
					t.Fatal("inactive wallet action accepted")
				}
				v.Active = v.Pages[0].ID
			}
			for _, id := range []string{"history", "walletHistory/owned/tx/effect", "explorer/readonly", "history-status", "status-label", "retry/arbitrary-observed-tx"} {
				if validEvent(v, Event{ID: id}, false) {
					t.Fatal("data identity became a controller action", id)
				}
			}
		})
	}
}

func TestHistoryStatusRejectsWrongStateIdentityExtraControlsAndProviderActions(t *testing.T) {
	for _, mutate := range []func(*Node){
		func(n *Node) { n.Children[0].Children[6].Text = "CONFIRMED" },
		func(n *Node) { n.Children[0].Children[6].Value = "different-plan" },
		func(n *Node) { n.Children[0].Children[6].Value = "" },
		func(n *Node) { n.Children[0].Children[6].Value = string(make([]byte, 129)) },
		func(n *Node) { n.Children[0].Children[6].Children[1].ID = "sendOperation/replacement" },
		func(n *Node) { n.Children[0].Children[6].Children[1].Value = "replacement" },
		func(n *Node) { n.Children[0].Children[6].Children[1].Kind = "entry" },
		func(n *Node) {
			n.Children[0].Children[6].Children[1].Children = []Node{{Kind: "button", ID: "send/forbidden"}}
		},
		func(n *Node) { n.Children[0].Children[6].Children[1].Options = []Option{{ID: "replacement"}} },
		func(n *Node) {
			n.Children[0].Children[6].Children = append(n.Children[0].Children[6].Children, Node{Kind: "button", ID: "send/replacement"})
		},
		func(n *Node) { n.Children[0].Children[4] = n.Children[0].Children[6] },
		func(n *Node) {
			n.Children[0].Children[5].Children = []Node{{Kind: "button", ID: "sendOperation/original-plan"}}
		},
		func(n *Node) { n.Children[0].Children[6].Children = n.Children[0].Children[6].Children[1:] },
	} {
		v := publicFixture()
		v.Active = v.Pages[0].ID
		table := actionableHistoryFixture("PREPARED", "sendOperation", "cancelPrepared")
		mutate(&table)
		v.Pages[0].Nodes = []Node{table}
		if validateView(v) || validEvent(v, Event{ID: "sendOperation/original-plan"}, false) {
			t.Fatal("malformed history status granted authority", table)
		}
	}
}
