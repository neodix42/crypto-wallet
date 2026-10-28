package webui

import (
	"context"
	"encoding/json"
	"localwallet/internal/uiprompt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func appearanceHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	return filepath.Join(root, "localwallet", "ui")
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

func TestPrivatePresentationIsBoundedAndCannotChangePromptAuthority(t *testing.T) {
	t.Setenv("LOCALWALLET_UI_THEME", "dark")
	t.Setenv("LOCALWALLET_UI_FONT_SIZE", "24")
	if got := privateAppearance(); got != (Appearance{"dark", 24}) {
		t.Fatal(got)
	}
	t.Setenv("LOCALWALLET_UI_FONT_SIZE", "unbounded")
	if got := privateAppearance(); got != DefaultAppearance() {
		t.Fatal("invalid inherited style accepted", got)
	}
	b := &PrivateBridge{prompt: promptFixture(), appearance: Appearance{"dark", 20}}
	p, err := b.Prompt()
	if err != nil || p.ID != b.prompt.ID || p.Appearance != b.appearance {
		t.Fatal("appearance not delivered with exact one-use prompt", err)
	}
	if _, err := b.Prompt(); err == nil {
		t.Fatal("presentation extension replayed private prompt")
	}
	for _, font := range []int{14, 18, 24} {
		for _, count := range []int{1, 2, 8} {
			prompt := promptFixture()
			prompt.Labels = make([]string, count)
			a := privateAppOptions(prompt, Appearance{"light", font})
			if a.Width != 680 || a.Height < 294 || a.Height > 850 || len(a.Bind) != 0 {
				t.Fatal("private sizing changed authority", a.Height)
			}
			if count == 2 && font >= 18 && a.Height < 474 {
				t.Fatal("Bybit credential footer has insufficient space")
			}
			width, height := windowSizeForMonitor(a.Width, a.Height, 640, 480)
			if width != 608 || height > 416 || height > a.Height {
				t.Fatal("private view exceeds logical monitor bounds")
			}
		}
	}
	if width, height := windowSizeForMonitor(680, 474, 0, 0); width != 680 || height != 474 {
		t.Fatal("invalid monitor dimensions changed window")
	}
}

func TestPrivatePresentationCrossesOnlyTheSanitizedOwnedProcessBoundary(t *testing.T) {
	t.Setenv("LOCALWALLET_UI_THEME", "https://inherited.invalid")
	t.Setenv("LOCALWALLET_UI_FONT_SIZE", "999999")
	for _, appearance := range []Appearance{{"dark", 24}, {"light", 18}, {"remote-style", 1000}} {
		p := promptFixture()
		p.Title = "presentation:owned-style"
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		reply, err := runPromptProcess(ctx, p, make(chan struct{}, 1), func() bool { t.Fatal("input prompt acquired clipboard authority"); return false }, appearance)
		cancel()
		want := appearance.Normalized()
		if err != nil || !reply.Accepted || len(reply.Values) != 1 || reply.Values[0] != want.Theme+":"+strconv.Itoa(want.FontSize) {
			t.Fatal("private process inherited unvalidated or stale presentation", err)
		}
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

func TestPrivatePresentationActualJSONPreservesStrictPromptFieldsAndOneUseDelivery(t *testing.T) {
	for _, kind := range []string{"input", "confirm", "notice", "receive", "recovery", "recoveryBackup"} {
		t.Run(kind, func(t *testing.T) {
			p := promptFixture()
			p.Kind = kind
			if kind != "input" {
				p.Labels = nil
			}
			if kind == "receive" {
				p = receiveFixture(t, 1)
			}
			if kind == "recovery" || kind == "recoveryBackup" {
				p.Phrase = strings.TrimSpace(strings.Repeat("abandon ", 11) + "about")
			}
			b := &PrivateBridge{prompt: p, appearance: Appearance{"dark", 24}}
			view, err := b.Prompt()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(raw)
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || string(fields["appearance"]) != `{"theme":"dark","fontSize":24}` {
				t.Fatal("actual private JSON lost the selected appearance")
			}
			original, _ := json.Marshal(p)
			defer clear(original)
			var expected map[string]json.RawMessage
			if json.Unmarshal(original, &expected) != nil || len(fields) != len(expected)+1 {
				t.Fatal("presentation changed the strict prompt field set")
			}
			for key, value := range expected {
				if string(fields[key]) != string(value) {
					t.Fatal("presentation changed signer-authored prompt field", key)
				}
			}
			delete(fields, "appearance")
			promptOnly, _ := json.Marshal(fields)
			defer clear(promptOnly)
			decoded, err := uiprompt.DecodePrompt(promptOnly)
			if err != nil || decoded.ID != p.ID || decoded.Kind != p.Kind || decoded.Summary != p.Summary {
				t.Fatal("style extension changed private prompt authority", err)
			}
			if _, err := b.Prompt(); err == nil {
				t.Fatal("serialized private prompt could be delivered twice")
			}
		})
	}
}
