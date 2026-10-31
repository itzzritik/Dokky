package daemon

import (
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
