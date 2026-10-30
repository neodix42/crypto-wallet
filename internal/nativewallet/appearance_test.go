package nativewallet

import (
	"localwallet/internal/webui"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppearanceIsAvailableLockedAndPersistsOnlyPublicValidatedPreferences(t *testing.T) {
	a, _, core := fixture(t)
	core.handler = func(_ uint64, method string, _, _ any) error {
		t.Fatalf("appearance reached wallet backend: %s", method)
		return nil
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	a.state.Locked = true
	a.fields["settings"] = "Appearance"
	a.active = "settings"
	view := a.view()
	if view.Appearance != webui.DefaultAppearance() || strings.Contains(viewText(view), "Retry storage check") {
		t.Fatal("locked appearance defaults or removed control changed")
	}
	var settings []webui.Node
	for _, page := range view.Pages {
		if page.ID == "settings" {
			settings = page.Nodes
		}
	}
	section, ok := findNode(settings, "settingsSections")
	if !ok || section.Value != "Appearance" {
		t.Fatal("settings selection has no explicit public state")
	}
	if _, ok := findNode(settings, "field/appearanceTheme"); !ok {
		t.Fatal("locked appearance controls missing")
	}
	a.event(webui.Event{ID: "field/appearanceTheme", Value: "dark"})
	a.event(webui.Event{ID: "field/appearanceFontSize", Value: "24"})
	got, err := webui.LoadAppearance()
	if err != nil || got != (webui.Appearance{Theme: "dark", FontSize: 24}) || a.view().Appearance != got {
		t.Fatal("public appearance did not persist", got, err)
	}
	for _, value := range []string{"25", "024", "10000", "<style>", ""} {
		a.event(webui.Event{ID: "field/appearanceFontSize", Value: value})
	}
	a.event(webui.Event{ID: "field/appearanceTheme", Value: "https://remote.invalid"})
	if a.view().Appearance != got {
		t.Fatal("unvalidated presentation reached view")
	}
	if raw, err := os.ReadFile(filepath.Join(root, "cryptowallet/ui/appearance.json")); err != nil || strings.Contains(string(raw), "account") {
		t.Fatal("public preference storage changed wallet state", err)
	}
	a.event(webui.Event{ID: "settingsWallet"})
	if text := viewText(a.settingsPage()); !strings.Contains(text, "Unlock the local wallet") || strings.Contains(text, "field/recipient") {
		t.Fatal("locked settings exposed wallet controls")
	}
}

func TestFailedAppearanceWritePreservesCurrentPresentation(t *testing.T) {
	a, _, _ := fixture(t)
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "cryptowallet"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "cryptowallet/ui")); err != nil {
		t.Fatal(err)
	}
	a.changeAppearance("appearanceTheme", "dark")
	if a.appearance != (webui.Appearance{}) || !strings.Contains(a.status, "could not be saved") {
		t.Fatal("failed write changed current presentation or hid failure")
	}
}
