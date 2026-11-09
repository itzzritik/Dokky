package vault

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

const (
	KDFArgon2id = "argon2id"
	SaltSize    = 32
	KeySize     = 32

	infoPasswordKEK = "dokky/v1/password-kek"
	infoRecoveryKEK = "dokky/v1/recovery-kek"
	infoDeviceKEK   = "dokky/v1/device-kek"
)

// these are part of the format; tests swap them for cheap values so they stay fast
var cost = kdfCost{t: 3, m: 64 * 1024, p: 4, minM: 19456}

type kdfCost struct {
	t, m, minM uint32
	p          uint8
}

// caps hold for params read from disk or the server, so a hostile server cannot make a device burn memory or hang
const (
	maxT uint32 = 10
	maxM uint32 = 256 * 1024
	maxP uint8  = 16
)

// KDF is how the master password is stretched; Salt is base64 in JSON like every []byte
type KDF struct {
	Alg  string `json:"alg"`
	Salt []byte `json:"salt"`
	T    uint32 `json:"t"`
	M    uint32 `json:"m"`
	P    uint8  `json:"p"`
}

func NewKDF() KDF {
	return KDF{Alg: KDFArgon2id, Salt: random(SaltSize), T: cost.t, M: cost.m, P: cost.p}
}

func (k KDF) Validate() error {
	switch {
	case k.Alg != KDFArgon2id:
		return fmt.Errorf("the password settings use %q, but only %s is supported", k.Alg, KDFArgon2id)
	case len(k.Salt) != SaltSize:
		return fmt.Errorf("the password salt is %d bytes, it should be %d", len(k.Salt), SaltSize)
	case k.T < 1 || k.T > maxT:
		return fmt.Errorf("the password time cost %d is outside 1 to %d", k.T, maxT)
	case k.M < cost.minM || k.M > maxM:
		return fmt.Errorf("the password memory cost %d KiB is outside %d to %d KiB", k.M, cost.minM, maxM)
	case k.P < 1 || k.P > maxP:
		return fmt.Errorf("the password parallelism %d is outside 1 to %d", k.P, maxP)
	}
	return nil
}

// the same password typed on a Mac and on Windows can arrive in different unicode forms; NFKC makes them one
func PasswordKEK(password []byte, k KDF) ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	if len(password) == 0 {
		return nil, errors.New("the password is empty")
	}
	pw := norm.NFKC.Bytes(password)
	stretched := argon2.IDKey(pw, k.Salt, k.T, k.M, k.P, KeySize)
	defer Zero(stretched)
	if len(pw) > 0 && &pw[0] != &password[0] {
		Zero(pw)
	}
	return derive(stretched, nil, infoPasswordKEK)
}

func RecoveryKEK(recovery, salt []byte) ([]byte, error) {
	if len(recovery) != RecoverySize {
		return nil, fmt.Errorf("the recovery key is %d bytes, it should be %d", len(recovery), RecoverySize)
	}
	if len(salt) != SaltSize {
		return nil, fmt.Errorf("the password salt is %d bytes, it should be %d", len(salt), SaltSize)
	}
	return derive(recovery, salt, infoRecoveryKEK)
}

func DeviceKEK(deviceKey []byte) ([]byte, error) {
	if len(deviceKey) != KeySize {
		return nil, fmt.Errorf("the device key is %d bytes, it should be %d", len(deviceKey), KeySize)
	}
	return derive(deviceKey, nil, infoDeviceKEK)
}

func derive(ikm, salt []byte, info string) ([]byte, error) {
	return hkdf.Key(sha256.New, ikm, salt, info, KeySize)
}

// crypto/rand never fails since go 1.24: it crashes the program instead of returning weak bytes
func random(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
