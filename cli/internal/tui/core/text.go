package core

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func Safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(Safe(s), w, "…")
}

func Size(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n), 0
	for v >= unit && i < 5 {
		v /= unit
		i++
	}
	f := "%.0f %s"
	if v < 10 {
		f = "%.1f %s"
	}
	return fmt.Sprintf(f, v, []string{"B", "KB", "MB", "GB", "TB", "PB"}[i])
}

// "1 drive", "3 drives"
func Count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func Speed(bps float64) string { return Size(int64(bps)) + "/s" }

func Ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return "never"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "min") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 48*time.Hour:
		return "yesterday"
	}
	return t.Local().Format("2 January 2006")
}

func In(d time.Duration) string {
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()+.5))
	case d < time.Hour:
		return plural(int(d.Minutes()+.5), "min")
	}
	return plural(int(d.Hours()+.5), "hour")
}

func plural(n int, unit string) string {
	if n == 1 || unit == "min" {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
