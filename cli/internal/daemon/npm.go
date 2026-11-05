package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// the npm that installed dokky, found beside its recorded package so launchd's bare PATH doesn't matter; else npm on PATH
func NPM(ctx context.Context, state string, args ...string) (*exec.Cmd, error) {
	records, _, _ := readInstalls(state)
	for _, r := range records {
		root := globalModules(r)
		cli, node := filepath.Join(root, "npm", "bin", "npm-cli.js"), nodeBeside(root)
		if isFile(cli) && isFile(node) {
			cmd := exec.CommandContext(ctx, node, append([]string{cli}, args...)...)
			// install scripts run "node"
			cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(node)+string(os.PathListSeparator)+os.Getenv("PATH"))
			return cmd, nil
		}
	}
	npm, err := exec.LookPath("npm")
	if err != nil {
		return nil, errors.New("npm was not found, run npm i -g dokky")
	}
	return exec.CommandContext(ctx, npm, args...), nil
}

// <prefix>/bin/node beside <prefix>/lib/node_modules; on Windows node.exe sits beside node_modules
func nodeBeside(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(filepath.Dir(root), "node.exe")
	}
	return filepath.Join(filepath.Dir(filepath.Dir(root)), "bin", "node")
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
