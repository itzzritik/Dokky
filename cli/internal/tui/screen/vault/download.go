package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const keyFile = "dokky-recovery-key"

// the person's Downloads folder, else their home
func downloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, "Downloads")
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d, nil
	}
	return home, nil
}

// what the file says: the key, whose it is, when, and what it is for
func keyText(key, email string, now time.Time) string {
	lines := []string{"dokky recovery key", "", key, ""}
	if email != "" {
		lines = append(lines, "Account: "+email)
	}
	lines = append(lines, "Created: "+now.Format("2 January 2006"), "",
		"Use it in dokky if you forget your master password.",
		"Keep it somewhere safe. dokky can't show it again.", "")
	return strings.Join(lines, "\n")
}

// writes the key to a new file in Downloads, never over another file; again, when set, is this screen's earlier file, written over
func saveKey(again, key, email string, now time.Time) (string, error) {
	text := []byte(keyText(key, email, now))
	if again != "" {
		return again, os.WriteFile(again, text, 0o600)
	}
	dir, err := downloadsDir()
	if err != nil {
		return "", err
	}
	return writeNew(dir, text)
}

// dokky-recovery-key.txt, else dokky-recovery-key (2).txt and on, like a browser download
func writeNew(dir string, text []byte) (string, error) {
	for i := 1; i <= 99; i++ {
		name := keyFile + ".txt"
		if i > 1 {
			name = keyFile + " (" + strconv.Itoa(i) + ").txt"
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(text)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(p)
			return "", err
		}
		return p, nil
	}
	return "", errors.New("too many recovery key files in " + dir)
}

// ~/Downloads/dokky-recovery-key.txt rather than the full path
func tidy(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return p
}
