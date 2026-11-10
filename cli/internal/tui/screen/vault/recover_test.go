package vault

import (
	"strings"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestRecoveryKeyReadsAnyWay(t *testing.T) {
	require.Equal(t, "DK1-ABCD-EFGH", groupKey([]rune("dk1 abcd-efgh")))
	require.Equal(t, "0011-ABC", groupKey([]rune("o0il abc")))
	require.Empty(t, groupKey(nil))

	raw, text := vault.NewRecoveryKey()
	clear(raw)
	bare := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(text, "DK1-"), "-", " "))
	for name, c := range map[string]struct {
		in   string
		want keyState
		n    int
	}{
		"empty":       {"", keyEmpty, 0},
		"started":     {"DK1-ABCD-EF", keyShort, 6},
		"as shown":    {text, keyGood, keyChars},
		"loose":       {bare, keyGood, keyChars},
		"one more":    {text + "Q", keyLong, keyChars + 1},
		"typo":        {flip(text), keyTypo, keyChars},
		"with spaces": {"  " + text + "  ", keyGood, keyChars},
	} {
		s, n := checkKey([]rune(c.in))
		require.Equal(t, c.want, s, name)
		require.Equal(t, c.n, n, name)
	}
}

// the last character swapped for another one, so its check fails
func flip(text string) string {
	last := text[len(text)-1]
	next := byte('0')
	if last == '0' {
		next = '2'
	}
	return text[:len(text)-1] + string(next)
}

func TestRecoverChecksKeyFirst(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	f := newFlow(st, newRecover(false), false, exitQuit)
	r := f.top().(*recoverForm)
	key(f, st, "enter")
	require.Equal(t, "Type your recovery key.", r.err)

	typed(f, st, "DK1-ABCD")
	key(f, st, "down")
	key(f, st, "enter")
	require.Equal(t, "The recovery key is too short.", r.err)
	require.Equal(t, "field:0", r.at)

	raw, text := vault.NewRecoveryKey()
	clear(raw)
	for range len("DK1-ABCD") {
		key(f, st, "backspace")
	}
	typed(f, st, text)
	require.Equal(t, keyGood, r.ks)
	key(f, st, "enter")
	require.Equal(t, "field:1", r.at, "enter moves on from a good key")
	typed(f, st, goodPassword)
	key(f, st, "enter")
	typed(f, st, goodPassword)
	key(f, st, "enter")
	require.NotZero(t, r.req)
	require.True(t, f.Holding())

	f.Update(core.Reply{Req: r.req, Err: coded(api.ErrBadRecoveryKey, "no")}, st)
	require.Equal(t, "That recovery key is not right.", r.err)
	require.Equal(t, "field:0", r.at)
}
