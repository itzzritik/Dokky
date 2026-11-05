package core

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var calls = map[string]bool{"dial": true, "read": true, "write": true, "lookup": true, "open": true, "stat": true, "lstat": true,
	"mkdir": true, "remove": true, "rename": true, "unmount": true, "get": true, "post": true, "put": true, "delete": true, "head": true}

var plain = strings.NewReplacer("context deadline exceeded", "it took too long", "i/o timeout", "it took too long",
	"connection refused", "the server refused to connect", "no such host", "the server can't be found")

// a daemon error as one plain sentence; "" for a cancel
func Report(err error) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return ""
	}
	first, _, _ := strings.Cut(err.Error(), "\n")
	first = Safe(first)
	var keep []string
	for _, seg := range strings.Split(first, ": ") {
		if seg = strings.TrimSpace(seg); seg != "" && !technical(seg) {
			keep = append(keep, seg)
		}
	}
	s := plain.Replace(strings.Join(keep, ": "))
	if s == "" {
		s = strings.TrimSpace(first[strings.LastIndex(first, ": ")+1:])
	}
	return sentence(s)
}

// rc method paths, bare words, rclone's "Error in X", and Go's "dial tcp 1.2.3.4" or `Get "url"` wrappers
func technical(seg string) bool {
	f := strings.Fields(seg)
	if len(f) == 1 || strings.HasPrefix(strings.ToLower(seg), "error in ") {
		return true
	}
	if strings.HasPrefix(f[1], "/") || strings.HasPrefix(f[1], `"`) || strings.HasPrefix(f[1], "http") || f[1] == "tcp" || f[1] == "udp" {
		return true
	}
	return len(f) <= 3 && calls[strings.ToLower(f[0])]
}

func sentence(s string) string {
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "dokky") {
		r, n := utf8.DecodeRuneInString(s)
		s = string(unicode.ToUpper(r)) + s[n:]
	}
	if !strings.ContainsAny(s[len(s)-1:], ".!?") {
		s += "."
	}
	return s
}
