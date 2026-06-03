// Package main implements pacer-mcp, an MCP server that exposes pacer/core
// API endpoints as native Claude Code tools over stdio.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	version         = "0.0.1"
	defaultCoreURL  = "https://portal.pacerrev.io"
	coreURLEnvVar   = "PACER_CORE_URL"
	coreTokenEnvVar = "PACER_CORE_TOKEN"
	httpTimeout     = 30 * time.Second
	// Lightweight PAT-auth-protected endpoint used to verify connectivity.
	healthProbePath = "/api/v1/portfolios/briefable"
)

const serverInstructions = `pacer-mcp exposes the Pacer revenue-management platform's API
(` + "`portal.pacerrev.io`" + `) as a set of tools. End users are short-term-rental
revenue managers driving these tools via an AI assistant; they are fluent in
STR concepts (ADR, RevPAR, occupancy, pacing, ABW, LOS, same-store, channel
mix) but rarely read JSON. Translate results into RM-friendly language.

## What this server can answer

- **Portfolio fundamentals** — which portfolios exist, who's on each team
  (RM / RD / executive), unit roster, reservation history.
- **Performance** — pacing vs prior year, YTD metrics (revenue, ADR,
  occupancy, RevPAR), single-month market benchmarks.
- **Guesty PMS configuration** — per-unit pricing config, channel-applied
  promotions on reservations.
- **Client health** — sentiment briefs (1-5 scale), composite health scores
  with same-store metric backing, scoring config.

## Working with this server

- **Portfolio arguments must come from the user.** Tools accept a portfolio
  name (partial match) or numeric ID. If unclear, call ` + "`list_briefable_portfolios`" + `
  to enumerate, then confirm with the user. Do not invent names.
- **All dates are UTC.** Date args are ` + "`YYYY-MM-DD`" + `; month args are ` + "`YYYY-MM`" + `.
- **Auth is a PAT (` + "`pat_...`" + `) requiring at least the ` + "`employee`" + ` role.** A 401/403
  means the token is missing, expired, or under-privileged — surface to the
  user and ask for a new PAT, do not retry.
- **If anything looks misconfigured, run ` + "`health_check`" + ` first.**

## Terminology cheat-sheet (for translating responses)

- **Same-store** — metrics calculated only over units that existed in BOTH
  the current and prior comparison period. The honest YoY view.
- **Pacing** — a recent reservation's expected revenue and timing vs the
  same booking window last year. A composite ` + "`score`" + ` flags outliers.
- **ABW** — advance booking window (days between booking and check-in).
- **LOS** — length of stay (nights).
- **YTD** — year-to-date through today (or end of selected year).
- **Health brief** — a single per-portfolio sentiment snapshot
  (1=very negative, 5=very positive) with stage and optional notes.
- **Intel brief** — a richer health record tied to a ClickUp task,
  including the composite score, metric backing, and a markdown writeup.

## Guesty PMS data caveats (important)

- **Implicit discounts are invisible to promotions data.** ` + "`weeklyPriceFactor`" + `
  and ` + "`monthlyPriceFactor`" + ` (on ` + "`guesty_pricing_config`" + `) silently reduce
  nightly rates for long stays without producing an invoice line. A
  reservation with no promotion row may still have been discounted. **Do not
  tell the user "no promo applied = full price."**
- **Channel promo SKU IDs are not preserved.** Airbnb sends a descriptive
  ` + "`title`" + ` only (e.g. "Weekly discount"). Treat titles as canonical.
- **Promotion IDs in pricing-config are opaque** — Guesty has no public
  catalog endpoint to resolve them. Don't fabricate names.

## When you don't know

If the user asks for data this server doesn't expose, say so. Don't
approximate with the wrong tool. Available endpoints are exactly the tools
listed — there is nothing else.
`

type server struct {
	coreURL    string
	coreToken  string
	httpClient *http.Client
}

func newServer() *server {
	coreURL := os.Getenv(coreURLEnvVar)
	if coreURL == "" {
		coreURL = defaultCoreURL
	}
	return &server{
		coreURL:    coreURL,
		coreToken:  os.Getenv(coreTokenEnvVar),
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// doGET performs an authenticated GET against the core API and returns the
// raw response body. Non-2xx responses are surfaced as errors with the body
// embedded so the caller can see core's error envelope.
func (s *server) doGET(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	u := s.coreURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if s.coreToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.coreToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("core API %d: %s", resp.StatusCode, string(body))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("core API returned %d with empty body — backend may be down or %s may be pointed at the wrong host (expected portal.pacerrev.io)", resp.StatusCode, s.coreURL)
	}
	return json.RawMessage(body), nil
}

// doPOSTJSON performs an authenticated POST with a JSON body against the
// core API and returns the raw response body. Same error envelope as doGET.
func (s *server) doPOSTJSON(ctx context.Context, path string, body any) (json.RawMessage, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.coreURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.coreToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.coreToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("core API %d: %s", resp.StatusCode, string(respBody))
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil, fmt.Errorf("core API returned %d with empty body — backend may be down or %s may be pointed at the wrong host (expected portal.pacerrev.io)", resp.StatusCode, s.coreURL)
	}
	return json.RawMessage(respBody), nil
}

