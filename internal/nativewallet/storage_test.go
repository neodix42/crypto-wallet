package nativewallet

import (
	"errors"
	"localwallet/internal/profilepath"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageConflictStopsStartupBeforePreferencesOrBackend(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("DISPLAY", ":invalid-owned-test-display")
	for _, name := range []string{"localwallet", "cryptowallet"} {
		if err := os.Mkdir(filepath.Join(config, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := Run("mainnet"); !errors.Is(err, profilepath.ErrConflict) {
		t.Fatal("startup selected a root or hid its storage conflict", err)
	}
	for _, name := range []string{"localwallet", "cryptowallet"} {
		entries, err := os.ReadDir(filepath.Join(config, name))
		if err != nil || len(entries) != 0 {
			t.Fatal("failed startup created preferences or a replacement wallet")
		}
	}
}

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
			expected := filepath.Join(configuration, "cryptowallet", environment)
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
	if !strings.Contains(a.storageLocation().Text, filepath.Join(home, ".config", "cryptowallet", "mainnet")) {
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
