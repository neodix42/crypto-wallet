package runtime

import (
	"context"
	"testing"
)

type authorizedClipboard struct{ called bool }

func (f *authorizedClipboard) ClipboardSetTextAuthorized(ctx context.Context, text string, authorize func(func()) bool) error {
	f.called = true
	authorize(func() {})
	return nil
}
func TestAuthorizedClipboardIsGoCapabilityOnly(t *testing.T) {
	authorize := func(commit func()) bool { commit(); return true }
	for _, ctx := range []context.Context{nil, context.Background(), context.WithValue(context.Background(), "frontend", struct{}{})} {
		if ClipboardSetTextAuthorized(ctx, "public fixture", authorize) == nil {
			t.Fatal("missing native capability accepted")
		}
	}
	fake := &authorizedClipboard{}
	ctx := context.WithValue(context.Background(), "frontend", fake)
	if ClipboardSetTextAuthorized(ctx, "public fixture", authorize) != nil || !fake.called {
		t.Fatal("trusted capability not called")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	fake.called = false
	if ClipboardSetTextAuthorized(canceled, "public fixture", authorize) == nil || fake.called {
		t.Fatal("canceled capability called")
	}
	if ClipboardSetTextAuthorized(ctx, "public fixture", nil) == nil {
		t.Fatal("missing authorizer accepted")
	}
}