func portfolioPath(portfolio, suffix string) string {
	return "/api/v1/portfolios/" + url.PathEscape(portfolio) + suffix
}

// toolError builds a tool result carrying the error in both text content
// (for human / fallback display) and structured content (so MCP clients
// that key off structuredContent see the failure instead of silently
// reading the absence of data as "zero rows").
func toolError(err error) (*mcp.CallToolResult, any, error) {
	msg := err.Error()
	return &mcp.CallToolResult{
		IsError: true,
		StructuredContent: map[string]any{
			"error": map[string]any{"message": msg},
		},
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// doGETTool wraps doGET to return the standard tool-result tuple: success
// passes the JSON body through as structured content; failure returns a
// toolError envelope. Collapses the `body, err := ...; if err { toolError(err) }
// return nil, body, nil` boilerplate at every GET-backed handler.
func (s *server) doGETTool(ctx context.Context, path string, query url.Values) (*mcp.CallToolResult, any, error) {
	body, err := s.doGET(ctx, path, query)
	if err != nil {
		return toolError(err)
	}
	return nil, body, nil
}

// doPOSTJSONTool is the POST sibling of doGETTool.
func (s *server) doPOSTJSONTool(ctx context.Context, path string, body any) (*mcp.CallToolResult, any, error) {
	respBody, err := s.doPOSTJSON(ctx, path, body)
	if err != nil {
		return toolError(err)
	}
	return nil, respBody, nil
}

// setOpt writes key=fmt(*v) onto q when v is non-nil. Cleans up the repeated
// `if args.X != nil { q.Set("x", strconv.Format...(*args.X)) }` blocks in
// handlers that forward optional args to core.
func setOpt[T any](q url.Values, key string, v *T, fmt func(T) string) {
	if v != nil {
		q.Set(key, fmt(*v))
	}
}

// ---------- health_check ----------

type healthCheckArgs struct{}

type healthCheckResult struct {
	CoreURL    string `json:"core_url"`
	TokenSet   bool   `json:"token_set"`
	Healthy    bool   `json:"healthy"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"status_code,omitempty"`
	BodyBytes  int    `json:"body_bytes"`
	HostWarn   string `json:"host_warning,omitempty"`
	Error      string `json:"error,omitempty"`
	ServerVer  string `json:"server_version"`
}

// healthCheck probes a real data endpoint and requires a non-empty JSON body
// before reporting healthy. Reports the resolved coreURL and warns when it
// doesn't match the expected production host so misconfig surfaces clearly.
func (s *server) healthCheck(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ healthCheckArgs,
) (*mcp.CallToolResult, healthCheckResult, error) {
	result := healthCheckResult{
		CoreURL:   s.coreURL,
		TokenSet:  s.coreToken != "",
		ServerVer: version,
	}
	if s.coreURL != defaultCoreURL && !strings.HasPrefix(s.coreURL, "http://localhost") && !strings.HasPrefix(s.coreURL, "http://127.0.0.1") {
		result.HostWarn = "PACER_CORE_URL is not " + defaultCoreURL + " — production should resolve there"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.coreURL+healthProbePath, nil)
	if err != nil {
		result.Error = err.Error()
		return nil, result, nil
	}
	if s.coreToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.coreToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		result.Error = err.Error()
		return nil, result, nil
	}
	defer func() { _ = resp.Body.Close() }()

	result.Reachable = resp.StatusCode < 500
	result.StatusCode = resp.StatusCode

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = "read body: " + err.Error()
		return nil, result, nil
	}
	result.BodyBytes = len(body)
	switch {
	case resp.StatusCode >= 400:
		result.Error = fmt.Sprintf("core API %d: %s", resp.StatusCode, truncate(string(body), 200))
	case len(bytes.TrimSpace(body)) == 0:
		result.Error = "core API returned " + strconv.Itoa(resp.StatusCode) + " with empty body — PACER_CORE_URL may be pointed at the wrong host (expected " + defaultCoreURL + ")"
	case !looksLikeJSON(body):
		result.Error = "core API returned " + strconv.Itoa(resp.StatusCode) + " with non-JSON body — likely an edge/proxy response, not the Pacer API"
	default:
		result.Healthy = true
	}
	return nil, result, nil
}

func looksLikeJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return false
	}
	return t[0] == '{' || t[0] == '[' || t[0] == '"'
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------- list_briefable_portfolios ----------

type listBriefablePortfoliosArgs struct {
	Q string `json:"q,omitempty" jsonschema:"optional case-insensitive substring filter on portfolio name OR client name"`
}

func (s *server) listBriefablePortfolios(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args listBriefablePortfoliosArgs,
) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if args.Q != "" {
		q.Set("q", args.Q)
	}
	return s.doGETTool(ctx, "/api/v1/portfolios/briefable", q)
}

// ---------- list_portfolio_teams ----------

type listPortfolioTeamsArgs struct{}

func (s *server) listPortfolioTeams(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ listPortfolioTeamsArgs,
) (*mcp.CallToolResult, any, error) {
	return s.doGETTool(ctx, "/api/v1/portfolios/teams", nil)
}

// ---------- get_portfolio_team ----------

type getPortfolioTeamArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
}

