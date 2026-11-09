package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const keyIDPrefix = "dokky/v1/key-id"

// the account key encrypts everything; it only ever leaves memory wrapped
func NewAccountKey() []byte { return random(KeySize) }

// KeyID names an account key without revealing it: first 16 hex chars of SHA-256(prefix ‖ key)
func KeyID(ak []byte) string {
	h := sha256.New()
	h.Write([]byte(keyIDPrefix))
	h.Write(ak)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func validKeyID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func Wrap(kek, ak []byte, kind, keyID string) (string, error) {
	if len(ak) != KeySize {
		return "", fmt.Errorf("the account key is %d bytes, it should be %d", len(ak), KeySize)
	}
	return Seal(kek, ak, AADWrap(kind, keyID))
}

// Unwrap also checks the key matches keyID, so a wrap from another account is never taken for this one
func Unwrap(kek []byte, env, kind, keyID string) ([]byte, error) {
	ak, err := Open(kek, env, AADWrap(kind, keyID))
	if err != nil {
		return nil, err
	}
	if len(ak) != KeySize || KeyID(ak) != keyID {
		Zero(ak)
		return nil, ErrOpen
	}
	return ak, nil
}

// Zero wipes a key or password; go may still hold copies it made, so this is best effort
func Zero(b []byte) { clear(b) }
