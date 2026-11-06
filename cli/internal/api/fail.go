package api

import (
	"slices"
	"strings"
)

var (
	offlineWords = []string{"no such host", "dial tcp", "connection refused", "connection reset", "timeout", "timed out", "did not answer", "unreachable", "tls", "eof", "502", "503", "504", "unavailable"}
	signInWords  = []string{"login", "unauthorized", "401", "invalid_grant", "token", "expired", "auth", "password", "credential", "access key", "signature"}
)

// network first, since a sign-in that can't reach the service also says login; "offline", "signin" or "other"
func FailKind(err string) string {
	e := strings.ToLower(err)
	has := func(words []string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(e, w) })
	}
	switch {
	case has(offlineWords):
		return "offline"
	case has(signInWords):
		return "signin"
	}
	return "other"
}
