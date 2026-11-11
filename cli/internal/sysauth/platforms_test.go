package sysauth

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

const hostile = `"); do shell script "rm -rf ~"; $(rm -rf ~) '`

func TestMacCommand(t *testing.T) {
	c := macCommand("prompt", hostile)
	require.Equal(t, "/usr/bin/osascript", c.path)
	require.Equal(t, []string{"-l", "JavaScript", "-e", macScript, "prompt", hostile}, c.args)
	require.NotContains(t, macScript, "rm -rf", "the reason is an argument, never part of the script")
	require.Empty(t, c.env)

	check := macCommand("check", "ignored")
	require.Equal(t, []string{"-l", "JavaScript", "-e", macScript, "check"}, check.args)
}

func TestMacScript(t *testing.T) {
	for _, want := range []string{
		"function run(argv)",
		"ObjC.import('LocalAuthentication')",
		"$.LAContext.alloc.init",
		"canEvaluatePolicyError(1, null)",
		"canEvaluatePolicyError(2, err)",
		"evaluatePolicyLocalizedReasonReply(2, argv[1],",
		"runModeBeforeDate($.NSDefaultRunLoopMode",
		"code === -2 || code === -4 || code === -9",
	} {
		require.Contains(t, macScript, want)
	}
}

func TestParseMacCheck(t *testing.T) {
	for out, want := range map[string]string{"touchid": "Touch ID", "password": "your Mac password", "none": "", "": "", "junk": ""} {
		name, ok := parseMacCheck(out)
		require.Equal(t, want, name, out)
		require.Equal(t, want != "", ok, out)
	}
}

func TestMacAvailable(t *testing.T) {
	calls := fakeRun(t, result{out: "touchid"}, nil)
	name, ok := macAvailable(context.Background())
	require.True(t, ok)
	require.Equal(t, "Touch ID", name)
	require.Equal(t, "check", (*calls)[0].args[4])

	fakeRun(t, result{out: "touchid", code: 1}, nil)
	_, ok = macAvailable(context.Background())
	require.False(t, ok, "a failed script is not a yes")
	fakeRun(t, result{code: -1}, os.ErrNotExist)
	_, ok = macAvailable(context.Background())
	require.False(t, ok)
}

func TestParseMacPrompt(t *testing.T) {
	require.NoError(t, parseMacPrompt(result{out: "ok"}, nil))
	require.ErrorIs(t, parseMacPrompt(result{out: "cancel"}, nil), ErrCancelled)

	err := parseMacPrompt(result{out: "unavailable:Biometry is not enrolled. (-7)"}, nil)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorContains(t, err, "not enrolled")

	err = parseMacPrompt(result{out: "error:Application retry limit exceeded. (-1)"}, nil)
	require.ErrorContains(t, err, "retry limit")
	require.NotErrorIs(t, err, ErrCancelled)
	require.NotErrorIs(t, err, ErrUnavailable)

	err = parseMacPrompt(result{stderr: "execution error: Error: ReferenceError: Can't find variable: $\nmore", code: 1}, nil)
	require.ErrorContains(t, err, "ReferenceError")
	require.NotContains(t, err.Error(), "more")

	require.ErrorContains(t, parseMacPrompt(result{out: "maybe"}, nil), "does not know")
	require.ErrorIs(t, parseMacPrompt(result{code: -1}, context.Canceled), ErrCancelled)
	require.ErrorIs(t, parseMacPrompt(result{code: -1}, context.DeadlineExceeded), ErrCancelled)
	require.ErrorIs(t, parseMacPrompt(result{code: -1}, os.ErrNotExist), ErrUnavailable)
}

func TestMacPromptPassesReason(t *testing.T) {
	calls := fakeRun(t, result{out: "ok"}, nil)
	require.NoError(t, macPrompt(context.Background(), "Unlock dokky"))
	require.Equal(t, "unlock dokky", (*calls)[0].args[5], "macOS shows it after \"dokky is trying to\"")
}

