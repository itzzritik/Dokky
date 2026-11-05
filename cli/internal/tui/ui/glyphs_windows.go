//go:build windows

package ui

import (
	"os"
	"strings"
)

func terminalSupportsUnicode() bool {
	return os.Getenv("WT_SESSION") != "" ||
		os.Getenv("TERM_PROGRAM") != "" ||
		strings.EqualFold(os.Getenv("ConEmuANSI"), "ON")
}
