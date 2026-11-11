#!/usr/bin/env bash
# Builds dokky's macOS Touch ID helper for both Mac chips, with its Info.plist inside so macOS names dokky in
# the prompt, into cli/internal/sysauth/dokkyauth where the darwin build embeds it. Runs on a Mac with Xcode.
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)/cli/internal/sysauth/dokkyauth"
out="${1:-$src/dokky-auth}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos12" -o "$tmp/$arch" \
    -Xlinker -sectcreate -Xlinker __TEXT -Xlinker __info_plist -Xlinker "$src/Info.plist" \
    "$src/main.swift"
done
lipo -create -output "$out" "$tmp/arm64" "$tmp/x86_64"
codesign --force --sign - --identifier me.ritik.dokky.auth "$out"
"$out" check
