//go:build ignore

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// sha256 of the upstream file each patch was made from; a mismatch means the patch must be redone
var patches = map[string]string{
	"vfs/file.go": "0411d72fbd6b9e4e73f1e904e0e3d082add590560439a9c3755c17f29c8aa223",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rclone overlay:", err)
		os.Exit(1)
	}
}

func run() error {
	if out, err := exec.Command("go", "mod", "vendor").CombinedOutput(); err != nil {
		return fmt.Errorf("go mod vendor: %w: %s", err, out)
	}
	here, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	replace := map[string]string{}
	for file, want := range patches {
		upstream := filepath.Join(here, "vendor", "github.com", "rclone", "rclone", filepath.FromSlash(file))
		b, err := os.ReadFile(upstream)
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != want {
			return fmt.Errorf("%s changed upstream, redo _patches/rclone/%s against the new version", file, file)
		}
		replace[upstream] = filepath.Join(here, "_patches", "rclone", filepath.FromSlash(file))
	}
	b, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return err
	}
	overlay := filepath.Join(here, "_patches", "overlay.json")
	if err := os.WriteFile(overlay, b, 0o644); err != nil {
		return err
	}
	fmt.Println("-overlay=" + overlay)
	return nil
}
