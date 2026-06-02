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
        "PACER_CORE_URL": "https://portal.pacerrev.io",
        "PACER_CORE_TOKEN": "pat_..."
      }
    }
  }
}
```

| Env var | Description | Default |
|---------|-------------|---------|
| `PACER_CORE_URL` | Base URL of the pacer/core app (PAT JSON API is mounted here) | `https://portal.pacerrev.io` |
| `PACER_CORE_TOKEN` | Personal access token, format `pat_...` | (unset) |

Run `health_check` after install to confirm the server can reach core.

PATs are minted by a core admin via `pacer pat create --user <email> --label <name>`; they require an `employee`-or-higher role and are sent as `Authorization: Bearer pat_...`.

## Tools

Each tool's full description (caveats, when to use, args) is returned to the MCP client at registration time — agents see them at tool-selection time. The list below is a quick index.

### Operational

| Tool | What it does |
|------|--------------|
| `health_check` | Pings the Pacer API and reports config + reachability |

### Portfolio fundamentals

| Tool | What it does |
|------|--------------|
| `list_briefable_portfolios` | Enumerate active portfolios (optional `q` name filter) |
| `list_portfolio_teams` | Bulk: notification team (RM/RD/Jon) for every portfolio |
| `get_portfolio_team` | Notification team for a single portfolio |
| `list_portfolio_units` | Unit roster: bedrooms, type, managed/active, location |
| `list_portfolio_reservations` | Reservations in a date range (by `check_in`/`check_out`/`booked_on`) |

### Performance

| Tool | What it does |
|------|--------------|
| `get_portfolio_pacing` | Recent reservations w/ YoY rent/ADR/ABW/LOS + anomaly score |
| `get_portfolio_metrics_ytd` | CY vs PY YTD: revenue, ADR, occupancy, RevPAR, LOS, count |
| `get_portfolio_market_metrics` | One-month CY vs PY + market benchmark deltas; optional decomposition |

### Guesty PMS

| Tool | What it does |
|------|--------------|
| `guesty_pricing_config` | Per-unit pricing intent: base price, fees, min/max nights, factors, channel settings |
| `guesty_reservation_promotions` | Channel-applied promos on reservations in a month (Airbnb, Vrbo, etc.) |

### Client health

| Tool | What it does |
|------|--------------|
| `get_client_health_brief` | Latest (or dated) sentiment brief for a portfolio |
| `upsert_client_health_brief` | Log a lightweight sentiment brief (1-5 + stage + payload) |
| `list_client_health_briefs` | Dashboard view: latest brief per portfolio as of a date |
| `get_client_health_scoring_config` | Scoring weights, labels, and tier thresholds |
| `upsert_intel_brief` | Publish a full intel brief (Postgres + ClickUp task + BigQuery mirror) |

### KeyData

| Tool | What it does |
|------|--------------|
| `list_managed_keydata_units` | Pacer-managed unit UUIDs for a KeyData customer account |

> Not wrapped: `POST /portfolios/{p}/intel-brief/attachments` (multipart binary upload). MCP has no natural multipart story.

## Development

```bash
go build -o pacer-mcp .
go test ./...
golangci-lint run --fix ./...
```

Releases are automated — push a `v*` tag and GitHub Actions builds darwin-arm64 and windows-amd64 binaries, then updates the Homebrew tap and Scoop bucket.

## License

MIT