func (s *server) getPortfolioTeam(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args getPortfolioTeamArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/team"), nil)
}

// ---------- list_portfolio_units ----------

type listPortfolioUnitsArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
}

func (s *server) listPortfolioUnits(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args listPortfolioUnitsArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/units"), nil)
}

// ---------- list_portfolio_reservations ----------

type listPortfolioReservationsArgs struct {
	Portfolio     string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Start         string `json:"start" jsonschema:"range start date (YYYY-MM-DD, UTC)"`
	End           string `json:"end" jsonschema:"range end date (YYYY-MM-DD, UTC), inclusive"`
	DateType      string `json:"date_type,omitempty" jsonschema:"which date the range applies to: 'check_in' (default), 'check_out', or 'booked_on'"`
	UnitID        *int64 `json:"unit_id,omitempty" jsonschema:"optional: only reservations for this unit"`
	ConfirmedOnly *bool  `json:"confirmed_only,omitempty" jsonschema:"optional: exclude inquiries / unconfirmed bookings"`
	HasPromo      *bool  `json:"has_promo,omitempty" jsonschema:"optional: only reservations with at least one channel promotion applied"`
	Limit         *int   `json:"limit,omitempty" jsonschema:"page size (default 500, max 5000)"`
	Offset        *int   `json:"offset,omitempty" jsonschema:"row offset for pagination; combine with limit. The response.pagination.has_more flag signals whether to fetch the next page"`
}

func (s *server) listPortfolioReservations(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args listPortfolioReservationsArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	if args.Start == "" || args.End == "" {
		return toolError(errors.New("start and end are required (YYYY-MM-DD)"))
	}
	q := url.Values{}
	q.Set("start", args.Start)
	q.Set("end", args.End)
	if args.DateType != "" {
		q.Set("date_type", args.DateType)
	}
	setOpt(q, "unit_id", args.UnitID, func(v int64) string { return strconv.FormatInt(v, 10) })
	setOpt(q, "confirmed_only", args.ConfirmedOnly, strconv.FormatBool)
	setOpt(q, "has_promo", args.HasPromo, strconv.FormatBool)
	setOpt(q, "limit", args.Limit, strconv.Itoa)
	setOpt(q, "offset", args.Offset, strconv.Itoa)
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/reservations"), q)
}

// ---------- list_portfolio_new_listings ----------

type listPortfolioNewListingsArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Since     string `json:"since,omitempty" jsonschema:"earliest managed_since date to include (YYYY-MM-DD, UTC). Defaults to 90 days ago."`
}

func (s *server) listPortfolioNewListings(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args listPortfolioNewListingsArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	q := url.Values{}
	if args.Since != "" {
		q.Set("since", args.Since)
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/new-listings"), q)
}

// ---------- get_portfolio_pacing ----------

type getPortfolioPacingArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Days      int    `json:"days,omitempty" jsonschema:"lookback window in days (default 7, max 90)"`
	SortBy    string `json:"sort_by,omitempty" jsonschema:"sort field; prefix with '-' for descending. Default '-score' (most-anomalous first). Other useful values: '-rent_yoy', 'booked_on', '-adr_yoy'"`
}

func (s *server) getPortfolioPacing(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args getPortfolioPacingArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	q := url.Values{}
	if args.Days > 0 {
		q.Set("days", strconv.Itoa(args.Days))
	}
	if args.SortBy != "" {
		q.Set("sort_by", args.SortBy)
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/pacing"), q)
}

// ---------- get_portfolio_metrics_ytd ----------

type getPortfolioMetricsYTDArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Year      int    `json:"year,omitempty" jsonschema:"4-digit year (2022-current). Defaults to current UTC year. When set to a past year, returns full-year results vs that year's PY."`
}

func (s *server) getPortfolioMetricsYTD(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args getPortfolioMetricsYTDArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	q := url.Values{}
	if args.Year > 0 {
		q.Set("year", strconv.Itoa(args.Year))
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/metrics/ytd"), q)
}

// ---------- get_portfolio_market_metrics ----------

type getPortfolioMarketMetricsArgs struct {
	Portfolio  string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Month      string `json:"month" jsonschema:"target month in YYYY-MM format, e.g. '2026-05'"`
	Decomposed bool   `json:"decomposed,omitempty" jsonschema:"if true, also break the result down by unit filter set (bedroom buckets, etc.) for benchmark drill-down"`
}

func (s *server) getPortfolioMarketMetrics(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args getPortfolioMarketMetricsArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	if args.Month == "" {
		return toolError(errors.New("month is required (YYYY-MM)"))
	}
	q := url.Values{}
	q.Set("month", args.Month)
	if args.Decomposed {
		q.Set("decomposed", "true")
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/market-metrics"), q)
}

// ---------- guesty_pricing_config ----------

type guestyPricingConfigArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
}

func (s *server) guestyPricingConfig(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args guestyPricingConfigArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/pricing-config"), nil)
}

// ---------- guesty_reservation_promotions ----------

type guestyReservationPromotionsArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Month     string `json:"month,omitempty" jsonschema:"target month in YYYY-MM format (filters by check_in within that month). Pass either month OR start/end."`
	Start     string `json:"start,omitempty" jsonschema:"window start date YYYY-MM-DD; pair with end + optional date_type for arbitrary windows (e.g. promos on bookings made in the last 30 days)"`
	End       string `json:"end,omitempty" jsonschema:"window end date YYYY-MM-DD inclusive"`
	DateType  string `json:"date_type,omitempty" jsonschema:"which reservation date the window applies to: 'check_in' (default), 'check_out', or 'booked_on'"`
	Flat      bool   `json:"flat,omitempty" jsonschema:"if true, return one row per reservation (aggregated). default false returns one row per promo line. Each row carries: is_discount (false for AF/MAR/AFE markups so callers can exclude them from discount totals), booked_on, rent (base rent at time of booking), discount_pct (computed when rent>0 and the row is a discount). Summary rows additionally carry total_discount + total_markup + total_net so markup-vs-discount aggregations are first-class."`
}