func decodeCommand(t *testing.T, enc string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(enc)
	require.NoError(t, err)
	require.Zero(t, len(b)%2)
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

func TestEncodeCommand(t *testing.T) {
	require.Equal(t, "YQCsIA==", encodeCommand("a€"))
	require.Equal(t, helloPromptScript, decodeCommand(t, encodeCommand(helloPromptScript)))
}

func TestHelloCommand(t *testing.T) {
	shell := powerShellPath(`C:\Windows\System32`)
	require.Equal(t, filepath.Join(`C:\Windows\System32`, "WindowsPowerShell", "v1.0", "powershell.exe"), shell)

	c := helloCommand(shell, helloPromptScript, hostile)
	require.Equal(t, shell, c.path)
	require.Equal(t, []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand"}, c.args[:5])
	require.Len(t, c.args, 6)
	require.Equal(t, helloPromptScript, decodeCommand(t, c.args[5]))
	require.Equal(t, []string{"DOKKY_AUTH_REASON=" + hostile}, c.env, "the reason travels in the environment")
	require.True(t, c.raise)
	for _, a := range c.args {
		require.NotContains(t, a, "rm -rf")
	}
	require.Contains(t, helloPromptScript, "$env:DOKKY_AUTH_REASON")
	require.Contains(t, helloPromptScript, "RequestVerificationAsync")
	require.Contains(t, helloCheckScript, "CheckAvailabilityAsync")
	require.Contains(t, helloPrelude, "IAsyncOperation`1", "the backtick survives the Go string")

	check := helloCommand(shell, helloCheckScript, "")
	require.Empty(t, check.env)
	require.False(t, check.raise)
}

func TestHelloAvailable(t *testing.T) {
	fakeRun(t, result{code: 0}, nil)
	name, ok := helloAvailable(context.Background(), "ps")
	require.True(t, ok)
	require.Equal(t, "Windows Hello", name)
	for _, code := range []int{1, 2, 4} {
		fakeRun(t, result{code: code}, nil)
		_, ok = helloAvailable(context.Background(), "ps")
		require.False(t, ok, code)
	}
}

func TestParseHello(t *testing.T) {
	require.NoError(t, parseHello(result{code: 0}, nil))
	require.ErrorIs(t, parseHello(result{code: 2}, nil), ErrUnavailable)
	require.ErrorIs(t, parseHello(result{code: 3}, nil), ErrCancelled)
	err := parseHello(result{code: 1}, nil)
	require.ErrorContains(t, err, "Windows Hello")
	require.NotErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, parseHello(result{code: -1}, os.ErrNotExist), ErrUnavailable)
	require.ErrorIs(t, parseHello(result{code: -1}, context.Canceled), ErrCancelled)
	require.ErrorIs(t, parseHello(result{code: -1}, errors.New("access denied")), ErrUnavailable)
}

func TestHelloPrompt(t *testing.T) {
	calls := fakeRun(t, result{code: 3}, nil)
	require.ErrorIs(t, helloPrompt(context.Background(), "ps", "unlock dokky"), ErrCancelled)
	require.Equal(t, []string{"DOKKY_AUTH_REASON=unlock dokky"}, (*calls)[0].env)
}

func TestPolkit(t *testing.T) {
	require.Equal(t, command{path: "/usr/bin/pkexec", args: []string{"--disable-internal-agent", "/bin/true"}}, polkitCommand("/usr/bin/pkexec"))

	fakeEnv(t, map[string]string{}, true)
	_, ok := polkitAvailable()
	require.False(t, ok, "no desktop session")
	require.ErrorIs(t, polkitPrompt(context.Background()), ErrUnavailable)

	fakeEnv(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, false)
	_, ok = polkitAvailable()
	require.False(t, ok, "no pkexec")
	require.ErrorIs(t, polkitPrompt(context.Background()), ErrUnavailable)

	fakeEnv(t, map[string]string{"DISPLAY": ":0"}, true)
	name, ok := polkitAvailable()
	require.True(t, ok)
	require.Equal(t, "your system password", name)

	calls := fakeRun(t, result{code: 0}, nil)
	require.NoError(t, polkitPrompt(context.Background()))
	require.Equal(t, "/usr/bin/pkexec", (*calls)[0].path)
}

func TestParsePolkit(t *testing.T) {
	require.NoError(t, parsePolkit(result{code: 0}, nil))
	require.ErrorIs(t, parsePolkit(result{code: 126}, nil), ErrCancelled)
	require.ErrorIs(t, parsePolkit(result{code: 127}, nil), ErrUnavailable)
	err := parsePolkit(result{code: 1}, nil)
	require.True(t, strings.Contains(err.Error(), "exit 1"))
	require.NotErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, parsePolkit(result{code: -1}, context.DeadlineExceeded), ErrCancelled)
	require.ErrorIs(t, parsePolkit(result{code: -1}, os.ErrPermission), ErrUnavailable)
}
