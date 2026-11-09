package vault

import (
	_ "embed"
	"math"
	"strings"
	"sync"
	"unicode"
)

const (
	MinPassword  = 10
	MaxPassword  = 128
	longPassword = 16 // long enough to be one character class
	okScore      = 2
)

//go:embed common.txt
var commonText string

var common = sync.OnceValue(func() map[string]bool {
	m := map[string]bool{}
	for line := range strings.Lines(commonText) {
		if w := strings.TrimSpace(line); w != "" && !strings.HasPrefix(w, "#") {
			m[w] = true
		}
	}
	return m
})

// CheckPassword scores 0 to 4 and says whether the password may be a master password, with a short hint for the meter
func CheckPassword(pw string) (score int, ok bool, hint string) {
	runes := []rune(pw)
	n := len(runes)
	score, patterned := strength(runes)
	switch {
	case n < MinPassword:
		return min(score, 1), false, "use at least 10 characters"
	case n > MaxPassword:
		return min(score, 1), false, "use at most 128 characters"
	case strings.TrimSpace(pw) != pw:
		return min(score, 1), false, "remove the spaces at the start or end"
	case isCommon(pw):
		return 0, false, "this is a common password, pick something less guessable"
	case classes(runes) == 1 && n < longPassword:
		return min(score, 1), false, "mix in capitals, numbers or symbols, or use 16 or more characters"
	case score < okScore && patterned:
		return score, false, "avoid repeats and runs like aaaa or 1234"
	case score < okScore:
		return score, false, "make it longer or mix in capitals, numbers and symbols"
	case score == okScore:
		return score, true, "okay, a few more characters makes it stronger"
	case score == 3:
		return score, true, "good"
	}
	return score, true, "strong"
}

// a rough entropy estimate: pool size per character, with repeats, runs and a small set of distinct characters counted less
func strength(runes []rune) (score int, patterned bool) {
	if len(runes) == 0 {
		return 0, false
	}
	effective := 0.0
	for i := range runes {
		if i >= 2 && step(runes[i-1], runes[i]) == step(runes[i-2], runes[i-1]) && abs(step(runes[i-1], runes[i])) <= 1 {
			patterned = true
			continue
		}
		effective++
	}
	distinct := map[rune]bool{}
	for _, r := range runes {
		distinct[r] = true
	}
	if limit := 1.5 * float64(len(distinct)); effective > limit {
		effective = limit
		patterned = true
	}
	bits := effective * math.Log2(float64(pool(runes)))
	switch {
	case bits < 30:
		return 0, patterned
	case bits < 45:
		return 1, patterned
	case bits < 60:
		return 2, patterned
	case bits < 80:
		return 3, patterned
	}
	return 4, patterned
}

func step(a, b rune) int { return int(b) - int(a) }

func abs(n int) int { return max(n, -n) }

func pool(runes []rune) int {
	var lower, upper, digit, other, wide bool
	for _, r := range runes {
		switch {
		case r > unicode.MaxASCII:
			wide = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	size := 0
	for _, c := range []struct {
		has  bool
		size int
	}{{lower, 26}, {upper, 26}, {digit, 10}, {other, 33}, {wide, 100}} {
		if c.has {
			size += c.size
		}
	}
	return size
}

// lowercase, uppercase, digits and everything else
func classes(runes []rune) int {
	seen := map[int]bool{}
	for _, r := range runes {
		switch {
		case unicode.IsLower(r):
			seen[0] = true
		case unicode.IsUpper(r):
			seen[1] = true
		case unicode.IsDigit(r):
			seen[2] = true
		default:
			seen[3] = true
		}
	}
	return len(seen)
}

// Password2024! and p@ssw0rd are as guessable as password
func isCommon(pw string) bool {
	lower := strings.ToLower(pw)
	trimmed := strings.TrimRightFunc(lower, notLetter)
	for _, c := range []string{
		lower,
		strings.TrimLeftFunc(trimmed, notLetter),
		strings.TrimLeftFunc(unleet.Replace(trimmed), notLetter),
	} {
		if c != "" && common()[c] {
			return true
		}
	}
	return false
}

var unleet = strings.NewReplacer("0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "@", "a", "$", "s", "!", "i")

func notLetter(r rune) bool { return !unicode.IsLetter(r) }
