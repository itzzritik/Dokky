package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

type emptyKeyring struct{}

func (emptyKeyring) Get() ([]byte, error) { return nil, store.ErrNotFound }
func (emptyKeyring) Set([]byte) error     { return nil }
func (emptyKeyring) Delete() error        { return nil }

func TestKeyringStateDetectsMissingKey(t *testing.T) {
	state := t.TempDir()
	_, err := keyringState(emptyKeyring{}, state, false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(state, "store.enc"), []byte("x"), 0o600))
	_, err = keyringState(emptyKeyring{}, state, false)
	require.ErrorIs(t, err, store.ErrKeyMissing)

	require.NoError(t, os.WriteFile(filepath.Join(state, "store.enc"), []byte("DKY2 locked with the master password"), 0o600))
	_, err = keyringState(emptyKeyring{}, state, false)
	require.NoError(t, err, "a v2 store does not need a keychain key")

	_, err = keyringState(emptyKeyring{}, state, true)
	require.ErrorIs(t, err, store.ErrNotFound, "a vault without its device key waits for the password at startup")
}

func TestDoctorLeavesTheMachineAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	root := newRootCmd()
	doctor, _, err := root.Find([]string{"doctor"})
	require.NoError(t, err)
	require.NoError(t, root.PersistentPreRunE(doctor, nil))
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries)
}
