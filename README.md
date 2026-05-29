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
        "PACER_CORE_URL": "https://api.pacer.run",
        "PACER_CORE_TOKEN": "..."
      }
    }
  }
}
```

| Env var | Description | Default |
|---------|-------------|---------|
| `PACER_CORE_URL` | Base URL of the pacer/core API | `https://api.pacer.run` |
| `PACER_CORE_TOKEN` | Bearer token for authenticated endpoints | (unset) |

Run `health_check` after install to confirm the server can reach core.

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
