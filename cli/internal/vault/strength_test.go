package vault

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckPasswordRejects(t *testing.T) {
	for pw, hint := range map[string]string{
		"":                         "at least 8",
		"Ab1$Ab1":                  "at least 8",
		"kx9mp2vl":                 "longer",
		strings.Repeat("Ab1$", 33): "at most 128",
		" Kx9#mP2$vLq":             "spaces",
		"Kx9#mP2$vLq ":             "spaces",
		"password123":              "common",
		"Password2024!":            "common",
		"P@ssw0rd2024":             "common",
		"qwertyuiop":               "common",
		"1234567890":               "common",
		"Summer2026!!":             "common",
		"iloveyou123456":           "common",
		"123password":              "common",
		"abcdefghijkl":             "16 or more",
		"KXQMPZVLRTWB":             "16 or more",
		"482917365019":             "16 or more",
		"bcdefghi12":               "runs",
		"abc12345xy":               "runs",
		"aaaaaaaaaaaaaaaaaaaa":     "runs",
		"abababababababababab":     "runs",
		"1234567890123456":         "runs",
	} {
		score, ok, got := CheckPassword(pw)
		require.False(t, ok, pw)
		require.Contains(t, got, hint, pw)
		require.LessOrEqual(t, score, 1, pw)
	}
}

func TestCheckPasswordAccepts(t *testing.T) {
	for pw, want := range map[string]int{
		"kx9mp2vlqz":                   2,
		"Kx9mP2vLqz":                   2,
		"Kx9#mP2$vL":                   3,
		"Kx9#mP2$":                     2,
		"Kx9mP2vL":                     2,
		"Tr0ub4dor&3":                  3,
		"correct horse battery staple": 4,
		"mydogisnamedrexx":             3,
		"मेरा पासवर्ड बहुत लंबा है": 4,
		"G7#kq!Vz2@pLw9$e": 4,
	} {
		score, ok, hint := CheckPassword(pw)
		require.True(t, ok, "%s: %s", pw, hint)
		require.Equal(t, want, score, pw)
		require.NotEmpty(t, hint)
	}
}

func TestCommonListLoads(t *testing.T) {
	require.Greater(t, len(common()), 300)
	for w := range common() {
		require.Equal(t, strings.ToLower(w), w)
	}
}
