//go:build linux
// +build linux

package linux

/*
#cgo linux pkg-config: gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0
#cgo webkit2_41 pkg-config: webkit2gtk-4.1

#include "gtk/gtk.h"
#include "webkit2/webkit2.h"

static gchar* GetClipboardText() {
	GtkClipboard *clip = gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
	return gtk_clipboard_wait_for_text(clip);
}

#include "wallet_clipboard.h"

*/
import "C"
import (
	"context"
	"github.com/wailsapp/wails/v2/internal/frontend/walletclipboard"
	"sync"
)

func (f *Frontend) ClipboardGetText() (string, error) {
	var text string
	var wg sync.WaitGroup
	wg.Add(1)
	invokeOnMainThread(func() {
		ctxt := C.GetClipboardText()
		defer C.g_free(C.gpointer(ctxt))
		text = C.GoString(ctxt)
		wg.Done()
	})
	wg.Wait()
	return text, nil
}

func (f *Frontend) ClipboardSetText(text string) error {
	return walletclipboard.Execute(context.Background(), func(commit func()) bool { commit(); return true }, invokeOnMainThread, func() bool {
		ctxt := (*C.gchar)(C.CString(text))
		defer C.WalletClearClipboardText(ctxt, C.gsize(len(text)))
		return C.WalletSetClipboardText(ctxt) != 0
	})
}
