package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func fakeHost(t *testing.T, addrs []string) *[]string {
	t.Helper()
	var asked []string
	oldLookup, oldAsk := lookupHost, askAdmin
	lookupHost = func(string) ([]string, error) {
		if addrs == nil {
			return nil, errors.New("no such host")
		}
		return addrs, nil
	}
	askAdmin = func(script string) error { asked = append(asked, script); return nil }
	t.Cleanup(func() { lookupHost, askAdmin = oldLookup, oldAsk })
	return &asked
}

func TestEnsureFinderHostAsksWhileMissing(t *testing.T) {
	asked := fakeHost(t, nil)

	require.NoError(t, EnsureFinderHost())
	require.Len(t, *asked, 1)
	require.Contains(t, (*asked)[0], "127.0.0.1 Dokky # dokky")
	require.Contains(t, (*asked)[0], "with administrator privileges")

	require.NoError(t, EnsureFinderHost())
	require.Len(t, *asked, 2, "a removed line is asked for again")
}

func TestEnsureFinderHostSkipsWhenClean(t *testing.T) {
	asked := fakeHost(t, []string{"127.0.0.1"})
	hosts := filepath.Join(t.TempDir(), "hosts")
	old := hostsFile
	hostsFile = hosts
	t.Cleanup(func() { hostsFile = old })

	require.NoError(t, os.WriteFile(hosts, []byte("127.0.0.1 localhost\n127.0.0.1 Dokky # dokky\n"), 0o644))
	require.NoError(t, EnsureFinderHost())
	require.Empty(t, *asked)

	require.NoError(t, os.WriteFile(hosts, []byte("127.0.0.1 Dokky Jarvis Google-Drive # dokky\n"), 0o644))
	require.NoError(t, EnsureFinderHost())
	require.Len(t, *asked, 1, "old drive names are cleaned out")
}
