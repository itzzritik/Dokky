package sysauth

import "context"

func available(ctx context.Context) (string, bool) { return macAvailable(ctx) }

func prompt(ctx context.Context, reason string) error { return macPrompt(ctx, reason) }
