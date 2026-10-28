package nativewallet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalletSettingsShowsExactProfileStorageWithoutOpeningIt(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "configuration with spaces")
	t.Setenv("XDG_CONFIG_HOME", configuration)
	a, _, core := fixture(t)
	core.handler = func(_ uint64, method string, _, _ any) error {
		t.Fatalf("showing storage location reached wallet backend: %s", method)
		return nil
	}
	for _, environment := range []string{"mainnet", "testnet"} {
		for _, locked := range []bool{false, true} {
			a.environment, a.state.Locked = environment, locked
			n, ok := findNode(a.settingsPage(), "storageLocation")
			expected := filepath.Join(configuration, "localwallet", environment)
			if !ok || n.Kind != "label" || !strings.Contains(n.Text, "\n"+expected+"\nVault file: vault.lwv") {
				t.Fatal("settings did not show its exact selected storage profile", environment, locked)
			}
		}
	}
	if _, err := os.Stat(configuration); !os.IsNotExist(err) {
		t.Fatal("read-only storage display created or opened a profile", err)
	}
}

func TestStorageLocationUsesHomeFallbackAndRejectsUnavailableConfiguration(t *testing.T) {
	a, _, _ := fixture(t)
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if !strings.Contains(a.storageLocation().Text, filepath.Join(home, ".config", "localwallet", "mainnet")) {
		t.Fatal("default HOME configuration path not displayed")
	}
	for _, configuration := range []string{"relative/configuration", string([]byte{'/', 0xff})} {
		t.Setenv("XDG_CONFIG_HOME", configuration)
		if !strings.Contains(a.storageLocation().Text, "unavailable") {
			t.Fatal("unusable configuration produced an apparent storage location")
		}
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if !strings.Contains(a.storageLocation().Text, "unavailable") {
		t.Fatal("missing configuration produced a guessed location")
	}
	t.Setenv("XDG_CONFIG_HOME", home)
	a.environment = "../other"
	if !strings.Contains(a.storageLocation().Text, "unavailable") {
		t.Fatal("invalid profile produced a storage location")
	}
}

func TestStorageLocationAppearsOnlyInWalletSettings(t *testing.T) {
	a, _, _ := fixture(t)
	for _, selected := range []string{"Appearance", "Exchange", "Journal"} {
		a.fields["settings"] = selected
		if _, ok := findNode(a.settingsPage(), "storageLocation"); ok {
			t.Fatal("wallet storage location appeared in another settings section", selected)
		}
	}
}
