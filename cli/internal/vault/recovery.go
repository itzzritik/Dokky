package vault

import (
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

const (
	RecoverySize   = 20
	recoveryPrefix = "DK1"
	recoveryChars  = 32 // 20 bytes in base32
	checksumChars  = 2  // one byte in base32
	groupSize      = 4
)

// crockford's alphabet has no I, L, O or U, so a key read aloud or off paper is hard to mistype
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var crockford = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)

var ErrBadRecoveryKey = errors.New("that recovery key is not right")

// NewRecoveryKey returns the raw key and its text, shown once: DK1-XXXX-...-XX
func NewRecoveryKey() (raw []byte, text string) {
	raw = random(RecoverySize)
	return raw, formatRecovery(raw)
}

func formatRecovery(raw []byte) string {
	body := crockford.EncodeToString(raw) + checksum(raw)
	var b strings.Builder
	b.WriteString(recoveryPrefix)
	for i := 0; i < len(body); i += groupSize {
		b.WriteByte('-')
		b.WriteString(body[i:min(i+groupSize, len(body))])
	}
	return b.String()
}

func checksum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return crockford.EncodeToString(sum[:1])
}

// ParseRecoveryKey ignores case, spaces and dashes, reads O as 0 and I or L as 1, and the DK1 prefix is optional
func ParseRecoveryKey(text string) ([]byte, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(text) {
		switch r {
		case ' ', '\t', '\n', '\r', '-', '_':
		case 'O':
			b.WriteByte('0')
		case 'I', 'L':
			b.WriteByte('1')
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	const want = recoveryChars + checksumChars
	// a 34 character key may itself start with DK1, so the prefix only goes when there is more than a key
	if len(s) != want && strings.HasPrefix(s, recoveryPrefix) {
		s = s[len(recoveryPrefix):]
	}
	if len(s) != want {
		return nil, fmt.Errorf("%w: it should have %d letters and numbers after DK1-, this has %d", ErrBadRecoveryKey, want, len(s))
	}
	for _, r := range s {
		if !strings.ContainsRune(crockfordAlphabet, r) {
			return nil, fmt.Errorf("%w: %q cannot appear in a recovery key", ErrBadRecoveryKey, r)
		}
	}
	raw, err := crockford.DecodeString(s[:recoveryChars])
	if err != nil || len(raw) != RecoverySize {
		return nil, fmt.Errorf("%w: it could not be read", ErrBadRecoveryKey)
	}
	if checksum(raw) != s[recoveryChars:] {
		Zero(raw)
		return nil, fmt.Errorf("%w: it has a typo, check each character", ErrBadRecoveryKey)
	}
	return raw, nil
}
