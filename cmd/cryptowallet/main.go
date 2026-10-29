// CryptoWallet is one native Go executable. Internal roles use the same live
// image over inherited private pipes; no Electron runtime or sandbox helper.
package main

import (
	"errors"
	"fmt"
	"io"
	"localwallet/internal/buildinfo"
	"localwallet/internal/isolation"
	"localwallet/internal/nativewallet"
	"localwallet/internal/profilepath"
	"localwallet/internal/redistribution"
	"localwallet/internal/walletcore"
	"localwallet/internal/walletsigner"
	"os"
)

// commandRuntime keeps CLI policy separate from process termination and GUI
// startup. It is constructed here only, never selected through user input.
// Unit tests can exercise rejected CLI/root/memory conditions without opening
// a wallet or changing the actual process's memory protection.
type commandRuntime struct {
	stdout, stderr io.Writer
	uid            func() int
	protect        func() error
	desktop        func(string) error
	coordinator    func()
	signer         func()
}

func main() {
	code := run(os.Args, commandRuntime{stdout: os.Stdout, stderr: os.Stderr,
		uid: os.Getuid, protect: isolation.ProtectMemory, desktop: nativewallet.Run,
		coordinator: walletcore.MainPortable, signer: walletsigner.MainPortable})
	if code != 0 {
		os.Exit(code)
	}
}

func run(args []string, runtime commandRuntime) int {
	if len(args) == 2 {
		switch args[1] {
		case "--version":
			fmt.Fprintln(runtime.stdout, buildinfo.Version)
			return 0
		case "--licenses":
			fmt.Fprint(runtime.stdout, redistribution.Text)
			return 0
		case "--runtime-info":
			fmt.Fprintln(runtime.stdout, "wails-webkitgtk4.1; portable-go; sandbox=none")
			return 0
		}
	}
	if runtime.uid() == 0 {
		fmt.Fprintln(runtime.stderr, "Run CryptoWallet as your ordinary desktop user.")
		return 2
	}
	if runtime.protect() != nil {
		fmt.Fprintln(runtime.stderr, "CryptoWallet cannot protect its process memory.")
		return 3
	}
	if len(args) > 1 {
		role := args[1]
		if role == "--internal-core" || role == "--internal-signer" {
			original := os.Args
			os.Args = append([]string{args[0]}, args[2:]...)
			defer func() { os.Args = original }()
			if role == "--internal-core" {
				runtime.coordinator()
			} else {
				runtime.signer()
			}
			return 0
		}
	}
	environment := "mainnet"
	if len(args) == 2 && (args[1] == "--profile=mainnet" || args[1] == "--profile=testnet") {
		environment = args[1][10:]
	} else if len(args) != 1 {
		fmt.Fprintln(runtime.stderr, "Usage: cryptowallet [--profile=mainnet|--profile=testnet] [--version|--licenses|--runtime-info]")
		return 2
	}
	if err := runtime.desktop(environment); err != nil {
		switch {
		case errors.Is(err, profilepath.ErrConflict):
			fmt.Fprintln(runtime.stderr, "CryptoWallet found both localwallet and cryptowallet storage directories. Keep both intact and resolve their locations before restarting; see README.md.")
		case errors.Is(err, profilepath.ErrBusy):
			fmt.Fprintln(runtime.stderr, "CryptoWallet storage is busy. Close other wallet processes before restarting.")
		case errors.Is(err, profilepath.ErrStorage):
			fmt.Fprintln(runtime.stderr, "CryptoWallet could not prepare storage. Check storage ownership, private permissions and configuration paths; see README.md.")
		default:
			fmt.Fprintln(runtime.stderr, "CryptoWallet could not open the desktop. Check the WebKitGTK runtime libraries and your desktop display.")
		}
		return 4
	}
	return 0
}
