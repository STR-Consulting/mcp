# Changelog

## Week of Jun 21 – Jun 27, 2026

### ✨ Features

- Add `get_portfolio_billed_units` and `get_billed_units_by_rm` MCP tools wrapping the new core `GET /api/v1/portfolios/{portfolio}/billed-units` and `GET /api/v1/billed-units/by-rm` endpoints. Expose the **authoritative** active billed-unit count — the number Pacer actually invoices, sourced from the most recent Bill.com 'Active/Ongoing' per-unit invoice line (`analytics.billing_actuals`), which matches the billing sheets. The operational `managed_unit_count` diverged from billing in both directions, so RMs had no one-call answer for "how many units do we bill this portfolio / this RM for"; the per-RM variant rolls each active portfolio up to its primary RM (with RD and per-portfolio breakdown) so "units per RM" is a single call. Revenue-share contracts read `billed_units=0` with a note; unmapped portfolios read null (jig ao7-wnr).

## Week of Jun 14 – Jun 20, 2026

### ✨ Features

- Add `create_portfolio_integration` MCP tool wrapping the new core `POST /api/v1/portfolios/{portfolio}/integrations` endpoint. Onboards a portfolio to a PMS/RMS/channel manager it has no row for yet — the gap that previously left handed-over client credentials with nowhere to land. Creates the integration row plus optional encrypted secrets in a single audited write, and refuses two classes of mistake: an exact `platform:purpose` duplicate, and a `purpose` already owned by another enabled integration (pass `enabled=false` to stage a dormant row past the latter). Access gating (admin / portfolio-assigned staff / their supervisor) is enforced in core; the wrapper forwards the caller's PAT (jig 7fo-u6d).

## Week of Jun 7 – Jun 13, 2026

### ✨ Features

- Add `list_portfolio_integrations` MCP tool wrapping the new core `GET /api/v1/portfolios/{portfolio}/integrations` endpoint. Surfaces which PMS, RMS, channel manager, etc. a portfolio is wired to, with both lowercase enum keys (`platform`, `purpose`) and human-readable display names (`platform_name`, `purpose_name`). Encrypted credentials are never returned — only a `has_secrets` boolean (jig wib-22l).
- Add `get_portfolio_integration_secrets` and `set_portfolio_integration_secrets` MCP tools for reading and overwriting the encrypted PMS/RMS credentials Pacer stores for a portfolio. Wraps the new core `GET`/`PUT /api/v1/portfolios/{portfolio}/integrations/{integration}/secrets` endpoints; integration is identified by row id (from `list_portfolio_integrations`) or a `platform:purpose` tuple. Access is admin OR portfolio-assigned staff OR their supervisor — gating is enforced in core; the MCP wrapper just forwards the caller's PAT. Setter is destructive (full overwrite, no versioning); reads are audited to `core.audit_log` and writes ride the existing `audit_portfolio_integrations` trigger (jig ip6-7l9).
- Add `get_pricelabs_notes`, `get_pricelabs_tags`, and `get_pricelabs_overrides` MCP tools wrapping the new core `GET /api/v1/portfolios/{portfolio}/pricelabs/{notes,tags,overrides}` endpoints. Surfaces the PriceLabs-native config — listing notes, group/subgroup labels, source='pricelabs' unit tags, and per-(listing, date) overrides — that the PMS and KeyData sync paths never see. Overrides accept optional `from`/`to` (YYYY-MM-DD) date bounds. Refreshed nightly by the core `mirror-pricelabs` job; each response row carries `fetched_at` so callers can see staleness (jig g23-w2n under epic sz6-wha).

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
