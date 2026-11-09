package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const (
	fileName = "store.enc"
	prevName = "store.enc.prev"
	keySize  = 32
)

// v2 is "DKY2" ‖ envelope(account key, json, dokky/v1/store); a file without it is v1, AES-GCM with the keychain store key
var magicV2 = []byte("DKY2")

var (
	ErrKeyMissing  = errors.New("the store key is missing from the keychain, so the existing store cannot be read")
	ErrNeedsKey    = errors.New("the store is locked with the master password, unlock dokky first")
	ErrOldFormat   = errors.New("the store is still in the old format and needs the old keychain key to move to the master password")
	errWrongAccKey = errors.New("the account key does not open the store")
)

type Store struct {
	path string
	kr   Keyring     // the old store key, only needed for a v1 file
	aead cipher.AEAD // v1
	ak   []byte      // the account key, v2
	v2   bool        // once v2, never written as v1 again
}

// Open is OpenLegacy under its old name, for callers that predate the vault
func Open(stateDir string, kr Keyring) (*Store, error) { return OpenLegacy(stateDir, kr) }

// OpenLegacy opens a v1 store with the keychain store key, making the key for a fresh install.
// a v2 file is left alone: it needs Upgrade with the account key before it can be read
func OpenLegacy(stateDir string, kr Keyring) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(stateDir, fileName), kr: kr}
	v, err := formatOf(s.path)
	if err != nil {
		return nil, err
	}
	if v == 2 {
		s.v2 = true
		return s, nil
	}
	if err := s.unlock(); err != nil {
		return nil, err
	}
	return s, nil
}

// OpenWithKey opens a v2 store with the account key. it keeps its own copy of ak; Close wipes it
func OpenWithKey(stateDir string, ak []byte) (*Store, error) {
	if len(ak) != keySize {
		return nil, errors.New("the account key has the wrong length")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(stateDir, fileName), ak: bytes.Clone(ak), v2: true}
	if err := s.settle(); err != nil {
		return nil, err
	}
	return s, nil
}

// FormatOf says which store is on disk: 0 none, 1 the keychain store key, 2 the account key
func FormatOf(stateDir string) (int, error) {
	return formatOf(filepath.Join(stateDir, fileName))
}

// CheckV2 checks a v2 store's shape without the account key, so a new binary can be tested before it takes over
func CheckV2(stateDir string) error {
	raw, err := os.ReadFile(filepath.Join(stateDir, fileName))
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(raw, magicV2) {
		return errors.New("store.enc is not a v2 store")
	}
	if err := vault.CheckEnvelope(raw[len(magicV2):]); err != nil {
		return fmt.Errorf("store.enc: %w", err)
	}
	return nil
}

func formatOf(path string) (int, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	head := make([]byte, len(magicV2))
	n, _ := io.ReadFull(f, head)
	if bytes.Equal(head[:n], magicV2) {
		return 2, nil
	}
	return 1, nil
}

func (s *Store) unlock() error {
	if s.kr == nil {
		return ErrOldFormat
	}
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

// Load reads whichever format is on disk, as long as the store has its key
func (s *Store) Load() (Data, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Fresh(), nil
	}
	if err != nil {
		return Data{}, err
	}
	var plain []byte
	if bytes.HasPrefix(raw, magicV2) {
		plain, err = s.openV2(raw)
	} else {
		plain, err = s.openV1(raw)
	}
	if err != nil {
		return Data{}, err
	}
	defer vault.Zero(plain)
	d := Fresh()
	if err := json.Unmarshal(plain, &d); err != nil {
		return Data{}, err
	}
	if d.Local == nil {
		d.Local = map[string]LocalMount{}
	}
	return d, nil
}

func (s *Store) openV2(raw []byte) ([]byte, error) {
	if s.ak == nil {
		return nil, ErrNeedsKey
	}
	plain, err := vault.OpenBytes(s.ak, raw[len(magicV2):], vault.AADStore)
	if errors.Is(err, vault.ErrOpen) {
		return nil, fmt.Errorf("decrypt store: %w", errWrongAccKey)
	}
	if err != nil {
		return nil, fmt.Errorf("decrypt store: %w", err)
	}
	return plain, nil
}

