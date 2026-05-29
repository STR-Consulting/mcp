# pacer-mcp

MCP server in Go that exposes pacer/core API endpoints as native Claude Code tools over stdio.

## Project Info

- **Language:** Go
- **Binary:** `pacer-mcp` (MCP server, stdio transport)
- **Repo:** github.com/STR-Consulting/pacer-mcp
- **Core API:** sibling `../core` repo (github.com/pacer/core)

## Architecture

Single Go binary, runs as MCP server via stdio. Claude Code launches it as a child process.

### MCP Tools

| Tool | Description |
|------|-------------|
| `health_check` | Verify connectivity to pacer/core and report config status |

More tools will be added as the core API surface stabilizes.

### Configuration

| Env var | Description | Default |
|---------|-------------|---------|
| `PACER_CORE_URL` | Base URL of the pacer/core API | `https://api.pacer.run` |
| `PACER_CORE_TOKEN` | Bearer token for authenticated endpoints | (unset) |

### MCP config (end-user)

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

## Dev Guidelines

- Keep it simple — single `main.go` or minimal packages
- Stdlib + the MCP SDK; avoid extra deps
- Cross-platform: must build for darwin-arm64, windows-amd64
- Always run `golangci-lint run --fix ./...` after modifying Go code
- Always run `shellcheck` after modifying shell scripts

## Build & Test

```bash
go build -o pacer-mcp .
go test ./...
```

### Cross-compile

```bash
GOOS=darwin GOARCH=arm64 go build -o dist/pacer-mcp-darwin-arm64 .
GOOS=windows GOARCH=amd64 go build -o dist/pacer-mcp-windows-amd64.exe .
```

## Release

Push a `v*` tag — GitHub Actions builds darwin-arm64 and windows-amd64 binaries via goreleaser, then updates the [Homebrew tap](https://github.com/STR-Consulting/homebrew-tap) and [Scoop bucket](https://github.com/STR-Consulting/scoop-bucket).
