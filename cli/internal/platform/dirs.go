package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func StateDir() (string, error) {
	if runtime.GOOS == "windows" {
		return windowsBase()
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "dokky"), nil
}

func CacheDir() (string, error) {
	if runtime.GOOS == "windows" {
		b, err := windowsBase()
		if err != nil {
			return "", err
		}
		return filepath.Join(b, "cache"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "dokky"), nil
}

func windowsBase() (string, error) {
	d := os.Getenv("LocalAppData")
	if d == "" {
		return "", errors.New("%LocalAppData% is not set")
	}
	return filepath.Join(d, "dokky"), nil
}
