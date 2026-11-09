package vault

import (
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	envelopeVersion = 0x01
	nonceSize       = chacha20poly1305.NonceSizeX
	// version byte, nonce and tag; an empty plaintext still has all three
	MinEnvelope = 1 + nonceSize + chacha20poly1305.Overhead
)

// every ciphertext is bound to its role, so a server cannot swap one blob for another
const (
	AADStore = "dokky/v1/store"

	WrapPassword = "password"
	WrapRecovery = "recovery"
	WrapDevice   = "device"
)

func AADWrap(kind, keyID string) string { return "dokky/v1/wrap|" + kind + "|" + keyID }
func AADServiceConfig(id string) string { return "dokky/v1/service|" + id + "|config" }
func AADServiceToken(id string) string  { return "dokky/v1/service|" + id + "|token" }

// one error for a wrong key, a wrong role and changed bytes: telling them apart would help an attacker
var ErrOpen = errors.New("could not decrypt: the key is wrong or the data was changed")

var errBase64 = errors.New("the encrypted data is not valid base64")

// Seal returns base64 of 0x01 ‖ nonce(24) ‖ ciphertext‖tag
func Seal(key, plain []byte, aad string) (string, error) {
	b, err := SealBytes(key, plain, aad)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func Open(key []byte, env, aad string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(env)
	if err != nil {
		return nil, errBase64
	}
	return OpenBytes(key, b, aad)
}

// SealBytes is Seal without base64, for files
func SealBytes(key, plain []byte, aad string) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+nonceSize, 1+nonceSize+len(plain)+aead.Overhead())
	out[0] = envelopeVersion
	copy(out[1:], random(nonceSize))
	return aead.Seal(out, out[1:], plain, []byte(aad)), nil
}

func OpenBytes(key, env []byte, aad string) ([]byte, error) {
	if err := CheckEnvelope(env); err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, env[1:1+nonceSize], env[1+nonceSize:], []byte(aad))
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}

// CheckEnvelope checks the shape only, so a file can be validated without the key
func CheckEnvelope(env []byte) error {
	if len(env) < MinEnvelope {
		return fmt.Errorf("the encrypted data is too short (%d bytes)", len(env))
	}
	if env[0] != envelopeVersion {
		return fmt.Errorf("the encrypted data uses format %d, which this dokky cannot read; update dokky", env[0])
	}
	return nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("the key is %d bytes, it should be %d", len(key), KeySize)
	}
	return chacha20poly1305.NewX(key)
}
