package store

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "dokky"
	storeKeyUser   = "store-key"
	storeKeyFile   = "store.key"
	deviceKeyUser  = "device-key"
	deviceKeyFile  = "device.key"
	restartKeyUser = "restart-key"
	restartKeyFile = "restart.key"
)

var ErrNotFound = errors.New("the key is not in the keychain")

type Keyring interface {
	Get() ([]byte, error)
	Set([]byte) error
	Delete() error
}

type Fallbacker interface {
	Fallback() bool
}

// SystemKeyring holds the old store key; a v1 store needs it until it moves to the account key
func SystemKeyring(stateDir string) Keyring {
	return systemKeyring(stateDir, storeKeyUser, storeKeyFile)
}

// DeviceKeyring holds this device's key, which opens the vault at startup without a prompt
func DeviceKeyring(stateDir string) Keyring {
	return systemKeyring(stateDir, deviceKeyUser, deviceKeyFile)
}

// RestartKeyring holds the one-use key a restart hands the account key over with; the next start deletes it
func RestartKeyring(stateDir string) Keyring {
	return systemKeyring(stateDir, restartKeyUser, restartKeyFile)
}

// headless linux has no secret service, so the key falls back to a 0600 file in the state dir
func systemKeyring(stateDir, user, file string) Keyring {
	if os.Getenv("DOKKY_KEYRING") == "file" {
		return newKeyring(fileOnly{}, filepath.Join(stateDir, file), true)
	}
	return newKeyring(osKeyring{user: user}, filepath.Join(stateDir, file), runtime.GOOS == "linux")
}

// EnsureKey returns the key in kr, or makes, saves and returns a new one when there is none
func EnsureKey(kr Keyring) ([]byte, error) {
	key, err := kr.Get()
	if errors.Is(err, ErrNotFound) {
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		err = kr.Set(key)
	}
	if err != nil {
		return nil, err
	}
	if len(key) != keySize {
		return nil, errors.New("the key in the keychain has the wrong length")
	}
	return key, nil
}

type osKeyring struct{ user string }

func (k osKeyring) Get() ([]byte, error) {
	v, err := keyring.Get(keyringService, k.user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(v)
}

func (k osKeyring) Set(key []byte) error {
	return keyring.Set(keyringService, k.user, base64.StdEncoding.EncodeToString(key))
}

func (k osKeyring) Delete() error {
	err := keyring.Delete(keyringService, k.user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

type fileOnly struct{}

var errFileOnly = errors.New("DOKKY_KEYRING=file keeps keys in files")

func (fileOnly) Get() ([]byte, error) { return nil, errFileOnly }
func (fileOnly) Set([]byte) error     { return errFileOnly }
func (fileOnly) Delete() error        { return nil }

type fileFallbackKeyring struct {
	primary Keyring
	path    string
	allowed bool
}

func newKeyring(primary Keyring, path string, allowed bool) *fileFallbackKeyring {
	return &fileFallbackKeyring{primary: primary, path: path, allowed: allowed}
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