func (s *server) guestyReservationPromotions(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args guestyReservationPromotionsArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	if args.Month == "" && (args.Start == "" || args.End == "") {
		return toolError(errors.New("either month=YYYY-MM or start=YYYY-MM-DD&end=YYYY-MM-DD is required"))
	}
	q := url.Values{}
	if args.Month != "" {
		q.Set("month", args.Month)
	}
	if args.Start != "" {
		q.Set("start", args.Start)
	}
	if args.End != "" {
		q.Set("end", args.End)
	}
	if args.DateType != "" {
		q.Set("date_type", args.DateType)
	}
	if args.Flat {
		q.Set("flat", "true")
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/reservation-promotions"), q)
}

// ---------- get_client_health_brief ----------

type getClientHealthBriefArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	Date      string `json:"date,omitempty" jsonschema:"optional YYYY-MM-DD to fetch the brief from a specific date. If omitted, returns the most recent brief."`
}

func (s *server) getClientHealthBrief(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args getClientHealthBriefArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	q := url.Values{}
	if args.Date != "" {
		q.Set("date", args.Date)
	}
	return s.doGETTool(ctx, portfolioPath(args.Portfolio, "/client-health-brief"), q)
}

// ---------- upsert_client_health_brief ----------

type upsertClientHealthBriefArgs struct {
	Portfolio string           `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	BriefDate string           `json:"brief_date" jsonschema:"date the brief applies to (YYYY-MM-DD)"`
	Sentiment int              `json:"sentiment" jsonschema:"sentiment on a 1-5 scale where 1=very negative, 5=very positive"`
	Stage     string           `json:"stage" jsonschema:"client lifecycle stage, e.g. 'Active Client', 'At Risk', 'Churning', 'Won Back'"`
	Payload   *json.RawMessage `json:"payload,omitempty" jsonschema:"optional free-form JSON object stored alongside the brief (notes, links, context)"`
}

type upsertClientHealthBriefBody struct {
	BriefDate string           `json:"brief_date"`
	Sentiment int              `json:"sentiment"`
	Stage     string           `json:"stage"`
	Payload   *json.RawMessage `json:"payload,omitempty"`
}

func (s *server) upsertClientHealthBrief(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args upsertClientHealthBriefArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	if args.BriefDate == "" {
		return toolError(errors.New("brief_date is required (YYYY-MM-DD)"))
	}
	if args.Sentiment < 1 || args.Sentiment > 5 {
		return toolError(errors.New("sentiment must be 1-5"))
	}
	if args.Stage == "" {
		return toolError(errors.New("stage is required"))
	}
	body, err := s.doPOSTJSON(ctx, portfolioPath(args.Portfolio, "/client-health-brief"), upsertClientHealthBriefBody{
		BriefDate: args.BriefDate,
		Sentiment: args.Sentiment,
		Stage:     args.Stage,
		Payload:   args.Payload,
	})
	if err != nil {
		return toolError(err)
	}
	return nil, body, nil
}

// ---------- list_client_health_briefs ----------

type listClientHealthBriefsArgs struct {
	AsOf string `json:"as_of,omitempty" jsonschema:"optional YYYY-MM-DD snapshot date. Defaults to today UTC. Returns the most recent brief per portfolio on or before this date."`
}

func (s *server) listClientHealthBriefs(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args listClientHealthBriefsArgs,
) (*mcp.CallToolResult, any, error) {
	q := url.Values{}
	if args.AsOf != "" {
		q.Set("as_of", args.AsOf)
	}
	return s.doGETTool(ctx, "/api/v1/client-health/briefs", q)
}

// ---------- get_client_health_scoring_config ----------

type getClientHealthScoringConfigArgs struct{}

func (s *server) getClientHealthScoringConfig(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ getClientHealthScoringConfigArgs,
) (*mcp.CallToolResult, any, error) {
	return s.doGETTool(ctx, "/api/v1/client-health/scoring-config", nil)
}

// ---------- upsert_intel_brief ----------

type upsertIntelBriefArgs struct {
	Portfolio       string  `json:"portfolio" jsonschema:"portfolio name (partial match) or numeric ID"`
	BriefDate       string  `json:"brief_date" jsonschema:"date the brief applies to (YYYY-MM-DD)"`
	Stage           string  `json:"stage" jsonschema:"client lifecycle stage, e.g. 'Active Client', 'At Risk'"`
	Sentiment       int     `json:"sentiment" jsonschema:"sentiment on a 1-5 scale"`
	HealthScore     float64 `json:"health_score" jsonschema:"composite client-health score on a 0-10 scale"`
	BriefMarkdown   string  `json:"brief_markdown" jsonschema:"markdown writeup that becomes the ClickUp task body"`
	RevparScore     float64 `json:"revpar_score,omitempty" jsonschema:"sub-score from RevPAR performance (component of health_score)"`
	PaceScore       float64 `json:"pace_score,omitempty" jsonschema:"sub-score from pacing performance (component of health_score)"`
	RunReason       string  `json:"run_reason,omitempty" jsonschema:"why this brief was generated, e.g. 'scheduled-weekly', 'on-demand', 'churn-risk-alert'"`
	SSRevpar        float64 `json:"ss_revpar,omitempty" jsonschema:"same-store RevPAR for the period"`
	SSRevparYoY     float64 `json:"ss_revpar_yoy,omitempty" jsonschema:"same-store RevPAR year-over-year change (decimal: 0.05 = +5%)"`
	SSAdrYoY        float64 `json:"ss_adr_yoy,omitempty" jsonschema:"same-store ADR year-over-year change (decimal)"`
	SSOcc           float64 `json:"ss_occ,omitempty" jsonschema:"same-store occupancy rate (decimal: 0.68 = 68%)"`
	SSOccPP         float64 `json:"ss_occ_pp,omitempty" jsonschema:"same-store occupancy YoY change in percentage points"`
	SSRevenueYoY    float64 `json:"ss_revenue_yoy,omitempty" jsonschema:"same-store revenue YoY change (decimal)"`
	SSFwdRevenueYoY float64 `json:"ss_fwd_revenue_yoy,omitempty" jsonschema:"same-store forward (on-the-books) revenue YoY change (decimal)"`
	SSFwdResYoY     float64 `json:"ss_fwd_res_yoy,omitempty" jsonschema:"same-store forward reservation count YoY change (decimal)"`
	UnitCount       int     `json:"unit_count,omitempty" jsonschema:"total active units in the portfolio at brief time"`
	SameStoreCount  int     `json:"same_store_count,omitempty" jsonschema:"unit count used for the same-store comparison (intersection with PY)"`
	TotalProperties int     `json:"total_properties,omitempty" jsonschema:"total properties (incl. inactive) for context"`
	DataQuality     string  `json:"data_quality,omitempty" jsonschema:"data quality flag: 'good', 'thin' (small sample), or 'invalid' (sync gap). Affects task title rendering."`
}

func (s *server) upsertIntelBrief(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args upsertIntelBriefArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Portfolio == "" {
		return toolError(errors.New("portfolio is required"))
	}
	if args.BriefDate == "" {
		return toolError(errors.New("brief_date is required (YYYY-MM-DD)"))
	}
	if args.Stage == "" {
		return toolError(errors.New("stage is required"))
	}
	if args.Sentiment < 1 || args.Sentiment > 5 {
		return toolError(errors.New("sentiment must be 1-5"))
	}
	if args.BriefMarkdown == "" {
		return toolError(errors.New("brief_markdown is required"))
	}
	// Strip portfolio from the body — it's in the URL.
	type bodyT struct {
		upsertIntelBriefArgs
		Portfolio string `json:"portfolio,omitempty"`
	}
	b := bodyT{upsertIntelBriefArgs: args}
	b.Portfolio = ""
	return s.doPOSTJSONTool(ctx, portfolioPath(args.Portfolio, "/intel-brief"), b)
}

// ---------- list_managed_keydata_units ----------

type listManagedKeydataUnitsArgs struct {
	KDCustomer string `json:"kd_customer" jsonschema:"KeyData customer account UUID. Pacer-managed active units linked to this account are returned."`
}

func (s *server) listManagedKeydataUnits(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args listManagedKeydataUnitsArgs,
) (*mcp.CallToolResult, any, error) {
	if args.KDCustomer == "" {
		return toolError(errors.New("kd_customer is required (KeyData account UUID)"))
	}
	q := url.Values{}
	q.Set("kd_customer", args.KDCustomer)
	return s.doGETTool(ctx, "/api/v1/keydata/managed-units", q)
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}

	s := newServer()

	impl := &mcp.Implementation{Name: "pacer-mcp", Version: version}
	srv := mcp.NewServer(impl, &mcp.ServerOptions{Instructions: serverInstructions})

	registerTools(srv, s)

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// registerTools wires every MCP tool against the server. Extracted from main
// so tests can drive it without a stdio transport.
func registerTools(srv *mcp.Server, s *server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "health_check",
		Description: "Check connectivity to the Pacer API and report config status. " +
			"Run this first if any other tool returns an auth error or if the user " +
			"reports the server isn't working. Returns the configured Pacer URL, " +
			"whether a PAT is set, and whether the API is reachable. A 401 here means " +
			"the PAT is missing or expired — ask the user for a new one rather than retrying.",
	}, s.healthCheck)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_briefable_portfolios",
		Description: "Enumerate active portfolios eligible for client-health briefing " +
			"(those with a KeyData integration set up). Use this as a name lookup when " +
			"the user mentions a portfolio loosely (\"the Smoky Mountain account\") and " +
			"you need the canonical name or ID before calling other tools.\n\n" +
			"USE WHEN: user asks 'which portfolios am I responsible for?', or you need " +
			"to disambiguate a partial name before drilling in.\n\n" +
			"ARGS: q (optional) — case-insensitive substring filter matched against " +
			"BOTH portfolio name and client name.",
	}, s.listBriefablePortfolios)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_portfolio_teams",
		Description: "Bulk endpoint: returns the notification team (assigned RM, RM's " +
			"manager / RD, and Jon) for every active portfolio in one call. Use this " +
			"for org-wide views like 'who covers what?' or 'who reports to me?'. For " +
			"a single portfolio, prefer get_portfolio_team.",
	}, s.listPortfolioTeams)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_portfolio_team",
		Description: "Return the notification team (RM, RM's manager / RD, and Jon) " +
			"for one portfolio. Use this when you need to know who to escalate to or " +
			"who owns an account.\n\n" +
			"ARG: portfolio = name (partial match) or numeric ID.",
	}, s.getPortfolioTeam)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_portfolio_units",
		Description: "Return the full unit roster for a portfolio: bedrooms, " +
			"bathrooms, property type, managed/active status, and location. Use this " +
			"for inventory questions ('how many 3BR cabins do we have at X?'), " +
			"footprint sanity-checks, or before interpreting metric tools that " +
			"depend on unit mix.\n\n" +
			"ARG: portfolio = name (partial match) or numeric ID.",
	}, s.listPortfolioUnits)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_portfolio_reservations",
		Description: "Return reservations for a portfolio in a date range, including " +
			"confirmation status, rent, fees, guest count, cancellation info, plus " +
			"unit city/region inline so location-grouped views don't need a second " +
			"roster call. This is the raw booking data — use it for ad-hoc analysis " +
			"the metric tools don't cover.\n\n" +
			"USE WHEN: user wants a specific list of bookings ('show me cancellations " +
			"last month', 'reservations checking in next week', 'bookings made in May " +
			"for July arrivals').\n\n" +
			"ARGS: portfolio (required), start + end (YYYY-MM-DD, both required), " +
			"date_type = 'check_in' (default — range filters arrival dates), " +
			"'check_out' (departure dates), or 'booked_on' (when the booking was made). " +
			"Pick date_type carefully: 'bookings made in May' = booked_on; 'guests " +
			"staying in May' = check_in.\n\n" +
			"OPTIONAL FILTERS: unit_id (one unit), confirmed_only=true (skip " +
			"unconfirmed / inquiries), has_promo=true (only rows with at least one " +
			"channel promotion applied — pair with guesty_reservation_promotions for " +
			"detail).\n\n" +
			"PAGINATION: limit (default 500, max 5000) + offset. The response includes " +
			"a pagination block with has_more — fetch the next page when has_more=true.",
	}, s.listPortfolioReservations)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_portfolio_new_listings",
		Description: "Track recently-launched units in a portfolio and how they're " +
			"booking out of the gate. Returns one row per unit whose managed_since is " +
			"on or after `since` (default: 90 days ago), with the booking-pace rollup " +
			"needed to manage Airbnb's new-listing promotion (the first 3 Airbnb " +
			"bookings on a new listing are discounted 20% by Airbnb).\n\n" +
			"USE WHEN: user asks 'how are my new listings booking?', 'which new units " +
			"aren't getting traction yet?', 'have we burned the Airbnb new-listing " +
			"promo on X yet?', or 'should I still have the near-term price bump on " +
			"this unit?'. Common workflow: an RM inflates rates further out on a new " +
			"listing to push the first 3 Airbnb bookings to near-term dates, then " +
			"removes the bump once airbnb_promo_remaining hits 0.\n\n" +
			"RESPONSE FIELDS (per unit):\n" +
			"  managed_since — date the unit went under active management. The " +
			"\"new listing\" clock starts here, not at PMS row creation.\n" +
			"  days_active — calendar days since managed_since.\n" +
			"  total_confirmed_bookings — count of confirmed, non-canceled " +
			"reservations on the unit (all channels, all stay dates).\n" +
			"  airbnb_confirmed_bookings — same count filtered to ota='airbnb'. " +
			"This is what counts toward Airbnb's 3-booking promo quota.\n" +
			"  airbnb_promo_remaining — max(0, 3 - airbnb_confirmed_bookings). " +
			"0 means the promo is fully consumed; >0 means the next Airbnb booking " +
			"will still be 20%-discounted by Airbnb.\n" +
			"  first_booked_on / last_booked_on — date range of confirmed bookings. " +
			"null if the unit hasn't booked yet (the case that usually needs " +
			"attention).\n" +
			"  next_check_in — soonest upcoming arrival date, or null if nothing " +
			"on the books.\n\n" +
			"CAVEATS: \"Confirmed\" excludes canceled reservations — Airbnb's promo " +
			"quota is also burned by canceled bookings in reality, but this tool " +
			"reports realized bookings, not gross. Source/channel comes from the " +
			"normalized `ota` column populated by each PMS sync; coverage is good " +
			"for Airbnb across all PMSes Pacer pulls from.\n\n" +
			"ARGS: portfolio (required), since (optional YYYY-MM-DD; defaults to " +
			"90 days ago).",
	}, s.listPortfolioNewListings)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_portfolio_pacing",
		Description: "Pacing view: recent reservations annotated with year-over-year " +
			"comps on rent, ADR, booking window (ABW), and length of stay (LOS), plus " +
			"a composite anomaly score that surfaces outlier bookings. This is the " +
			"workhorse for 'how are we pacing?' conversations.\n\n" +
			"USE WHEN: user asks about recent booking velocity, anomalous bookings, " +
			"or YoY revenue per booking. The default sort (-score) puts the most-" +
			"unusual reservations on top — great for triage.\n\n" +
			"ARGS: portfolio (required), days = lookback window (default 7, max 90), " +
			"sort_by = sort field (default '-score'; other useful values: " +
			"'-rent_yoy', '-adr_yoy', 'booked_on'). Prefix with '-' for descending.",
	}, s.getPortfolioPacing)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_portfolio_metrics_ytd",
		Description: "Year-to-date metrics for a portfolio with current-year vs " +
			"prior-year comps: revenue, ADR, occupancy, RevPAR, LOS, reservation " +
			"count. Includes unit counts and a same-store flag so the agent can tell " +
			"the user whether the YoY comparison is apples-to-apples.\n\n" +
			"USE WHEN: user asks 'how's <portfolio> doing this year?' or wants a " +
			"top-line scorecard. For a historical full year, pass year=2024 etc.\n\n" +
			"ARGS: portfolio (required), year (optional, default current year, range " +
			"2022-current).\n\n" +
			"NOTE ON SAME-STORE: if same_store=false, the unit count changed between " +
			"the periods, so direct YoY % changes can be misleading. Flag this for the user.",
	}, s.getPortfolioMetricsYTD)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_portfolio_market_metrics",
		Description: "One month's CY vs PY pacing for a portfolio (occupancy, ADR, " +
			"RevPAR) PLUS market benchmark deltas, so the user can see whether " +
			"performance is portfolio-specific or following the broader market.\n\n" +
			"USE WHEN: user asks how a month performed vs the comp set, or whether " +
			"a soft month was 'us or the market'. Pass decomposed=true to get " +
			"per-bucket breakdown (by bedroom count, location filter, etc.) for " +
			"benchmark drill-downs.\n\n" +
			"ARGS: portfolio (required), month (YYYY-MM, required), decomposed " +
			"(optional bool).",
	}, s.getPortfolioMarketMetrics)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "guesty_pricing_config",
		Description: "Per-unit Guesty PMS pricing config: base price, cleaning fee, " +
			"weekend pricing, min/max nights, weekly/monthly factors, extra-person " +
			"fee, security deposit, attached promotion IDs (opaque — Guesty exposes " +
			"no catalog endpoint to resolve them), per-channel settings, last-synced " +
			"timestamp.\n\n" +
			"USE WHEN: the question is about what a unit is *configured* to charge — " +
			"pricing intent, fees, restrictions, channel-specific settings.\n\n" +
			"DO NOT USE FOR: actual prices charged or discounts applied to bookings — " +
			"use guesty_reservation_promotions for that.\n\n" +
			"CAVEAT: weeklyPriceFactor and monthlyPriceFactor are implicit discounts " +
			"applied automatically to long stays. They DO NOT produce invoice lines, " +
			"so a reservation with no promotion row may still have been discounted.\n\n" +
			"ARG: portfolio = name (partial match) or numeric ID.",
	}, s.guestyPricingConfig)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "guesty_reservation_promotions",
		Description: "Channel-applied promotions (Airbnb, Vrbo, etc.) on reservations " +
			"in a date window. By default returns one row per (reservation × promo " +
			"line); pass flat=true for one row per reservation with aggregated " +
			"totals.\n\n" +
			"USE WHEN: 'which bookings got which promo?', 'monthly promo discount " +
			"totals', 'export of reservations with promo name if applied', 'promos " +
			"on bookings made in the last 30 days'. Use flat=true for export-style " +
			"monthly summaries.\n\n" +
			"FILTER WINDOW: pass either month=YYYY-MM (filters by check_in within " +
			"that month — the default behavior) OR start=YYYY-MM-DD&end=YYYY-MM-DD " +
			"with optional date_type=check_in|check_out|booked_on for arbitrary " +
			"windows. 'Promos on bookings made in the last 30 days' = " +
			"start/end + date_type=booked_on.\n\n" +
			"PROMO_NORMAL_TYPE TAXONOMY (so you can read the data correctly):\n" +
			"  LOSD = length-of-stay discount (real discount)\n" +
			"  GCD  = generic channel discount (real discount; channel rebates)\n" +
			"  PRO  = PROMOTION (host-configured discount campaign)\n" +
			"  AF   = ACCOMMODATION_FARE (markup, NOT a discount)\n" +
			"  AFE  = ADDITIONAL (markup, NOT a discount)\n" +
			"  MAR  = markup (NOT a discount)\n" +
			"Every row carries is_discount=true only for LOSD/GCD/PRO. Treat AF/AFE/" +
			"MAR rows as price increases — exclude them from discount totals.\n\n" +
			"PER-ROW FIELDS: booked_on (when the booking was made), rent (base rent " +
			"on the reservation at booking time), discount_pct (computed when " +
			"rent>0 and the row is a discount). In flat mode each reservation row " +
			"adds total_discount (sum of LOSD+GCD+PRO amounts), total_markup (sum " +
			"of AF+AFE+MAR amounts), and total_net (the raw view sum — what older " +
			"clients called total_discount; kept for backward compat).\n\n" +
			"CAVEATS: Only channel-sent promos that produced an invoice line are " +
			"returned. Airbnb's internal SKU IDs are not preserved — only the " +
			"descriptive title (e.g. 'Weekly discount', 'New listing promotion'). " +
			"Implicit weekly/monthly factor discounts from pricing-config will NOT " +
			"appear here.\n\n" +
			"ARGS: portfolio (required), month OR start+end (one is required), " +
			"date_type (optional), flat (optional bool).",
	}, s.guestyReservationPromotions)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_client_health_brief",
		Description: "Retrieve the lightweight client-health brief (sentiment 1-5, " +
			"stage, optional payload) for a portfolio. Returns the latest brief by " +
			"default, or the brief from a specific date if provided. Includes trend " +
			"fields: prior sentiment, delta vs prior, and a trending_down flag.\n\n" +
			"USE WHEN: user asks about a single portfolio's current health state or " +
			"how sentiment has moved.\n\n" +
			"ARGS: portfolio (required), date (optional YYYY-MM-DD; omit for latest).",
	}, s.getClientHealthBrief)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "upsert_client_health_brief",
		Description: "Create or update the lightweight client-health brief for a " +
			"portfolio (sentiment 1-5, stage, optional free-form payload). Idempotent " +
			"on (portfolio, brief_date). This is the simple variant — for the " +
			"full intel brief with metric backing and a ClickUp task, use " +
			"upsert_intel_brief instead.\n\n" +
			"USE WHEN: the user wants to log a quick sentiment check-in without all " +
			"the metric scaffolding. Confirm sentiment and stage with the user before " +
			"writing; do not infer them.\n\n" +
			"ARGS: portfolio (required), brief_date (YYYY-MM-DD, required), sentiment " +
			"(1-5 int, required), stage (e.g. 'Active Client', 'At Risk', required), " +
			"payload (optional JSON object for notes/context).",
	}, s.upsertClientHealthBrief)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_client_health_briefs",
		Description: "Dashboard list view: the latest client-health brief per " +
			"portfolio as of a given date. Respects the caller's access scope — " +
			"admins see all, RMs see their own portfolios plus reports.\n\n" +
			"USE WHEN: user wants a portfolio-wide health snapshot ('who's at risk?', " +
			"'show me everyone trending down').\n\n" +
			"ARG: as_of (optional YYYY-MM-DD; defaults to today UTC). Pass a past " +
			"date to reconstruct the historical state.",
	}, s.listClientHealthBriefs)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_client_health_scoring_config",
		Description: "Return the canonical scoring configuration used to compute " +
			"composite health scores: metric labels, weights, and tier thresholds. " +
			"This is static reference data — use it to explain to the user what " +
			"drives a health score, or to validate a manual score calculation.",
	}, s.getClientHealthScoringConfig)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "upsert_intel_brief",
		Description: "Create or update a full intel brief for a portfolio. This is " +
			"the production brief workflow: it persists to Postgres, creates/updates a " +
			"ClickUp task with team assignments, and mirrors to BigQuery for analytics. " +
			"Returns tier, emoji, ClickUp task reference, and assignee list.\n\n" +
			"USE WHEN: the user explicitly wants to publish a real brief that will " +
			"surface to the team in ClickUp. Always confirm sentiment, stage, and the " +
			"markdown writeup with the user before sending — this has side effects " +
			"(tasks created, notifications fired).\n\n" +
			"REQUIRED: portfolio, brief_date (YYYY-MM-DD), stage, sentiment (1-5), " +
			"health_score (0-10), brief_markdown.\n\n" +
			"OPTIONAL METRIC BACKING (recommended when available): revpar_score, " +
			"pace_score (sub-scores), ss_revpar, ss_revpar_yoy, ss_adr_yoy, ss_occ, " +
			"ss_occ_pp, ss_revenue_yoy, ss_fwd_revenue_yoy, ss_fwd_res_yoy (all " +
			"same-store; YoY values are decimals — 0.05 = +5%), unit_count, " +
			"same_store_count, total_properties.\n\n" +
			"data_quality (optional): 'good', 'thin' (small sample — flag in title), " +
			"or 'invalid' (sync gap — title rendered with warning).",
	}, s.upsertIntelBrief)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_managed_keydata_units",
		Description: "Return the set of KeyData property UUIDs corresponding to " +
			"Pacer-managed, active units for the portfolio linked to a given KeyData " +
			"customer account. Primarily used by the KeyData userscript to annotate " +
			"the KeyData UI; rarely needed in a chat session.\n\n" +
			"ARG: kd_customer = KeyData customer account UUID.",
	}, s.listManagedKeydataUnits)
}
