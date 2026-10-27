package walletcore

import (
	"io"
	"os"
	"testing"

	"localwallet/internal/buildinfo"
)

func TestReadOnlyVersionBeforeInitialization(t *testing.T) {
	configuration := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configuration)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	beforeArgs, beforeOutput := os.Args, os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	defer func() { os.Args, os.Stdout = beforeArgs, beforeOutput }()
	os.Args = []string{"wallet-core", "--version"}
	os.Stdout = writer
	main()
	writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil || string(output) != buildinfo.Version+"\n" || buildinfo.Version == "" {
		t.Fatal("version did not report embedded build identity", string(output), err)
	}
	entries, err := os.ReadDir(configuration)
	if err != nil || len(entries) != 0 {
		t.Fatal("version initialized or modified a profile", err)
	}
}
