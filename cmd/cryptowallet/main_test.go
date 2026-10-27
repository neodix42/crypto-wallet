package main

import (
	"bytes"
	"errors"
	"localwallet/internal/buildinfo"
	"localwallet/internal/redistribution"
	"os"
	"reflect"
	"strings"
	"testing"
)

func testRuntime(t *testing.T) (commandRuntime, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, diagnostic := new(bytes.Buffer), new(bytes.Buffer)
	unexpected := func() { t.Fatal("rejected/public CLI unexpectedly entered wallet role") }
	return commandRuntime{stdout: out, stderr: diagnostic, uid: func() int { return 1000 },
		protect: func() error { return nil }, desktop: func(string) error { unexpected(); return nil },
		coordinator: unexpected, signer: unexpected}, out, diagnostic
}

func TestPublicInspectionDoesNotOpenWalletDisplayOrProtectMemory(t *testing.T) {
	for _, row := range []struct{ flag, expected string }{{"--version", buildinfo.Version + "\n"}, {"--runtime-info", "native-gtk3; portable-go; sandbox=none\n"}, {"--licenses", redistribution.Text}} {
		t.Run(row.flag, func(t *testing.T) {
			r, out, diagnostic := testRuntime(t)
			r.uid = func() int { t.Fatal("public inspection queried execution identity"); return 0 }
			r.protect = func() error { t.Fatal("public inspection changed process protection"); return nil }
			if code := run([]string{"cryptowallet", row.flag}, r); code != 0 || out.String() != row.expected || diagnostic.Len() != 0 {
				t.Fatal("public inspection output, exit or dependencies changed")
			}
		})
	}
}

func TestRootAndMemoryProtectionFailuresCannotOpenWallet(t *testing.T) {
	r, _, diagnostic := testRuntime(t)
	r.uid = func() int { return 0 }
	r.protect = func() error { t.Fatal("root execution proceeded to memory protection"); return nil }
	if run([]string{"cryptowallet"}, r) != 2 || !strings.Contains(diagnostic.String(), "ordinary desktop user") {
		t.Fatal("root execution did not fail before wallet startup")
	}
	r, _, diagnostic = testRuntime(t)
	r.protect = func() error { return errors.New("private-platform-error-canary") }
	if run([]string{"cryptowallet"}, r) != 3 || !strings.Contains(diagnostic.String(), "cannot protect") || strings.Contains(diagnostic.String(), "canary") {
		t.Fatal("failed memory protection opened a wallet or disclosed detail")
	}
}

func TestOnlyExplicitMainnetOrTestnetProfilesReachNativeDesktop(t *testing.T) {
	for _, row := range []struct {
		args    []string
		profile string
	}{{[]string{"cryptowallet"}, "mainnet"}, {[]string{"cryptowallet", "--profile=mainnet"}, "mainnet"}, {[]string{"cryptowallet", "--profile=testnet"}, "testnet"}} {
		r, out, diagnostic := testRuntime(t)
		calls := 0
		r.desktop = func(profile string) error {
			calls++
			if profile != row.profile {
				t.Error("profile changed before desktop startup")
			}
			return nil
		}
		if code := run(row.args, r); code != 0 || calls != 1 || out.Len() != 0 || diagnostic.Len() != 0 {
			t.Fatal("valid profile failed or started duplicate desktops")
		}
	}
	for _, args := range [][]string{nil, {"cryptowallet", "--profile=other"}, {"cryptowallet", "--no-sandbox"}, {"cryptowallet", "--ozone-platform=x11"}, {"cryptowallet", "--profile=testnet", "extra"}} {
		r, out, diagnostic := testRuntime(t)
		if run(args, r) != 2 || !strings.Contains(diagnostic.String(), "Usage:") || out.Len() != 0 {
			t.Fatal("unsupported arguments were accepted")
		}
	}
}

func TestDesktopFailureHasFixedErrorAndNoFallbackOrUserDetails(t *testing.T) {
	r, out, diagnostic := testRuntime(t)
	calls := 0
	r.desktop = func(string) error { calls++; return errors.New("private-runtime-canary") }
	if run([]string{"cryptowallet"}, r) != 4 || calls != 1 || out.Len() != 0 || strings.Contains(diagnostic.String(), "canary") || !strings.Contains(diagnostic.String(), "GTK runtime") {
		t.Fatal("desktop failure leaked detail or attempted a fallback")
	}
}

func TestInternalRolesReceiveOnlyTheirOwnTrailingArguments(t *testing.T) {
	for _, role := range []string{"--internal-core", "--internal-signer"} {
		r, out, diagnostic := testRuntime(t)
		argsBefore := append([]string(nil), os.Args...)
		calls := 0
		callback := func() {
			calls++
			if !reflect.DeepEqual(os.Args, []string{"cryptowallet", "--environment", "testnet"}) {
				t.Error("private role prefix or profile argument changed")
			}
		}
		if role == "--internal-core" {
			r.coordinator = callback
		} else {
			r.signer = callback
		}
		if run([]string{"cryptowallet", role, "--environment", "testnet"}, r) != 0 || calls != 1 || out.Len() != 0 || diagnostic.Len() != 0 || !reflect.DeepEqual(os.Args, argsBefore) {
			t.Fatal("internal role dispatch or argument restoration failed")
		}
	}
}

func TestActualMainPublicVersionUsesProductionDependenciesWithoutOpeningWallet(t *testing.T) {
	originalArgs, originalOut := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = originalArgs, originalOut })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal("public fixture pipe unavailable")
	}
	defer read.Close()
	os.Args, os.Stdout = []string{"cryptowallet", "--version"}, write
	main()
	write.Close()
	var result bytes.Buffer
	result.ReadFrom(read)
	if result.String() != buildinfo.Version+"\n" {
		t.Fatal("actual public main version changed")
	}
}
