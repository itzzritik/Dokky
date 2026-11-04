package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		require.Contains(t, got, w)
	}
}

func TestRenderLaunchAgent(t *testing.T) {
	got := renderLaunchAgent("/Users/a b/.local/bin/dokky")
	requireAll(t, got,
		"<string>me.ritik.dokky</string>",
		"<string>/Users/a b/.local/bin/dokky</string>",
		"<string>daemon</string>",
		"<key>RunAtLoad</key>",
		"<key>SuccessfulExit</key>\n        <false/>",
		"<key>ThrottleInterval</key>\n    <integer>10</integer>",
		"<key>ProcessType</key>\n    <string>Interactive</string>",
		"<key>ExitTimeOut</key>\n    <integer>60</integer>",
		"<key>StandardOutPath</key>\n    <string>/dev/null</string>",
		"<key>StandardErrorPath</key>\n    <string>/dev/null</string>",
	)
}

func TestRenderSystemdUnit(t *testing.T) {
	got := renderSystemdUnit("/home/a/.local/bin/dokky")
	requireAll(t, got,
		`ExecStart="/home/a/.local/bin/dokky" daemon`+"\n",
		"Restart=on-failure\n",
		"RestartSec=5\n",
		"StartLimitIntervalSec=300\n",
		"StartLimitBurst=10\n",
		"TimeoutStopSec=60\n",
		"WantedBy=default.target\n",
	)
	require.Less(t, strings.Index(got, "StartLimitBurst"), strings.Index(got, "[Service]"))
}

func TestRenderSystemdUnitQuotesPath(t *testing.T) {
	got := renderSystemdUnit(`/home/a b/100%/"q"\x/dokky`)
	require.Contains(t, got, `ExecStart="/home/a b/100%%/\"q\"\\x/dokky" daemon`)
}

func TestRenderTaskXML(t *testing.T) {
	got := renderTaskXML(`C:\Users\a&b\.local\bin\dokky.exe`, `PC\a`)
	requireAll(t, got,
		"<LogonTrigger>",
		"<UserId>PC\\a</UserId>",
		"<Hidden>true</Hidden>",
		"<RestartOnFailure>",
		"<Interval>PT1M</Interval>",
		"<Count>3</Count>",
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"conhost.exe",
		`--headless "C:\Users\a&amp;b\.local\bin\dokky.exe" daemon`,
	)
}

func TestServiceNames(t *testing.T) {
	require.Equal(t, "me.ritik.dokky", launchdLabel)
	require.Equal(t, "dokky.service", systemdUnit)
	require.Equal(t, `dokky-ritik`, windowsTaskName(`PC\ritik`))
	require.Equal(t, `dokky-ritik`, windowsTaskName(`ritik`))
}

func TestRefreshServiceFileOnlyWhenStale(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "dokky")
	require.NoError(t, os.WriteFile(bin, nil, 0o755))
	installed, current, reloads := true, false, 0
	oldI, oldC, oldR, oldE := serviceInstalled, serviceCurrent, reloadService, executable
	serviceInstalled = func() bool { return installed }
	serviceCurrent = func(string) bool { return current }
	reloadService = func(string) (bool, error) { reloads++; return true, nil }
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { serviceInstalled, serviceCurrent, reloadService, executable = oldI, oldC, oldR, oldE })

	restart, err := refreshServiceFile(bin)
	require.NoError(t, err)
	require.True(t, restart)
	require.Equal(t, 1, reloads)

	current = true
	restart, _ = refreshServiceFile(bin)
	require.False(t, restart)
	current, installed = false, false
	refreshServiceFile(bin)
	installed = true
	_, _ = refreshServiceFile(filepath.Join(t.TempDir(), "other"))
	require.Equal(t, 1, reloads, "not installed, or not running the installed binary")
}
