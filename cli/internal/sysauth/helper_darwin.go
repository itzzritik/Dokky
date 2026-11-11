package sysauth

import "embed"

// the release builds dokkyauth/dokky-auth on a Mac from main.swift before building dokky for macOS, so the
// Touch ID prompt says "dokky"; a build made without it has only the source here and asks through osascript
//
//go:embed dokkyauth
var helperFS embed.FS

func init() { helperBin, _ = helperFS.ReadFile("dokkyauth/dokky-auth") }
