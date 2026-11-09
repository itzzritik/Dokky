package vault

import "unicode/utf8"

const (
	MinPassword = 8    // the one rule for a master password
	MaxPassword = 1024 // only bounds what a field takes
)

// CheckPassword reports whether pw is long enough to be a master password
func CheckPassword(pw string) bool { return utf8.RuneCountInString(pw) >= MinPassword }
