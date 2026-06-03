# Changelog

## Week of May 31 – Jun 6, 2026

### ✨ Features

- Wrap Guesty pricing-config and reservation-promotions endpoints as `guesty_pricing_config` and `guesty_reservation_promotions` MCP tools
- Expose Guesty pricing and Airbnb promo data as MCP tools; `get_promotion_catalog` dropped (no public Guesty endpoint)
- Add server `Instructions` and richer tool descriptions to guide agents
- Wrap remaining core `/api/v1` endpoints as MCP tools
- Publish as official MCP server installable from Claude and agent tool registries

### 🐞 Fixes

- Fix panic on launch from malformed `list_briefable_portfolios` tool registration
- Point MCPB bundle and `server.json` default `PACER_CORE_URL` at `portal.pacerrev.io`; the previous default pointed at a host that returned empty 200s on every data endpoint
- `health_check` now requires a non-empty JSON body before reporting healthy, flags non-default hosts via `host_warning`, and surfaces `healthy` / `body_bytes` fields so a Cloudflare-style empty 200 can't pose as a working backend
- Surface tool errors via both text content **and** `structuredContent` (`{ error: { message } }`) so MCP clients that key off `structuredContent` see the failure instead of silently reading absent data as "zero rows"

### 🗜️ Tweaks

- Publish `server.json` to the official MCP Registry
- Document linux tarball + OCI install paths in the README (already shipped by goreleaser since v0.6.0)
