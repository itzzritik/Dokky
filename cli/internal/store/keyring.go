package store

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "dokky"
	keyringUser    = "store-key"
	keyFileName    = "store.key"
)

var ErrNotFound = errors.New("store key not found")

type Keyring interface {
	Get() ([]byte, error)
	Set([]byte) error
	Delete() error
}

type Fallbacker interface {
	Fallback() bool
}

type osKeyring struct{}

func (osKeyring) Get() ([]byte, error) {
	v, err := keyring.Get(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(v)
}

func (osKeyring) Set(k []byte) error {
	return keyring.Set(keyringService, keyringUser, base64.StdEncoding.EncodeToString(k))
}

func (osKeyring) Delete() error {
	err := keyring.Delete(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

type fileFallbackKeyring struct {
	primary Keyring
	path    string
	allowed bool
}

func newKeyring(primary Keyring, stateDir string, allowed bool) *fileFallbackKeyring {
	return &fileFallbackKeyring{primary: primary, path: filepath.Join(stateDir, keyFileName), allowed: allowed}
}

type fileOnly struct{}

var errFileOnly = errors.New("DOKKY_KEYRING=file keeps the store key in a file")

func (fileOnly) Get() ([]byte, error) { return nil, errFileOnly }
func (fileOnly) Set([]byte) error     { return errFileOnly }
func (fileOnly) Delete() error        { return nil }

func SystemKeyring(stateDir string) Keyring {
	if os.Getenv("DOKKY_KEYRING") == "file" {
		return newKeyring(fileOnly{}, stateDir, true)
	}
	return newKeyring(osKeyring{}, stateDir, runtime.GOOS == "linux")
}

func (k *fileFallbackKeyring) Fallback() bool {
	_, err := os.Stat(k.path)
	return err == nil
}

func (k *fileFallbackKeyring) Get() ([]byte, error) {
	if k.Fallback() {
		return os.ReadFile(k.path)
	}
	key, err := k.primary.Get()
	if err != nil && !errors.Is(err, ErrNotFound) && k.allowed {
		return nil, ErrNotFound
	}
	return key, err
}

func (k *fileFallbackKeyring) Set(key []byte) error {
	if !k.Fallback() {
		err := k.primary.Set(key)
		if err == nil || !k.allowed {
			return err
		}
	}
	return os.WriteFile(k.path, key, 0o600)
}

func (k *fileFallbackKeyring) Delete() error {
	if err := os.Remove(k.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := k.primary.Delete(); err != nil && !k.allowed {
		return err
	}
	return nil
}
