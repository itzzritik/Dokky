package sysauth

import "context"

func available(context.Context) (string, bool) { return polkitAvailable() }

func prompt(ctx context.Context, _ string) error { return polkitPrompt(ctx) }
