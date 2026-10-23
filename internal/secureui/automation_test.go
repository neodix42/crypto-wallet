//go:build automation

package secureui

import (
	"bytes"
	"testing"
)

func TestTP033RealNativeMaskedInput(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED: native display unavailable: %v", err)
	}
	const canary = "public synthetic native input canary 79326"
	autoSchedule(canary, -3)
	in, err := Input("TEST — synthetic masked input", "Synthetic password", "Synthetic phrase")
	if err != nil || len(in) != 2 {
		t.Fatal("native input completion")
	}
	if !autoMasked() {
		t.Fatal("native input had reveal/length violation")
	}
	for _, b := range in {
		if !bytes.Equal(b, []byte(canary)) {
			t.Fatal("native input changed bytes")
		}
		clear(b)
	}
	autoSchedule(canary, -6)
	if out, err := Input("TEST — cancel synthetic input", "Synthetic password"); err == nil || out != nil {
		t.Fatal("cancel exposed input")
	}
}
func TestTP042RealNativeConfirmation(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	autoSchedule("", -6)
	if Confirm("TEST synthetic transfer", "Public test source and exact recipient") {
		t.Fatal("cancel approved")
	}
	autoSchedule("", -3)
	if !Confirm("TEST synthetic transfer", "Public test source and exact recipient") {
		t.Fatal("native approval event")
	}
}
