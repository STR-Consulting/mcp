# pacer-mcp

MCP server in Go that exposes [pacer/core](../core) API endpoints as native Claude Code tools over stdio.

> **⚠️ This repo is PUBLIC.** Do not commit customer names, employee names,
> portfolio identifiers, real reservation/unit IDs, PATs, internal hostnames
> beyond `portal.pacerrev.io`, or any data pulled from a live `core` instance.
> Commit messages, code comments, and test fixtures are all world-readable.
> When pasting examples from probes or jig threads, scrub proper nouns first.
> The upstream `pacer/core` repo is private and is where sensitive context
> belongs.
>
> **This repo must be worked in conjunction with `../core`.** It has no jig
> config of its own — all issue tracking for pacer-mcp lives in
> `../core/.issues/` and syncs to the shared ClickUp list. Run all `jig`
> commands from `../core`. Use issue tags or titles (e.g. `pacer-mcp:`) to
> distinguish wrapper work from `core` API work.

## Project Info

- **Language:** Go (single `main.go`, stdio transport)
- **Binary:** `pacer-mcp`
- **Repo:** github.com/STR-Consulting/mcp (public)
- **Upstream API:** sibling `../core` repo — github.com/pacer/core (private), `mc` app

## Tight coupling with `pacer/core`

**This repo is a thin client around `core`. It has no business logic and no data of its own — every tool is a wrapper over an HTTP endpoint defined in `pacer/core/internal/web/api/`.**

That means:

- **Every new MCP tool needs a matching endpoint in `core`.** If a user asks
  for a tool and the endpoint doesn't exist yet, the work is *primarily* a
  `core` change. The MCP-side wrapper is the easy half.
- **All issue tracking lives in `../core/.issues/`.** This repo has no jig
  config. Whether the work is a `core` API change or a pacer-mcp wrapper,
  file the issue in `../core/.issues/` (synced to ClickUp list
  `901112937048`). Use a `pacer-mcp:` title prefix or tag on wrapper issues
  so they're easy to find.
- **Don't fork response shapes.** Read the handler in
  `core/internal/web/api/<feature>.go` and reuse its DTO field names verbatim
  in the Go types here. If you find yourself reshaping data, that's a sign the
  endpoint itself needs adjustment — push the change into `core`.
- **Don't add request validation, retries, or business rules here that
  belong in `core`.** This binary should be boring: marshal args, hit the
  endpoint, return the JSON. Validation and authorization live behind the PAT
  middleware on the server side.

### Workflow for "add tool X" requests

1. Check `../core/internal/web/api/routes.go` — does an endpoint already cover it?
2. **If no:** `cd ../core && jig todo create ...` to file an API issue describing the desired endpoint contract. *Stop here* on the MCP side until the API ships — there's nothing useful to wrap yet.
3. **If yes:** `cd ../core && jig todo create "pacer-mcp: ..." ...` for the wrapper, implement the tool here, and ship.
4. Mention the jig ID in commit messages on both sides so the ClickUp sync threads them.

### Version compatibility

There is no version negotiation. `pacer-mcp` assumes the `mc` app at
`PACER_CORE_URL` exposes the endpoints it calls. If `core` removes or changes
a route, the corresponding MCP tool will break — fix it by bumping this repo
in lockstep. Don't add compatibility shims; just keep both sides current.

## Architecture

Single Go binary, runs as MCP server via stdio. Claude Code (or any MCP client) launches it as a child process. All state lives upstream in `core` / Postgres — this binary holds nothing across calls except an `http.Client`.

### MCP Tools

| Tool | Description |
|------|-------------|
| `health_check` | Pings the PAT-gated `/api/v1/portfolios/briefable` endpoint to verify URL + token config |
| `guesty_pricing_config` | Per-unit PMS pricing config for a portfolio (base price, fees, min/max nights, channel settings, attached promotion IDs, last-synced timestamp) |
| `guesty_reservation_promotions` | Channel-applied promotions (Airbnb, Vrbo, etc.) for a portfolio's reservations in a given month; `flat=true` returns aggregated per-reservation rows |

More tools will be added as the `core` API surface stabilizes — see the workflow above.

### Configuration

| Env var | Description | Default |
|---------|-------------|---------|
| `PACER_CORE_URL` | Base URL of the `pacer/core` app (the PAT JSON API is mounted on the main `core-prod` Cloud Run service, served at `portal.pacerrev.io`; **not** the Mission Control UI at `mc2.pacerrev.io`, and **not** the legacy Django app at `mc.pacerrev.io`) | `https://portal.pacerrev.io` |
| `PACER_CORE_TOKEN` | Personal access token, format `pat_...` | (unset) |

