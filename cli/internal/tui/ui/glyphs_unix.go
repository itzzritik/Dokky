//go:build !windows

package ui

import (
	"os"
	"strings"
)

func terminalSupportsUnicode() bool {
	if term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))); term == "dumb" || term == "linux" {
		return false
	}

	locale := ""
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			locale = value
			break
		}
	}
	if locale == "" {
		return false
	}

	locale = strings.ToLower(strings.SplitN(locale, "@", 2)[0])
	if locale == "c" || locale == "posix" {
		return false
	}
	codeset := locale
	if index := strings.LastIndex(locale, "."); index >= 0 {
		codeset = locale[index+1:]
	}
	codeset = strings.NewReplacer("-", "", "_", "").Replace(codeset)
	return codeset == "utf8"
}
