#!/usr/bin/env bash
# Assemble the MCPB bundle for Claude Desktop install.
#
# Inputs:
#   - mcpb/manifest.json (manifest template; version field is stamped here)
#   - dist/ produced by `goreleaser release` containing per-platform binaries
#
# Output:
#   - dist/pacer-mcp-<version>.mcpb (a ZIP archive ready to be attached to a
#     GitHub release and drag-dropped into Claude Desktop)
#
# Requires: npx (Node), jq.

set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [ ! -d dist ]; then
  echo "::error::dist/ not found — run goreleaser first" >&2
  exit 1
fi

darwin_bin="$(find dist -type f -path '*darwin_arm64*/pacer-mcp' | head -n1)"
windows_bin="$(find dist -type f -path '*windows_amd64*/pacer-mcp.exe' | head -n1)"

if [ -z "$darwin_bin" ] || [ -z "$windows_bin" ]; then
  echo "::error::could not locate per-platform binaries under dist/" >&2
  find dist -maxdepth 3 -type f -name 'pacer-mcp*' >&2 || true
  exit 1
fi

build_dir="dist/mcpb-build"
rm -rf "$build_dir"
mkdir -p "$build_dir/server"

cp "$darwin_bin" "$build_dir/server/pacer-mcp-darwin-arm64"
cp "$windows_bin" "$build_dir/server/pacer-mcp-windows-amd64.exe"
chmod +x "$build_dir/server/pacer-mcp-darwin-arm64"

jq --arg v "$VERSION" '.version = $v' mcpb/manifest.json > "$build_dir/manifest.json"

out="dist/pacer-mcp-${VERSION}.mcpb"
rm -f "$out"

# `mcpb pack <dir> <output>` zips the directory contents into a .mcpb.
npx --yes @anthropic-ai/mcpb pack "$build_dir" "$out"

echo "built $out"
