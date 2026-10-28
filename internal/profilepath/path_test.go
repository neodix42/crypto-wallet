package profilepath

import (
	"bytes"
	"errors"
	"localwallet/internal/vault"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func legacyProfile(t *testing.T, config, network string) string {
	t.Helper()
	directory := filepath.Join(config, legacyName, network)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestMigrationPreservesCiphertextInodesBothProfilesAndPreferences(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config with spaces")
	password := []byte("synthetic-only-migration-password")
	plaintext := []byte("synthetic-only encrypted migration payload")
	ciphertext, err := vault.Seal(plaintext, password, vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"mainnet", "testnet"} {
		dir := legacyProfile(t, config, network)
		for _, name := range []string{"vault.lwv", "vault.previous.lwv", ".encrypted-interrupted"} {
			if err := os.WriteFile(filepath.Join(dir, name), ciphertext, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	ui := legacyProfile(t, config, "ui")
	prefs := []byte(`{"theme":"dark","fontSize":24}`)
	if err := os.WriteFile(filepath.Join(ui, "appearance.json"), prefs, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(filepath.Join(config, legacyName, "mainnet", "vault.lwv"))
	if err := Prepare(config); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(config, legacyName)); !os.IsNotExist(err) {
		t.Fatal("legacy root still exists", err)
	}
	after, _ := os.Stat(filepath.Join(Profile(config, "mainnet"), "vault.lwv"))
	if !os.SameFile(before, after) || after.Mode().Perm() != 0600 {
		t.Fatal("migration copied/replaced a vault inode or changed permissions")
	}
	for _, network := range []string{"mainnet", "testnet"} {
		for _, name := range []string{"vault.lwv", "vault.previous.lwv", ".encrypted-interrupted"} {
			data, err := os.ReadFile(filepath.Join(Profile(config, network), name))
			if err != nil || !bytes.Equal(data, ciphertext) {
				t.Fatal("migration changed ciphertext", network, name)
			}
			opened, err := vault.Open(data, password)
			if err != nil || !bytes.Equal(opened, plaintext) {
				t.Fatal("migrated vault cannot authenticate")
			}
			clear(opened)
		}
	}
	data, err := os.ReadFile(filepath.Join(config, Name, "ui", "appearance.json"))
	if err != nil || !bytes.Equal(data, prefs) || Prepare(config) != nil {
		t.Fatal("preferences or repeat startup changed")
	}
}

func TestFreshLookupCreatesNothingAndRejectsRelativeOrLinkedConfiguration(t *testing.T) {
	config := filepath.Join(t.TempDir(), "missing", "configuration")
	if err := Prepare(config); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(config)); !os.IsNotExist(err) {
		t.Fatal("fresh preparation created configuration")
	}
	if Prepare("relative") == nil || Prepare("") == nil {
		t.Fatal("relative configuration accepted")
	}
	base := t.TempDir()
	link := filepath.Join(base, "linked")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if Prepare(link) == nil {
		t.Fatal("linked configuration followed")
	}
}

func TestConflictNeverMergesOrReplacesEitherTree(t *testing.T) {
	config := t.TempDir()
	old := legacyProfile(t, config, "mainnet")
	current := Profile(config, "testnet")
	os.MkdirAll(current, 0700)
	os.WriteFile(filepath.Join(old, "sentinel"), []byte("old"), 0600)
	os.WriteFile(filepath.Join(current, "sentinel"), []byte("current"), 0600)
	if !errors.Is(Prepare(config), ErrConflict) {
		t.Fatal("simultaneous roots did not stop migration")
	}
	for name, want := range map[string]string{filepath.Join(old, "sentinel"): "old", filepath.Join(current, "sentinel"): "current"} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatal("conflict modified original data")
		}
	}
	if _, err := os.Stat(filepath.Join(old, "writer.lock")); !os.IsNotExist(err) {
		t.Fatal("conflict changed a source profile")
	}
}

func TestBusyWriterInEitherNetworkPreventsMigrationUntilClosed(t *testing.T) {
	for _, network := range []string{"mainnet", "testnet"} {
		t.Run(network, func(t *testing.T) {
			config := t.TempDir()
			legacyProfile(t, config, "mainnet")
			dir := legacyProfile(t, config, network)
			writer, err := vault.NewFiles(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(Prepare(config), ErrBusy) {
				t.Fatal("active vault moved")
			}
			if _, err := os.Stat(filepath.Join(config, Name)); !os.IsNotExist(err) {
				t.Fatal("busy migration created destination")
			}
			writer.Close()
			if err := Prepare(config); err != nil {
				t.Fatal("closed writer prevented retry", err)
			}
		})
	}
}

func TestUnsafeEntriesAndPermissionsAreNotFollowedOrMoved(t *testing.T) {
	for _, kind := range []string{"root-link", "destination-link", "profile-link", "ui-link", "root-public", "profile-public", "lock-link", "lock-hardlink", "lock-fifo", "lock-public", "configuration-writable"} {
		t.Run(kind, func(t *testing.T) {
			config := t.TempDir()
			dir := legacyProfile(t, config, "mainnet")
			outside := t.TempDir()
			target := filepath.Join(outside, "untouched")
			os.WriteFile(target, []byte("public-test-sentinel"), 0600)
			lock := filepath.Join(dir, "writer.lock")
			switch kind {
			case "root-link":
				os.Remove(dir)
				os.Remove(filepath.Dir(dir))
				os.Symlink(outside, filepath.Dir(dir))
			case "destination-link":
				os.Symlink(outside, filepath.Join(config, Name))
			case "profile-link":
				os.Remove(dir)
				os.Symlink(outside, dir)
			case "ui-link":
				os.Symlink(outside, filepath.Join(config, legacyName, "ui"))
			case "root-public":
				os.Chmod(filepath.Dir(dir), 0755)
			case "profile-public":
				os.Chmod(dir, 0755)
			case "lock-link":
				os.Symlink(target, lock)
			case "lock-hardlink":
				os.Link(target, lock)
			case "lock-fifo":
				unix.Mkfifo(lock, 0600)
			case "lock-public":
				os.WriteFile(lock, nil, 0644)
			case "configuration-writable":
				os.Chmod(config, 0777)
			}
			if Prepare(config) == nil {
				t.Fatal("unsafe migration accepted", kind)
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "public-test-sentinel" {
				t.Fatal("migration followed/modified unrelated file")
			}
			if _, err := os.Lstat(filepath.Join(config, legacyName)); err != nil {
				t.Fatal("unsafe legacy tree moved")
			}
		})
	}
}

func TestConcurrentMigrationsLeaveExactlyOneIntactDirectory(t *testing.T) {
	config := t.TempDir()
	dir := legacyProfile(t, config, "mainnet")
	os.WriteFile(filepath.Join(dir, "sentinel"), []byte("unchanged"), 0600)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := Prepare(config); err != nil && !errors.Is(err, ErrBusy) {
				t.Error("concurrent migration", err)
			}
		}()
	}
	workers.Wait()
	data, err := os.ReadFile(filepath.Join(Profile(config, "mainnet"), "sentinel"))
	if err != nil || string(data) != "unchanged" {
		t.Fatal("concurrent migration lost original data")
	}
}

func TestDestinationAppearingAtCommitCannotBeOverwrittenEvenWhenEmpty(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		config := t.TempDir()
		dir := legacyProfile(t, config, "mainnet")
		if err := os.WriteFile(filepath.Join(dir, "original"), []byte("preserve-source"), 0600); err != nil {
			t.Fatal(err)
		}
		parent, err := openDirectory(config)
		if err != nil {
			t.Fatal(err)
		}
		old, _, err := directoryEntry(parent, legacyName)
		if err != nil {
			t.Fatal(err)
		}
		// Model a noncooperating process creating the destination after the
		// initial existence check, immediately before the actual commit.
		current := filepath.Join(config, Name)
		if err := os.Mkdir(current, 0700); err != nil {
			t.Fatal(err)
		}
		if nonempty {
			if err := os.WriteFile(filepath.Join(current, "original"), []byte("preserve-destination"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		before, _ := os.Stat(current)
		err = commitRename(parent, old)
		unix.Close(parent)
		after, _ := os.Stat(current)
		if !errors.Is(err, ErrConflict) || !os.SameFile(before, after) {
			t.Fatal("commit replaced a newly appeared destination", nonempty, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "original"))
		if err != nil || string(data) != "preserve-source" {
			t.Fatal("commit modified original source")
		}
		if nonempty {
			data, err := os.ReadFile(filepath.Join(current, "original"))
			if err != nil || string(data) != "preserve-destination" {
				t.Fatal("commit modified original destination")
			}
		}
	}
}

func TestSourceReplacedAtCommitDoesNotSelectAReplacementWallet(t *testing.T) {
	config := t.TempDir()
	legacyProfile(t, config, "mainnet")
	parent, err := openDirectory(config)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(parent)
	old, _, err := directoryEntry(parent, legacyName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(config, legacyName), filepath.Join(config, "preserved-original")); err != nil {
		t.Fatal(err)
	}
	legacyProfile(t, config, "testnet")
	if !errors.Is(commitRename(parent, old), ErrStorage) {
		t.Fatal("a replacement source was committed as the original wallet")
	}
	for _, name := range []string{legacyName, "preserved-original"} {
		if _, err := os.Stat(filepath.Join(config, name)); err != nil {
			t.Fatal("source race removed a preserved tree")
		}
	}
	if _, err := os.Stat(filepath.Join(config, Name)); !os.IsNotExist(err) {
		t.Fatal("source race created destination storage")
	}
}
