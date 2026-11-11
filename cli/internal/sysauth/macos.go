package sysauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const osascript = "/usr/bin/osascript"

// JXA over the ObjC bridge keeps the build cgo free. policy 1 is LAPolicyDeviceOwnerAuthenticationWithBiometrics,
// 2 is LAPolicyDeviceOwnerAuthentication (Touch ID with the Mac password as the fallback).
// the reply block fires on another thread, so the main run loop is pumped until it does.
// the reason arrives as an argument, never inside the script, so no text can change what runs
const macScript = `ObjC.import('LocalAuthentication');
ObjC.import('Foundation');

function describe(e) {
  if (!e || e.isNil()) return 'unknown';
  return ObjC.unwrap(e.localizedDescription) + ' (' + e.code + ')';
}

function classify(e) {
  if (!e || e.isNil()) return 'error:unknown';
  var code = e.code;
  if (code === -2 || code === -4 || code === -9) return 'cancel';
  if ([-5, -6, -7, -10, -11, -1004].indexOf(code) >= 0) return 'unavailable:' + describe(e);
  return 'error:' + describe(e);
}

function run(argv) {
  var ctx = $.LAContext.alloc.init;
  if (argv[0] === 'check') {
    if (ctx.canEvaluatePolicyError(1, null)) return 'touchid';
    if (ctx.canEvaluatePolicyError(2, null)) return 'password';
    return 'none';
  }
  var err = Ref();
  if (!ctx.canEvaluatePolicyError(2, err)) return 'unavailable:' + describe(err[0]);
  var done = false;
  var result = 'error:no reply';
  ctx.evaluatePolicyLocalizedReasonReply(2, argv[1], function (ok, e) {
    result = ok ? 'ok' : classify(e);
    done = true;
  });
  var deadline = Date.now() + 115000;
  while (!done && Date.now() < deadline) {
    $.NSRunLoop.currentRunLoop.runModeBeforeDate($.NSDefaultRunLoopMode, $.NSDate.dateWithTimeIntervalSinceNow(0.1));
  }
  if (!done) {
    ctx.invalidate;
    return 'cancel';
  }
  return result;
}`

// the helper when this dokky carries one, so the prompt names dokky; else the same answers from a script
func macCommand(mode, reason string) command {
	if helper := helperPath(); helper != "" {
		args := []string{mode}
		if mode == "prompt" {
			args = append(args, reason)
		}
		return command{path: helper, args: args}
	}
	args := []string{"-l", "JavaScript", "-e", macScript, mode}
	if mode == "prompt" {
		args = append(args, reason)
	}
	return command{path: osascript, args: args}
}

func macAvailable(ctx context.Context) (string, bool) {
	res, err := run(ctx, macCommand("check", ""))
	if err != nil || res.code != 0 {
		return "", false
	}
	return parseMacCheck(res.out)
}

func parseMacCheck(out string) (string, bool) {
	switch out {
	case "touchid":
		return "Touch ID", true
	case "password":
		return "your Mac password", true
	}
	return "", false
}

// macOS reads the reason after "Dokky is trying to", so it starts lower case there: "unlock Dokky"
func macPrompt(ctx context.Context, reason string) error {
	if r, n := utf8.DecodeRuneInString(reason); n > 0 {
		reason = string(unicode.ToLower(r)) + reason[n:]
	}
	res, err := run(ctx, macCommand("prompt", reason))
	return parseMacPrompt(res, err)
}

func parseMacPrompt(res result, err error) error {
	switch {
	case err != nil && stopped(err):
		return ErrCancelled
	case err != nil:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	case res.code != 0:
		return fmt.Errorf("the macOS prompt failed: %s", firstLine(res.stderr, fmt.Sprintf("exit %d", res.code)))
	}
	kind, msg, _ := strings.Cut(res.out, ":")
	switch kind {
	case "ok":
		return nil
	case "cancel":
		return ErrCancelled
	case "unavailable":
		return fmt.Errorf("%w: %s", ErrUnavailable, msg)
	case "error":
		return fmt.Errorf("could not confirm it is you with Touch ID: %s", msg)
	}
	return fmt.Errorf("the macOS prompt gave an answer dokky does not know: %q", res.out)
}

func stopped(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func firstLine(s, fallback string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if s == "" {
		return fallback
	}
	return s
}
