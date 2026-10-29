package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func appearanceHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	return filepath.Join(root, "cryptowallet", "ui")
}

func TestAppearanceDefaultsDoNotCreateAProfileAndPublicPreferencesSurviveRestart(t *testing.T) {
	directory := appearanceHome(t)
	got, err := LoadAppearance()
	if err != nil || got != DefaultAppearance() {
		t.Fatal("fresh appearance defaults", got, err)
	}
	if _, err := os.Stat(filepath.Dir(directory)); !os.IsNotExist(err) {
		t.Fatal("reading absent public preferences created wallet state")
	}
	for _, want := range []Appearance{{"dark", 24}, {"light", 18}, {"dark", 14}} {
		if err := SaveAppearance(want); err != nil {
			t.Fatal(err)
		}
		got, err := LoadAppearance()
		if err != nil || got != want {
			t.Fatal("appearance did not survive independent reload", got, err)
		}
		st, _ := os.Stat(filepath.Join(directory, "appearance.json"))
		if st.Mode().Perm() != 0600 {
			t.Fatal("public preferences are not owner-only")
		}
		entries, _ := os.ReadDir(directory)
		if len(entries) != 1 || entries[0].Name() != "appearance.json" {
			t.Fatal("atomic update left staging files")
		}
		raw, _ := os.ReadFile(filepath.Join(directory, "appearance.json"))
		var fields map[string]any
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["theme"] != want.Theme || fields["fontSize"] != float64(want.FontSize) {
			t.Fatal("preference storage contains something other than public appearance")
		}
	}
}

func TestAppearanceRejectsUntrustedValuesAndUnsafeFilesWithoutFollowingThem(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "directory", "public-mode", "oversize", "duplicate", "unknown", "invalid-theme", "invalid-size", "truncated", "empty"} {
		t.Run(kind, func(t *testing.T) {
			directory := appearanceHome(t)
			if err := SaveAppearance(DefaultAppearance()); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(directory, "appearance.json")
			target := filepath.Join(t.TempDir(), "untouched")
			original := []byte(`{"theme":"light","fontSize":18}`)
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			os.Remove(file)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, file)
			case "hardlink":
				err = os.Link(target, file)
			case "fifo":
				err = unix.Mkfifo(file, 0600)
			case "directory":
				err = os.Mkdir(file, 0700)
			default:
				data := map[string]string{"public-mode": string(original), "oversize": strings.Repeat(" ", 129), "duplicate": `{"theme":"light","theme":"dark","fontSize":18}`, "unknown": `{"theme":"light","fontSize":18,"password":"SYNTHETIC"}`, "invalid-theme": `{"theme":"remote-css","fontSize":18}`, "invalid-size": `{"theme":"light","fontSize":1000000}`, "truncated": `{`, "empty": ""}[kind]
				err = os.WriteFile(file, []byte(data), 0600)
				if kind == "public-mode" {
					err = os.Chmod(file, 0644)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := LoadAppearance()
			if err == nil || got != DefaultAppearance() {
				t.Fatal("unsafe preferences accepted", got, err)
			}
			if SaveAppearance(Appearance{"dark", 20}) == nil {
				t.Fatal("unsafe existing preference entry replaced")
			}
			bytes, _ := os.ReadFile(target)
			if string(bytes) != string(original) {
				t.Fatal("preference update modified another file")
			}
		})
	}
	for _, value := range []Appearance{{"", 18}, {"LIGHT", 18}, {"dark", 15}, {"light", 12}, {"light", 26}} {
		if value.Valid() || value.Normalized() != DefaultAppearance() || SaveAppearance(value) == nil {
			t.Fatal("unbounded appearance accepted", value)
		}
	}
}

func TestAppearanceRejectsSymlinkedDirectoriesAndDoesNotUseRelativeConfiguration(t *testing.T) {
	directory := appearanceHome(t)
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAppearance(); err == nil {
		t.Fatal("symlinked preference directory read")
	}
	if SaveAppearance(DefaultAppearance()) == nil {
		t.Fatal("symlinked preference directory written")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("preferences followed a directory symlink")
	}
	os.Remove(directory)
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAppearance(); err == nil {
		t.Fatal("nonprivate UI directory accepted")
	}
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	if _, err := LoadAppearance(); err == nil {
		t.Fatal("relative preference directory accepted")
	}
	if SaveAppearance(DefaultAppearance()) == nil {
		t.Fatal("relative preference directory created")
	}
}

func TestAppearanceMissingFileDefaultsAndInvalidPublicViewIsRejected(t *testing.T) {
	directory := appearanceHome(t)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadAppearance(); err != nil || got != DefaultAppearance() {
		t.Fatal("missing public preference entry is not fresh defaults", got, err)
	}
	w, _ := New(nil)
	defer w.Close()
	initial := publicFixture()
	initial.Appearance = DefaultAppearance()
	w.Render(initial)
	broken := initial
	broken.Appearance = Appearance{Theme: "remote-css", FontSize: 18}
	w.Render(broken)
	if got := (&PublicBridge{window: w}).Snapshot().Appearance; got != initial.Appearance {
		t.Fatal("unvalidated appearance replaced public view")
	}
}
