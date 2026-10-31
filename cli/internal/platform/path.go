package platform

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func recordPathAdded(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(map[string]bool{"pathAdded": true}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "path.json"), b, 0o644)
}

func clearPathRecord(stateDir string) error {
	err := os.Remove(filepath.Join(stateDir, "path.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
