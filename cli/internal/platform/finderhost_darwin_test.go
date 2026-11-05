package platform

import (
	"errors"
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

func TestEnsureFinderHostsAsksWhileMissing(t *testing.T) {
	asked := fakeHost(t, nil)

	require.NoError(t, EnsureFinderHosts([]string{"Google Drive"}))
	require.Len(t, *asked, 1)
	require.Contains(t, (*asked)[0], "127.0.0.1 Dokky Google\u00a0Drive Google-Drive # dokky")
	require.Contains(t, (*asked)[0], "with administrator privileges")

	require.NoError(t, EnsureFinderHosts(nil))
	require.Len(t, *asked, 2, "a removed line is asked for again")
}

func TestEnsureFinderHostsSkipsWhenResolved(t *testing.T) {
	asked := fakeHost(t, []string{"127.0.0.1"})
	require.True(t, FinderHostReady())
	require.NoError(t, EnsureFinderHosts([]string{"Jarvis"}))
	require.Empty(t, *asked)
	require.Equal(t, "Jarvis", MountHost("Jarvis"))
}
