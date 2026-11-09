package vault

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckPasswordOnlyCountsCharacters(t *testing.T) {
	for pw, ok := range map[string]bool{
		"":         false,
		"abcdefg":  false,
		"abcdefgh": true,
		"password": true,
		"11111111": true,
		" spaces ": true,
		"пароль12": true,
		"密码密码密码密":  false,
	} {
		require.Equal(t, ok, CheckPassword(pw), pw)
	}
}
