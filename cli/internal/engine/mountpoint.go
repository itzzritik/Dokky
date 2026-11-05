package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNameLen   = 40
	mountRootDir = "dokky"
)

var driveLetter = regexp.MustCompile(`^[A-Za-z]:\\?$`)

func ValidName(name string) error {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameLen || strings.ContainsFunc(name, func(r rune) bool { return r == '/' || r == ':' || unicode.IsControl(r) }) {
		return fmt.Errorf("a drive name must be 1 to %d characters, without / or :", maxNameLen)
	}
	return nil
}

func SafeFolder(name, goos string) string {
	bad := "/:"
	if goos == "windows" {
		bad += `<>"\|?*`
	}
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(bad, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if s = strings.TrimRight(s, ". "); s == "" {
		return "Drive"
	}
	return s
}

func DefaultMountPoint(home, name, goos string) string {
	if goos == "windows" {
		return "*"
	}
	return filepath.Join(home, mountRootDir, SafeFolder(name, goos))
}

func isWindowsTarget(path string) bool {
	return runtime.GOOS == "windows" && (path == "*" || driveLetter.MatchString(path))
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	return len(entries) == 0, err
}

func PrepareMountPoint(path string) error {
	if isWindowsTarget(path) {
		return nil
	}
	st, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return fmt.Errorf("mount point %s: %w", path, err)
	case !st.IsDir():
		return fmt.Errorf("mount point %s is a file, not a folder", path)
	default:
		empty, err := isEmptyDir(path)
		if err != nil {
			return fmt.Errorf("mount point %s: %w", path, err)
		}
		if !empty {
			return fmt.Errorf("mount point %s is not empty, move or remove its files first", path)
		}
		if runtime.GOOS != "windows" {
			return nil
		}
		// WinFsp creates the folder itself and fails if it exists
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("mount point %s: %w", path, err)
		}
	}
	if runtime.GOOS == "windows" {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	} else {
		err = os.MkdirAll(path, 0o755)
	}
	if err != nil {
		return fmt.Errorf("mount point %s: %w", path, err)
	}
	return nil
}

func CleanupMountPoint(path string) {
	if isWindowsTarget(path) {
		return
	}
	if empty, err := isEmptyDir(path); err != nil || !empty {
		return
	}
	if os.Remove(path) != nil {
		return
	}
	parent := filepath.Dir(path)
	if filepath.Base(parent) != mountRootDir {
		return
	}
	if empty, err := isEmptyDir(parent); err == nil && empty {
		_ = os.Remove(parent)
	}
}
