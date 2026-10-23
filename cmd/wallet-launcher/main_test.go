package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Public fake executables test process isolation/pipe bootstrap only, never
// blockchain acceptance. These init hooks exist exclusively in the test binary.
func init() {
	switch filepath.Base(os.Args[0]) {
	case "launcher-test-entry":
		main()
		os.Exit(0)
	case "wallet-core":
		if len(os.Args) != 3 || os.Args[1] != "--environment" || (os.Args[2] != "mainnet" && os.Args[2] != "testnet") {
			os.Exit(91)
		}
		// The public coordinator starts before the renderer's IP restriction.
		fd, e := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if e != nil {
			os.Exit(96)
		}
		syscall.Close(fd)
		for _, k := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "NODE_OPTIONS", "ELECTRON_RUN_AS_NODE", "LOCALWALLET_DEV"} {
			if _, present := os.LookupEnv(k); present {
				os.Exit(97)
			}
		}
		os.Stdout.Write([]byte("PUBLIC-BOOT" + strings.ToUpper(os.Args[2][:1])))
		var b [1]byte
		io.ReadFull(os.Stdin, b[:])
		os.Exit(0)
	case "LocalWallet":
		if os.Getenv("LOCALWALLET_INHERITED_CORE") != "1" {
			os.Exit(92)
		}
		for _, af := range []int{syscall.AF_INET, syscall.AF_INET6} {
			fd, e := syscall.Socket(af, syscall.SOCK_STREAM, 0)
			if e == nil {
				syscall.Close(fd)
				os.Exit(93)
			}
		}
		fd, e := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if e != nil {
			os.Exit(94)
		}
		syscall.Close(fd)
		var b [12]byte
		if _, e = io.ReadFull(os.NewFile(3, "private-core-output"), b[:]); e != nil || !bytes.Equal(b[:], []byte("PUBLIC-BOOTM")) {
			os.Exit(95)
		}
		os.NewFile(4, "private-core-input").Write([]byte{1})
		os.Exit(0)
	}
}
func TestTP041LauncherExecInheritanceAndFixedPipes(t *testing.T) {
	if os.Getuid() == 0 {
		t.Fatal("BLOCKED: launcher must be tested as non-root")
	}
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	binary, e := os.ReadFile(self)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "resources/bin")
	if e = os.MkdirAll(bin, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"launcher-test-entry", "LocalWallet", "resources/bin/wallet-core", "resources/bin/wallet-signer"} {
		if e = os.WriteFile(filepath.Join(dir, name), binary, 0700); e != nil {
			t.Fatal(e)
		}
	}
	sum := sha256.Sum256(binary)
	manifest, _ := json.Marshal(map[string]string{"wallet-core": hex.EncodeToString(sum[:]), "wallet-signer": hex.EncodeToString(sum[:])})
	if e = os.WriteFile(filepath.Join(bin, "manifest.json"), manifest, 0600); e != nil {
		t.Fatal(e)
	}
	for attempt := 0; attempt < 32; attempt++ {
		cmd := exec.Command(filepath.Join(dir, "launcher-test-entry"))
		cmd.Env = append(os.Environ(), "LD_PRELOAD=synthetic-ignored-by-launcher")
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("public bootstrap attempt %d failed: %v %s", attempt, e, b)
		}
	}
	if e = os.WriteFile(filepath.Join(bin, "wallet-signer"), []byte("tampered public test executable"), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(filepath.Join(dir, "launcher-test-entry"))
	if e = cmd.Run(); e == nil {
		t.Fatal("tampered packaged child launched")
	}
}
