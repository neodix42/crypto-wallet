package nativewallet

import (
	"localwallet/internal/webui"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// This is the same public configuration/profile convention used by
// walletcore.signerChild. Looking up its location never opens a vault or
// creates profile directories, and remains available while locked.
func (a *App) storageLocation() webui.Node {
	text := "Encrypted storage directory is unavailable. Check your HOME and XDG_CONFIG_HOME settings."
	configuration, err := os.UserConfigDir()
	if err == nil && filepath.IsAbs(configuration) && (a.environment == "mainnet" || a.environment == "testnet") {
		directory := filepath.Join(configuration, "localwallet", a.environment)
		if len(directory) <= 4096 && utf8.ValidString(directory) && !strings.ContainsRune(directory, 0) {
			text = "Encrypted storage directory\n" + directory + "\nVault file: vault.lwv"
		}
	}
	n := label(text)
	n.ID = "storageLocation"
	return n
}
