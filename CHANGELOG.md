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

### 🗜️ Tweaks

- Publish `server.json` to the official MCP Registry
