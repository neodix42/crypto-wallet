// Package branding supplies the original CryptoWallet window icon without
// requiring an installed resource directory beside the portable executable.
package branding

import _ "embed"

// GTK's X11 backend may reject a large sole icon property. The 128-pixel
// rendition is sufficient for window/task-switcher use; desktop registration
// installs the separate 512-pixel PNG and scalable SVG.
//
//go:embed window-icon.png
var windowIcon []byte

// WindowIcon returns independent PNG bytes for a Wails window's Linux options.
func WindowIcon() []byte {
	return append([]byte(nil), windowIcon...)
}
