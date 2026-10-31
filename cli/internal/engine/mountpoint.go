package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

const (
	maxNameLen   = 40
	mountRootDir = "dokky"
)

var (
	namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	driveLetter = regexp.MustCompile(`^[A-Za-z]:\\?$`)
)

func ValidName(name string) error {
	if len(name) > maxNameLen || !namePattern.MatchString(name) {
		return fmt.Errorf("mount name %q must be lowercase letters, digits and hyphens, at most %d characters", name, maxNameLen)
	}
	return nil
}

func DefaultMountPoint(home, name, goos string) string {
	if goos == "windows" {
		return "*"
	}
	return filepath.Join(home, mountRootDir, name)
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

// realPoint resolves symlinks in the parent only, never stepping into the mount itself
func realPoint(p string) string {
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return p
	}
	return filepath.Join(dir, filepath.Base(p))
}