### Pacer Core API (PAT-gated)

The MCP server talks to the JSON API exposed by `pacer/core`'s `mc` app under
`/api/v1`, authenticated with a personal access token in
`Authorization: Bearer pat_...`. PATs are issued via the core CLI
(`pacer pat create --user <email> --label <name>`) and require the user to
have at least the `employee` role.

Canonical route list: `pacer/core/internal/web/api/routes.go`. As of writing:

- `GET /api/v1/portfolios/briefable`
- `GET /api/v1/portfolios/teams`
- `GET /api/v1/portfolios/{portfolio}/team`
- `GET /api/v1/portfolios/{portfolio}/units`
- `GET /api/v1/portfolios/{portfolio}/reservations`
- `GET /api/v1/portfolios/{portfolio}/pacing`
- `GET /api/v1/portfolios/{portfolio}/metrics/ytd`
- `GET /api/v1/portfolios/{portfolio}/market-metrics`
- `GET /api/v1/portfolios/{portfolio}/pricing-config`
- `GET /api/v1/portfolios/{portfolio}/reservation-promotions`
- `GET /api/v1/portfolios/{portfolio}/client-health-brief`
- `POST /api/v1/portfolios/{portfolio}/client-health-brief`
- `GET /api/v1/client-health/briefs`
- `GET /api/v1/client-health/scoring-config`
- `POST /api/v1/portfolios/{portfolio}/intel-brief`
- `POST /api/v1/portfolios/{portfolio}/intel-brief/attachments` — **not wrapped as an MCP tool.** Multipart binary upload (PNG chart attachments). MCP has no natural multipart story and RMs/agents have no use case for pushing binaries through a chat session. Use the core admin UI or a direct `curl` if needed.
- `GET /api/v1/keydata/managed-units`

Always re-read `routes.go` before adding a tool — this list rots.

### MCP config (end-user)

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

## Dev Guidelines

- Keep it simple — single `main.go` or minimal packages; this is a thin HTTP client
- Stdlib + the MCP SDK; avoid extra deps
- Cross-platform: must build for darwin-arm64, windows-amd64
- Always run `golangci-lint run --fix ./...` after modifying Go code
- Always run `shellcheck` after modifying shell scripts
- Mirror DTO field names from `core/internal/web/api/` exactly — no rename layer
- No client-side caching, no retries beyond what `http.Client` does by default

### Tool documentation principle

End users of this MCP are **revenue managers**, not programmers. They will drive the tools indirectly via AI assistants. They are
fluent in short-term-rental jargon (ADR, RevPAR, occupancy, pacing, ABW, LOS,
YoY same-store, channel mix, etc.) but **do not read API docs, JSON schemas, or
HTTP semantics**.

Every tool's `Description` field (and the server-level `Instructions`) must
therefore be **robust, prose-style documentation** written for the agent acting
on the RM's behalf. Concretely:

- **Lead with the business question the tool answers**, in RM terms — e.g.
  "Use this when the user asks how a portfolio is pacing vs last year," not
  "Wraps GET /portfolios/{p}/pacing."
- **Use STR terminology freely.** Don't dumb it down; RMs and modern agents
  both understand ADR/RevPAR/ABW. Do define quirky internal terms (e.g.
  "same-store" = unit count matched against prior year).
- **Explain key response fields in plain English** with their STR meaning, so
  the agent can interpret the JSON for the RM without guessing.
- **Call out caveats inline** — implicit Guesty discounts, missing data when
  PMS sync is stale, date-type semantics, etc. — so a model that ignores
  `Instructions` still gets the warning at tool-selection time.
- **Avoid Go/HTTP/DB jargon** (no "DTO", "endpoint shape", "404 means…", "the
  handler returns…"). The agent doesn't need to know it's HTTP.
- **Give a concrete example arg set** for anything more complex than a
  portfolio name (date ranges, month strings, sort fields).

If a tool's description is shorter than its response shape is complex, it's
under-documented.

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

## Issue tracking

This repo has no jig config — all issues for pacer-mcp live in
`../core/.issues/` and sync to ClickUp list `901112937048`. Run `jig` from
`../core`. Prefix wrapper issue titles with `pacer-mcp:` to keep them
distinguishable from `core` API issues.
