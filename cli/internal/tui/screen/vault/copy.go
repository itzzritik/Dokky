package vault

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

// the daemon's code for a failed vault call, "" for any other failure
func code(err error) string {
	var e *client.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// a vault call's failure as one line for its screen; "" when the person cancelled it
func Explain(err error, st *core.State) string { return explain(err, st.Snap.Vault, st.Now) }

// off the update loop, with the vault's state copied before
func explain(err error, v api.Vault, now time.Time) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return ""
	}
	switch code(err) {
	case api.ErrWrongPassword:
		return "That password is not right."
	case api.ErrTooManyAttempts:
		if left := v.CooldownUntil.Sub(now); left > 0 {
			return cooldown(left)
		}
		if msg := daemonSays(err); msg != "" {
			return msg
		}
		return "Too many tries. Wait a moment and try again."
	case api.ErrCancelled:
		return "Cancelled. Try again or use your master password."
	case api.ErrSystemUnavailable:
		return systemChoice(v) + " isn't available right now."
	case api.ErrPasswordRequired:
		if v.PasswordDays > 0 {
			return "It's been " + strconv.Itoa(v.PasswordDays) + " days. Type your master password this time."
		}
		return "Type your master password this time."
	case api.ErrWeakPassword:
		if msg := daemonSays(err); msg != "" {
			return msg
		}
		return "Pick a stronger password."
	case api.ErrBadRecoveryKey:
		return "That recovery key is not right."
	case api.ErrGrantRequired:
		return "That took too long. Try again."
	case api.ErrLocked:
		return "dokky is locked. Unlock it first."
	}
	return core.Report(err)
}

// too many wrong passwords: the wait, counted down while the screen shows it
func cooldown(left time.Duration) string {
	return "Too many tries. Try again in " + core.In(left.Round(time.Second)) + "."
}

// a form's error line: the live countdown while one runs, nothing once a known one ran out
func errLine(err string, cool bool, st *core.State) string {
	switch left := coolingFor(st); {
	case left > 0:
		return cooldown(left)
	case cool && !st.Snap.Vault.CooldownUntil.IsZero():
		return ""
	}
	return err
}

func daemonSays(err error) string {
	var e *client.Error
	if !errors.As(err, &e) || strings.TrimSpace(e.Message) == "" {
		return ""
	}
	return core.Report(errors.New(e.Message))
}

// the system check by its own name, "Touch ID" or "your system password"
func systemName(v api.Vault) string {
	if v.System != "" {
		return v.System
	}
	switch runtime.GOOS {
	case "darwin":
		return "Touch ID"
	case "windows":
		return "Windows Hello"
	}
	return "your system password"
}

// as a choice: "Touch ID", "Mac password", "System password"
func systemChoice(v api.Vault) string {
	return upper(strings.TrimPrefix(systemName(v), "your "))
}

// Touch ID or Windows Hello can answer here: the daemon has one, the TUI isn't over SSH, and the password isn't due
func offered(st *core.State) bool {
	v := st.Snap.Vault
	return v.System != "" && !st.Deps.Remote && !v.PasswordDue
}

func upper(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// a hint from vault.CheckPassword as a sentence
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s[len(s)-1:], ".!?") {
		s += "."
	}
	return upper(s)
}
