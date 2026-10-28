package webui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"localwallet/internal/profilepath"
	"localwallet/internal/strictjson"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Appearance is public presentation data. It never contains wallet state or
// credentials and cannot select CSS, URLs, fonts, paths or executable code.
type Appearance struct {
	Theme    string `json:"theme"`
	FontSize int    `json:"fontSize"`
}

func DefaultAppearance() Appearance { return Appearance{Theme: "light", FontSize: 18} }
func (a Appearance) Valid() bool {
	return (a.Theme == "light" || a.Theme == "dark") && a.FontSize >= 14 && a.FontSize <= 24 && a.FontSize%2 == 0
}
func (a Appearance) Normalized() Appearance {
	if !a.Valid() {
		return DefaultAppearance()
	}
	return a
}

var errPreferences = errors.New("public appearance preferences unavailable")

func appearanceDirectory(create bool) (*os.File, error) {
	config, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(config) {
		return nil, errPreferences
	}
	if profilepath.Prepare(config) != nil {
		return nil, errPreferences
	}
	fd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errPreferences
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Join(config, profilepath.Name, "ui"), "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e == unix.ENOENT && create {
			if e = unix.Mkdirat(fd, part, 0700); e == nil || e == unix.EEXIST {
				next, e = unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			if e == unix.ENOENT {
				return nil, os.ErrNotExist
			}
			return nil, errPreferences
		}
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 {
		unix.Close(fd)
		return nil, errPreferences
	}
	return os.NewFile(uintptr(fd), "public-ui-preferences"), nil
}

func readAppearance(dir *os.File) (Appearance, error) {
	fd, err := unix.Openat(int(dir.Fd()), "appearance.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if err == unix.ENOENT {
			return DefaultAppearance(), os.ErrNotExist
		}
		return DefaultAppearance(), errPreferences
	}
	f := os.NewFile(uintptr(fd), "appearance.json")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Nlink != 1 || st.Size < 1 || st.Size > 128 {
		return DefaultAppearance(), errPreferences
	}
	raw, err := io.ReadAll(io.LimitReader(f, 129))
	var a Appearance
	if err != nil || len(raw) > 128 || strictjson.Decode(raw, &a) != nil || !a.Valid() {
		return DefaultAppearance(), errPreferences
	}
	return a, nil
}

func LoadAppearance() (Appearance, error) {
	dir, err := appearanceDirectory(false)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultAppearance(), nil
	}
	if err != nil {
		return DefaultAppearance(), err
	}
	defer dir.Close()
	a, err := readAppearance(dir)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return a, err
}

func SaveAppearance(a Appearance) error {
	if !a.Valid() {
		return errPreferences
	}
	dir, err := appearanceDirectory(true)
	if err != nil {
		return err
	}
	defer dir.Close()
	// Refuse existing symlinks, hardlinks and nonregular files before replacing
	// an entry. The directory handle pins every later operation to this owner.
	if _, err = readAppearance(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return errPreferences
	}
	name := ".appearance-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errPreferences
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { f.Close(); unix.Unlinkat(int(dir.Fd()), name, 0) }()
	raw, _ := json.Marshal(a)
	if n, err := f.Write(raw); err != nil || n != len(raw) || f.Sync() != nil || f.Close() != nil {
		return errPreferences
	}
	if unix.Renameat(int(dir.Fd()), name, int(dir.Fd()), "appearance.json") != nil || dir.Sync() != nil {
		return errPreferences
	}
	return nil
}

func privateAppearance() Appearance {
	a := Appearance{Theme: os.Getenv("LOCALWALLET_UI_THEME")}
	a.FontSize, _ = strconv.Atoi(os.Getenv("LOCALWALLET_UI_FONT_SIZE"))
	return a.Normalized()
}
