package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/sysauth"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

const (
	testPassword  = "Velvet-Harbor-Lamp-93"
	otherPassword = "Copper-Meadow-Kite-57"
)

type fixtureVault struct {
	file     *vault.File
	ak, dk   []byte
	recovery string
}

// argon2 runs at its real cost here, so each vault is made once for the whole package
func makeVault(password string) fixtureVault {
	dk := make([]byte, vault.KeySize)
	_, _ = rand.Read(dk)
	f, ak, rec, err := vault.Create([]byte(password), dk, time.Now())
	if err != nil {
		panic(err)
	}
	return fixtureVault{file: f, ak: ak, dk: dk, recovery: rec}
}

var (
	testVault  = sync.OnceValue(func() fixtureVault { return makeVault(testPassword) })
	otherVault = sync.OnceValue(func() fixtureVault { return makeVault(otherPassword) }) // another account's
)

// no system prompt unless a test offers one
func testKeeper(dir string) *keeper {
	k := newKeeper(dir, slog.New(slog.DiscardHandler))
	k.system = func(bool) (string, bool) { return "", false }
	k.prompt = func(context.Context, string) error { return sysauth.ErrUnavailable }
	return k
}

// the shared test vault on disk with its device key, unlocked
func unlockedKeeper(t *testing.T, dir string, linked bool) *keeper {
	t.Helper()
	fx := testVault()
	k := testKeeper(dir)
	f := fx.file.Clone()
	f.Account = linked
	require.NoError(t, f.Save(dir))
	require.NoError(t, k.device.Set(fx.dk))
	k.file, k.ak = f, bytes.Clone(fx.ak)
	return k
}

// reads the store as a fresh start would: with the test vault's key once there is a vault.json
func reopen(t *testing.T, dir string) store.Data {
	t.Helper()
	var st *store.Store
	var err error
	if _, verr := vault.Load(dir); verr == nil {
		st, err = store.OpenWithKey(dir, testVault().ak)
	} else {
		st, err = store.Open(dir, store.SystemKeyring(dir))
	}
	require.NoError(t, err)
	d, err := st.Load()
	require.NoError(t, err)
	return d
}
