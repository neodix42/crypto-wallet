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
