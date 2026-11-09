package daemon

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

// A restart out of the unlocked phase (an update, daemon.restart, a reinstall) hands the account key to the next
// start, so a device set to unlock with system auth or the password does not drop every drive until someone
// unlocks it. The trade-off: between the restart and the next start, normally seconds, the file plus the key in
// the keychain open the vault, like the device key does in auto mode. The next start deletes both before
// anything else and honours them only for two minutes; a lock, a stop or a pending removal never writes one.
const (
	handoffFile   = "handoff.json"
	handoffMaxAge = 2 * time.Minute
	wrapHandoff   = "handoff"
	infoHandoff   = "dokky/v1/handoff-kek"
)

type handoffNote struct {
	At      time.Time `json:"at"`
	KeyID   string    `json:"keyId"`
	Wrapped string    `json:"wrapped"` // the account key sealed with a key derived from restart-key
}

func handoffKEK(key []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, key, nil, infoHandoff, vault.KeySize)
}

// handOff writes handoff.json for the next start. auto mode opens with the device key anyway, so it gets none
func (k *keeper) handOff() error {
	k.mu.Lock()
	f, ak := k.file, k.ak
	var keyID, unlock string
	if f != nil {
		keyID, unlock = f.KeyID, f.Unlock
	}
	k.mu.Unlock()
	if f == nil || ak == nil || unlock == vault.UnlockAuto {
		return nil
	}
	key := make([]byte, vault.KeySize)
	_, _ = rand.Read(key)
	defer vault.Zero(key)
	kek, err := handoffKEK(key)
	if err != nil {
		return err
	}
	defer vault.Zero(kek)
	wrapped, err := vault.Wrap(kek, ak, wrapHandoff, keyID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(handoffNote{At: k.now().UTC(), KeyID: keyID, Wrapped: wrapped})
	if err != nil {
		return err
	}
	if err := k.restart.Set(key); err != nil {
		return err
	}
	if err := platform.WriteFileAtomic(filepath.Join(k.dir, handoffFile), raw); err != nil {
		_ = k.restart.Delete()
		return err
	}
	k.log.Info("handed the key to the next start")
	return nil
}

// takeHandoff returns the account key a restart left, or nil. both halves go at once, used or not
func (k *keeper) takeHandoff() []byte {
	path := filepath.Join(k.dir, handoffFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	key, kerr := k.restart.Get()
	defer vault.Zero(key)
	k.dropHandoff()
	var n handoffNote
	if err != nil || kerr != nil || json.Unmarshal(raw, &n) != nil {
		k.log.Warn("a restart's key handover could not be read", "err", errors.Join(err, kerr))
		return nil
	}
	f := k.current()
	age := k.now().Sub(n.At)
	if f == nil || n.KeyID != f.KeyID || age < 0 || age > handoffMaxAge {
		k.log.Warn("ignored a restart's key handover that is too old or for another vault", "age", age.Round(time.Second))
		return nil
	}
	kek, err := handoffKEK(key)
	if err != nil {
		return nil
	}
	defer vault.Zero(kek)
	ak, err := vault.Unwrap(kek, n.Wrapped, wrapHandoff, f.KeyID)
	if err != nil {
		k.log.Warn("a restart's key handover did not open", "err", err)
		return nil
	}
	k.log.Info("opened with the key the restart handed over")
	return ak
}

func (k *keeper) dropHandoff() {
	if err := os.Remove(filepath.Join(k.dir, handoffFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		k.log.Warn("remove the key handover", "err", err)
	}
	if err := k.restart.Delete(); err != nil {
		k.log.Warn("remove the restart key", "err", err)
	}
}