func (s *Store) openV1(raw []byte) ([]byte, error) {
	if s.aead == nil {
		if err := s.unlock(); err != nil {
			return nil, err
		}
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return nil, errors.New("store file is truncated")
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt store: %w", err)
	}
	return plain, nil
}

// Save writes the store's own format: v2 once it has the account key, v1 before
func (s *Store) Save(d Data) error {
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	defer vault.Zero(plain)
	var out []byte
	if s.v2 {
		if s.ak == nil {
			return ErrNeedsKey
		}
		env, err := vault.SealBytes(s.ak, plain, vault.AADStore)
		if err != nil {
			return err
		}
		out = append(bytes.Clone(magicV2), env...)
	} else {
		if s.aead == nil {
			if err := s.unlock(); err != nil {
				return err
			}
		}
		nonce := make([]byte, s.aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		out = s.aead.Seal(nonce, nonce, plain, nil)
	}
	return platform.WriteFileAtomic(s.path, out)
}

// Upgrade moves the store to the account key: the v2 file is fully written before the keychain store key is
// deleted, so a crash leaves either the old store with its key or the new one, and a v2 file always wins.
// vault.json must be saved first, or a crash here would leave a store nothing can open.
// it also works on a store that is already v2, which finishes an upgrade that died before deleting the key
func (s *Store) Upgrade(ak []byte) error {
	if len(ak) != keySize {
		return errors.New("the account key has the wrong length")
	}
	oldAK, oldV2 := s.ak, s.v2
	s.ak = bytes.Clone(ak)
	d, err := s.Load()
	if err == nil {
		s.v2 = true
		err = s.Save(d)
	}
	if err != nil {
		vault.Zero(s.ak)
		s.ak, s.v2 = oldAK, oldV2
		return err
	}
	vault.Zero(oldAK)
	s.aead = nil
	if s.kr != nil {
		if err := s.kr.Delete(); err != nil {
			return fmt.Errorf("the store now uses the master password, but the old keychain key could not be removed: %w", err)
		}
	}
	return nil
}

// Rekey re-encrypts the store with another account key, for joining an account. the old file is kept beside
// the new one until OpenWithKey next runs, so a crash before vault.json switches keys loses nothing
func (s *Store) Rekey(ak []byte) error {
	if !s.v2 {
		return s.Upgrade(ak)
	}
	if len(ak) != keySize {
		return errors.New("the account key has the wrong length")
	}
	d, err := s.Load()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := platform.WriteFileAtomic(filepath.Join(filepath.Dir(s.path), prevName), raw); err != nil {
			return err
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	oldAK := s.ak
	s.ak = bytes.Clone(ak)
	if err := s.Save(d); err != nil {
		vault.Zero(s.ak)
		s.ak = oldAK
		return err
	}
	vault.Zero(oldAK)
	return nil
}

// settle finishes an interrupted Rekey: whichever of the two files the account key opens stays
func (s *Store) settle() error {
	prev := filepath.Join(filepath.Dir(s.path), prevName)
	old, err := os.ReadFile(prev)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(s.path); err == nil {
		if _, err := s.Load(); err == nil {
			return os.Remove(prev)
		}
	}
	if !bytes.HasPrefix(old, magicV2) {
		return nil
	}
	plain, err := s.openV2(old)
	if err != nil {
		return nil
	}
	vault.Zero(plain)
	return os.Rename(prev, s.path)
}

// Delete removes the store and the old keychain key. a v1 store starts a new key on the next Save, like a
// fresh install; a v2 store keeps its account key
func (s *Store) Delete() error {
	for _, name := range []string{s.path, filepath.Join(filepath.Dir(s.path), prevName)} {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	s.aead = nil
	if s.kr == nil {
		return nil
	}
	return s.kr.Delete()
}

// Close wipes the account key; a closed v2 store cannot load or save
func (s *Store) Close() {
	vault.Zero(s.ak)
	s.ak = nil
	s.aead = nil
}
