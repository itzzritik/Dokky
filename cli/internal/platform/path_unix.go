//go:build !windows

package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	blockStart = "# >>> dokky >>>"
	blockEnd   = "# <<< dokky <<<"
)

var (
	posixLine = `export PATH="$HOME/.local/bin:$PATH"`
	fishLine  = `fish_add_path $HOME/.local/bin`
	fishRc    = filepath.Join(".config", "fish", "config.fish")
	rcFiles   = []string{".zshrc", ".bashrc", ".bash_profile", fishRc}
)

func EnsurePath(home, stateDir string) (bool, error) {
	bin := filepath.Join(home, ".local", "bin")
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == bin {
			return false, nil
		}
	}

	var existing []string
	for _, rc := range rcFiles {
		if _, err := os.Stat(filepath.Join(home, rc)); err == nil {
			existing = append(existing, rc)
		}
	}
	if len(existing) == 0 {
		existing = []string{defaultRc()}
	}

	changed := false
	for _, rc := range existing {
		added, err := addBlock(filepath.Join(home, rc), rc == fishRc)
		if err != nil {
			return changed, err
		}
		changed = changed || added
	}
	if changed {
		return true, recordPathAdded(stateDir)
	}
	return false, nil
}

func RemovePath(home, stateDir string) error {
	for _, rc := range append(rcFiles, ".profile") {
		if err := removeBlock(filepath.Join(home, rc)); err != nil {
			return err
		}
	}
	return clearPathRecord(stateDir)
}

func defaultRc() string {
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return ".zshrc"
	case "bash":
		return ".bashrc"
	case "fish":
		return fishRc
	}
	return ".profile"
}

func addBlock(path string, fish bool) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	s := string(b)
	if strings.Contains(s, blockStart) {
		return false, nil
	}
	line := posixLine
	if fish {
		line = fishLine
	}
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += blockStart + "\n" + line + "\n" + blockEnd + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(s), 0o644)
}

func removeBlock(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	start, end := -1, -1
	for i, l := range lines {
		switch {
		case start < 0 && strings.TrimSpace(l) == blockStart:
			start = i
		case start >= 0 && strings.TrimSpace(l) == blockEnd:
			end = i
		}
		if end >= 0 {
			break
		}
	}
	if start < 0 || end < 0 {
		return nil
	}
	out := append(lines[:start:start], lines[end+1:]...)
	return os.WriteFile(path, []byte(strings.Join(out, "")), 0o644)
}
