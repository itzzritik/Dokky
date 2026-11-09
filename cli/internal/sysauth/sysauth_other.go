//go:build !darwin && !linux && !windows

package sysauth

import "context"

func available(context.Context) (string, bool) { return "", false }

func prompt(context.Context, string) error { return ErrUnavailable }
