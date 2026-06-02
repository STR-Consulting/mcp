# pacer-mcp

An MCP server that exposes [pacer/core](https://github.com/pacer/core) API endpoints as native tools for Claude Code (and any other MCP-aware client). One binary, stdio transport, no fuss.

## Install

### macOS (Homebrew)

```
brew install STR-Consulting/tap/pacer-mcp
```

### Windows (Scoop)

```
scoop bucket add pacer https://github.com/STR-Consulting/scoop-bucket
scoop install pacer-mcp
```

Non-programmer? See [docs/windows-setup.md](docs/windows-setup.md) for a copy-paste prompt to hand to your AI assistant.

### From source

```
go install github.com/STR-Consulting/pacer-mcp@latest
```

## Setup

Add to your MCP config (e.g. `.mcp.json` in your project or your Claude Code user config):

```json
{
  "mcpServers": {
    "pacer": {
      "command": "pacer-mcp",
      "env": {
        "PACER_CORE_URL": "https://mc.pacerrev.io",
        "PACER_CORE_TOKEN": "pat_..."
      }
    }
  }
}
```

| Env var | Description | Default |
|---------|-------------|---------|
| `PACER_CORE_URL` | Base URL of the pacer/core `mc` app | `https://mc.pacerrev.io` |
| `PACER_CORE_TOKEN` | Personal access token, format `pat_...` | (unset) |

Run `health_check` after install to confirm the server can reach core.

PATs are minted by a core admin via `pacer pat create --user <email> --label <name>`; they require an `employee`-or-higher role and are sent as `Authorization: Bearer pat_...`.

## Tools

| Tool | What it does |
|------|--------------|
| `health_check` | Pings the core API and reports config + reachability |

More to come.

## Development

```bash
go build -o pacer-mcp .
go test ./...
golangci-lint run --fix ./...
```

Releases are automated — push a `v*` tag and GitHub Actions builds darwin-arm64 and windows-amd64 binaries, then updates the Homebrew tap and Scoop bucket.

## License

MIT
