package webui

import "testing"

func TestSingleWindowSupportsOrdinaryUserClipboardInteraction(t *testing.T) {
	options := appOptions()
	if !options.EnableDefaultContextMenu {
		t.Fatal("trusted wallet UI must offer the native copy/paste context menu")
	}
	if options.Linux == nil {
		t.Fatal("Linux desktop options unavailable")
	}
}
