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
	"strings"
)

// sha256 of the upstream file each patch was made from; a mismatch means the patch must be redone.
// A patch sits at _patches/<repo>/<file>.
var patches = map[string]string{
	"github.com/rclone/rclone/vfs/file.go":                  "0411d72fbd6b9e4e73f1e904e0e3d082add590560439a9c3755c17f29c8aa223",
	"github.com/rclone/rclone/fs/accounting/transfermap.go": "2f7f20476bddc395ed9df86d869ae3ffc2166336f15fbf31fb131b1696d388c9",
	"github.com/rclone/rclone/fs/accounting/inprogress.go":  "454b32c359cbef429071ac9542b1048317644045125600476d42c440bc884d62",
	"github.com/willscott/go-nfs/conn.go":                   "a62885ef2f7ff40355c3fe8112832e86456342c6f07d2016788c0ca87a254e6e",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "overlay:", err)
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
		upstream := filepath.Join(here, "vendor", filepath.FromSlash(file))
		patch := strings.SplitN(file, "/", 3)[2]
		b, err := os.ReadFile(upstream)
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != want {
			return fmt.Errorf("%s changed upstream, redo _patches/%s against the new version", file, patch)
		}
		replace[upstream] = filepath.Join(here, "_patches", filepath.FromSlash(patch))
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
