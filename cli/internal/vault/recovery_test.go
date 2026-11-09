package vault

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var recoveryShape = regexp.MustCompile(`^DK1(-[0-9A-HJKMNP-TV-Z]{4}){8}-[0-9A-HJKMNP-TV-Z]{2}$`)

func fixedRaw() []byte {
	raw := make([]byte, RecoverySize)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	return raw
}

const fixedText = "DK1-0410-6105-0R3G-G28A-1C60-T3GF-208H-44RM-W4"

func TestRecoveryFormat(t *testing.T) {
	require.Equal(t, fixedText, formatRecovery(fixedRaw()))
	for range 50 {
		raw, text := NewRecoveryKey()
		require.Len(t, raw, 20)
		require.Regexp(t, recoveryShape, text)
		got, err := ParseRecoveryKey(text)
		require.NoError(t, err)
		require.Equal(t, raw, got)
	}
}

func TestRecoveryParseIsLenient(t *testing.T) {
	for _, in := range []string{
		fixedText,
		strings.ToLower(fixedText),
		strings.ReplaceAll(fixedText, "-", ""),
		strings.ReplaceAll(fixedText, "-", " "),
		"  " + fixedText + "\n",
		strings.TrimPrefix(fixedText, "DK1-"),
		strings.ReplaceAll(strings.TrimPrefix(fixedText, "DK1-"), "-", ""),
		strings.NewReplacer("0", "O").Replace(strings.TrimPrefix(fixedText, "DK1-")),
		strings.NewReplacer("1", "l").Replace(strings.TrimPrefix(fixedText, "DK1-")),
		"DK1-" + strings.NewReplacer("1", "I").Replace(strings.TrimPrefix(fixedText, "DK1-")),
		"dk1 0410 6105 0r3g g28a 1c60 t3gf 208h 44rm w4",
	} {
		got, err := ParseRecoveryKey(in)
		require.NoError(t, err, in)
		require.Equal(t, fixedRaw(), got, in)
	}
}

func TestRecoveryParseCatchesTypos(t *testing.T) {
	body := strings.ReplaceAll(strings.TrimPrefix(fixedText, "DK1-"), "-", "")
	typos := 0
	for i := range body {
		for _, c := range crockfordAlphabet {
			if byte(c) == body[i] {
				continue
			}
			bad := body[:i] + string(c) + body[i+1:]
			if _, err := ParseRecoveryKey(bad); err != nil {
				require.ErrorIs(t, err, ErrBadRecoveryKey)
				typos++
			}
		}
	}
	// one checksum byte lets about 1 in 256 typos through
	require.Greater(t, typos, 34*31*250/256)

	_, err := ParseRecoveryKey("DK1-0410-6105-0R3G-G28A-1C60-T3GF-208H-44RM-W5")
	require.ErrorContains(t, err, "typo")
	_, err = ParseRecoveryKey("DK1-0510-6105-0R3G-G28A-1C60-T3GF-208H-44RM-W4")
	require.ErrorIs(t, err, ErrBadRecoveryKey)
}

func TestRecoveryParseRejectsBadInput(t *testing.T) {
	for in, want := range map[string]string{
		"":               "this has 0",
		"DK1-0410":       "this has 4",
		fixedText + "-X": "this has 35",
		strings.Replace(fixedText, "0410", "U410", 1):                              `'U' cannot appear`,
		strings.Replace(fixedText, "0410", "*410", 1):                              `'*' cannot appear`,
		"DK2" + strings.ReplaceAll(strings.TrimPrefix(fixedText, "DK1-"), "-", ""): "this has 37",
	} {
		_, err := ParseRecoveryKey(in)
		require.ErrorIs(t, err, ErrBadRecoveryKey, in)
		require.ErrorContains(t, err, want, in)
	}
}
