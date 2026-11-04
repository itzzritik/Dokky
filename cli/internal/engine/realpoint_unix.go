//go:build !windows

package engine

import "path/filepath"

// realPoint resolves symlinks in the parent only, never stepping into the mount itself
func realPoint(p string) string {
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return p
	}
	return filepath.Join(dir, filepath.Base(p))
}
