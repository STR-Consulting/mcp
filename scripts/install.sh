#!/usr/bin/env bash
set -euo pipefail

target="$(realpath "$(brew --prefix pacer-mcp)/bin/pacer-mcp")"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

go build -o "$tmp" .
install -m 755 "$tmp" "$target"

echo "Installed to $target"
