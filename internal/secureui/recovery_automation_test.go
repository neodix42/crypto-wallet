//go:build automation

package secureui

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeRecoveryRequiresRecordedWordsAndClearsWidgets(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	// These published all-zero-entropy BIP39 vectors are public synthetic
	// fixtures. No screenshot, OCR, file, clipboard or generated user root is used.
	for _, words := range []int{12, 24} {
		last := "about"
		if words == 24 {
			last = "art"
		}
		phrase := []byte(strings.Repeat("abandon ", words-1) + last)
		original := bytes.Clone(phrase)
		defer clear(phrase)
		defer clear(original)
		for mode := 0; mode < 5; mode++ {
			const prior = "public synthetic prior clipboard 95148"
			(Native{}).CopyPublic(prior)
			var activity atomic.Int32
			SetActivity(func() { activity.Add(1) })
			autoScheduleRecovery(mode, words)
			accepted := (Native{}).Recovery("TEST record public synthetic recovery vector", phrase)
			autoCancelRecovery()
			SetActivity(nil)
			ok, clearedLabels, clearedEntries, phraseLabels, width, height, checkWidth, checkHeight, dialogs := autoRecoveryResults()
			wantEntries, wantDialogs := 3, 2
			if mode == 1 {
				wantEntries, wantDialogs = 0, 1
			} else if mode >= 3 {
				wantDialogs = 3 // Fixed notice contains no entered value.
			}
			if accepted != (mode == 0) || !ok || clearedLabels != 1 || clearedEntries != wantEntries || phraseLabels != 1 || dialogs != wantDialogs {
				t.Fatalf("recovery native consent/masking/clearing violation: words=%d mode=%d accepted=%v safe=%v labels=%d entries=%d shown=%d dialogs=%d", words, mode, accepted, ok, clearedLabels, clearedEntries, phraseLabels, dialogs)
			}
			if autoPublicClipboard() != prior || activity.Load() != 0 || !bytes.Equal(phrase, original) {
				t.Fatal("native recovery changed clipboard/caller bytes or counted programmatic events as user input")
			}
			if width > 650 || height > 600 || checkWidth > 650 || checkHeight > 480 {
				t.Fatalf("native recovery exceeded its bounded dialog sizes: phrase=%dx%d challenge=%dx%d", width, height, checkWidth, checkHeight)
			}
			t.Logf("words=%d mode=%d phrase=%dx%d challenge=%dx%d cleared labels=%d entries=%d", words, mode, width, height, checkWidth, checkHeight, clearedLabels, clearedEntries)
		}
	}
	for _, invalid := range [][]byte{
		nil, []byte("invalid"), []byte(strings.Repeat("abandon ", 11) + "About"),
		[]byte(strings.Repeat("abandon ", 11) + "about\x00"),
		[]byte(strings.Repeat("abandon ", 11) + "about "),
		[]byte(strings.Repeat("abandon ", 11) + " about"),
		[]byte(strings.Repeat("abandon ", 11) + strings.Repeat("x", 257)),
	} {
		if (Native{}).Recovery("TEST invalid recovery must not display", invalid) {
			t.Fatal("invalid native phrase format accepted")
		}
	}
}
