package runtime

import (
	"context"
	"github.com/wailsapp/wails/v2/internal/frontend/walletclipboard"
)

func ClipboardGetText(ctx context.Context) (string, error) {
	appFrontend := getFrontend(ctx)
	return appFrontend.ClipboardGetText()
}

func ClipboardSetText(ctx context.Context, text string) error {
	appFrontend := getFrontend(ctx)
	return appFrontend.ClipboardSetText(text)
}

// ClipboardSetTextAuthorized is a wallet-specific Go capability. authorize must
// validate lifecycle state and invoke commit synchronously under its state lock.
// No JavaScript runtime call can invoke it.
func ClipboardSetTextAuthorized(ctx context.Context, text string, authorize func(func()) bool) error {
	if ctx == nil || authorize == nil || ctx.Err() != nil {
		return walletclipboard.ErrUnavailable
	}
	capable, ok := ctx.Value("frontend").(interface {
		ClipboardSetTextAuthorized(context.Context, string, func(func()) bool) error
	})
	if !ok {
		return walletclipboard.ErrUnavailable
	}
	return capable.ClipboardSetTextAuthorized(ctx, text, authorize)
}
