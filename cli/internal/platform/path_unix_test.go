//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (home, state string) {
	t.Helper()
	home = t.TempDir()
	state = filepath.Join(t.TempDir(), "state")
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SHELL", "/bin/zsh")
	return home, state
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

func TestEnsurePathAddsBlockOnce(t *testing.T) {
	home, state := setup(t)
	rc := filepath.Join(home, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("alias a=b\n"), 0o644))

	changed, err := EnsurePath(home, state)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = EnsurePath(home, state)
	require.NoError(t, err)
	require.False(t, changed)

	require.Equal(t, 1, strings.Count(read(t, rc), "# >>> dokky >>>"))
	require.Contains(t, read(t, filepath.Join(state, "path.json")), `"pathAdded": true`)
}

func TestEnsurePathSkipsWhenOnPath(t *testing.T) {
	home, state := setup(t)
	t.Setenv("PATH", "/usr/bin:"+filepath.Join(home, ".local", "bin"))
	rc := filepath.Join(home, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("alias a=b\n"), 0o644))

	changed, err := EnsurePath(home, state)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "alias a=b\n", read(t, rc))
	require.NoFileExists(t, filepath.Join(state, "path.json"))
}

func TestEnsurePathFishSyntax(t *testing.T) {
	home, state := setup(t)
	fish := filepath.Join(home, ".config", "fish", "config.fish")
	require.NoError(t, os.MkdirAll(filepath.Dir(fish), 0o755))
	require.NoError(t, os.WriteFile(fish, nil, 0o644))

	_, err := EnsurePath(home, state)
	require.NoError(t, err)
	require.Contains(t, read(t, fish), "fish_add_path $HOME/.local/bin")
}

func TestEnsurePathCreatesRcForShell(t *testing.T) {
	cases := map[string]string{
		"/bin/zsh":      ".zshrc",
		"/usr/bin/bash": ".bashrc",
		"/usr/bin/fish": filepath.Join(".config", "fish", "config.fish"),
		"/bin/dash":     ".profile",
		"":              ".profile",
	}
	for shell, want := range cases {
		home, state := setup(t)
		t.Setenv("SHELL", shell)

		changed, err := EnsurePath(home, state)
		require.NoError(t, err)
		require.True(t, changed, shell)
		require.Contains(t, read(t, filepath.Join(home, want)), "# >>> dokky >>>", shell)
	}
}

func TestRemovePathOnlyRemovesOwnBlock(t *testing.T) {
	home, state := setup(t)
	rc := filepath.Join(home, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("before\n"), 0o644))
	_, err := EnsurePath(home, state)
	require.NoError(t, err)
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString("after\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.NoError(t, RemovePath(home, state))
	require.Equal(t, "before\nafter\n", read(t, rc))
	require.NoFileExists(t, filepath.Join(state, "path.json"))
}

func TestRemovePathLeavesUnterminatedBlock(t *testing.T) {
	home, state := setup(t)
	rc := filepath.Join(home, ".zshrc")
	content := "before\n# >>> dokky >>>\nexport X=1\nafter\n"
	require.NoError(t, os.WriteFile(rc, []byte(content), 0o644))

	require.NoError(t, RemovePath(home, state))
	require.Equal(t, content, read(t, rc))
}
