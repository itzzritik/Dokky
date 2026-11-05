package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	fileName = "store.enc"
	keySize  = 32
)

var ErrKeyMissing = errors.New("the store key is missing from the keychain, so the existing store cannot be read")

type Store struct {
	path string
	kr   Keyring
	aead cipher.AEAD
}

func Open(stateDir string, kr Keyring) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(stateDir, fileName), kr: kr}
	if err := s.unlock(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) unlock() error {
	key, err := s.kr.Get()
	if errors.Is(err, ErrNotFound) {
		if _, statErr := os.Stat(s.path); statErr == nil {
			return fmt.Errorf("%w: cannot read %s", ErrKeyMissing, s.path)
		}
		key = make([]byte, keySize)
		if _, err = rand.Read(key); err != nil {
			return err
		}
		err = s.kr.Set(key)
	}
	if err != nil {
		return fmt.Errorf("store key: %w", err)
	}
	if len(key) != keySize {
		return errors.New("store key has the wrong length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.aead, err = cipher.NewGCM(block)
	return err
}

func (s *Store) Load() (Data, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Fresh(), nil
	}
	if err != nil {
		return Data{}, err
	}
	if s.aead == nil {
		if err := s.unlock(); err != nil {
			return Data{}, err
		}
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return Data{}, errors.New("store file is truncated")
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return Data{}, fmt.Errorf("decrypt store: %w", err)
	}
	d := Fresh()
	if err := json.Unmarshal(plain, &d); err != nil {
		return Data{}, err
	}
	if d.Local == nil {
		d.Local = map[string]LocalMount{}
	}
	return d, nil
}

func (s *Store) Save(d Data) error {
	if s.aead == nil {
		if err := s.unlock(); err != nil {
			return err
		}
	}
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	out := s.aead.Seal(nonce, nonce, plain, nil)

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, fileName+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	return syncDir(dir)
}

// a Save after Delete starts a new key, like a fresh install
func (s *Store) Delete() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.aead = nil
	return s.kr.Delete()
}

func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
