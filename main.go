// Package main implements pacer-mcp, an MCP server that exposes pacer/core
// API endpoints as native Claude Code tools over stdio.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	version         = "0.0.1"
	defaultCoreURL  = "https://mc.pacerrev.io"
	coreURLEnvVar   = "PACER_CORE_URL"
	coreTokenEnvVar = "PACER_CORE_TOKEN"
	httpTimeout     = 30 * time.Second
	// Lightweight PAT-auth-protected endpoint used to verify connectivity.
	healthProbePath = "/api/v1/portfolios/briefable"
)

const serverInstructions = `pacer-mcp is a thin wrapper over the pacer/core HTTP API (` + "`" + `mc.pacerrev.io` + "`" + `).
It holds no data of its own — every tool is a single authenticated GET against
` + "`/api/v1/...`" + ` and returns the response verbatim. All business logic, validation,
and authorization lives upstream in core behind PAT middleware.

## Working with this server

- **Portfolio arguments are required and not guessable.** Tools take a ` + "`portfolio`" + `
  name or ID. If the user hasn't given you one, ask — do not invent names or
  reuse a portfolio from earlier in the conversation unless the user reaffirms it.
- **Authentication is a PAT (` + "`pat_...`" + `) requiring at least the ` + "`employee`" + ` role.**
  A 401 or 403 means the token is missing, expired, or under-privileged — surface
  the error to the user and ask for a new PAT rather than retrying or working around it.
- **If anything looks misconfigured, run ` + "`health_check`" + ` first.** It will tell you
  the configured core URL, whether a token is set, and whether the API is reachable.

## Tool selection

- ` + "`guesty_pricing_config`" + ` answers questions about *intent* — what a unit is
  set up to charge, fees, min/max nights, attached promo IDs, channel settings.
- ` + "`guesty_reservation_promotions`" + ` answers questions about *outcomes* — which
  bookings actually received which channel-sent promo in a given month.
- These are not interchangeable. A question like "did this stay get a discount?"
  almost always wants ` + "`guesty_reservation_promotions`" + `, not the pricing config.

## Guesty data caveats (important — these trip up analysis)

- **Implicit discounts are invisible to promotions data.** Guesty's
  ` + "`weeklyPriceFactor`" + ` and ` + "`monthlyPriceFactor`" + ` (visible on
  ` + "`guesty_pricing_config`" + `) silently reduce nightly rates for long stays
  without producing an invoice line. A reservation with no promotion row in
  ` + "`guesty_reservation_promotions`" + ` may still have been discounted.
  **Do not tell the user "no promo applied = full price."**
- **Channel promo SKU IDs are not preserved.** Airbnb sends a descriptive
  ` + "`title`" + ` (e.g. "Weekly discount", "New listing promotion") but not its
  internal SKU. Treat titles as the canonical identifier; don't try to look them
  up against a catalog (there isn't one).
- **Promotion IDs in pricing-config are opaque.** Guesty does not expose a
  public endpoint to resolve them to names — don't fabricate names.

## When you don't know

If the user asks for data this server doesn't expose, say so. Don't approximate
with the wrong tool. The full route list lives in ` + "`pacer/core/internal/web/api/routes.go`" + `;
new tools must be added there first.
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

type healthCheckArgs struct{}

type healthCheckResult struct {
	CoreURL    string `json:"core_url"`
	TokenSet   bool   `json:"token_set"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	ServerVer  string `json:"server_version"`
}

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
	return nil, result, nil
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
	return json.RawMessage(body), nil
}

type guestyPricingConfigArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name or ID"`
}

func (s *server) guestyPricingConfig(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args guestyPricingConfigArgs,
) (*mcp.CallToolResult, json.RawMessage, error) {
	if args.Portfolio == "" {
		return nil, nil, errors.New("portfolio is required")
	}
	path := "/api/v1/portfolios/" + url.PathEscape(args.Portfolio) + "/pricing-config"
	body, err := s.doGET(ctx, path, nil)
	if err != nil {
		return nil, nil, err
	}
	return nil, body, nil
}

type guestyReservationPromotionsArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name or ID"`
	Month     string `json:"month" jsonschema:"month in YYYY-MM format"`
	Flat      bool   `json:"flat,omitempty" jsonschema:"if true, return one row per reservation (aggregated); default false returns one row per promo line"`
}

func (s *server) guestyReservationPromotions(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args guestyReservationPromotionsArgs,
) (*mcp.CallToolResult, json.RawMessage, error) {
	if args.Portfolio == "" {
		return nil, nil, errors.New("portfolio is required")
	}
	if args.Month == "" {
		return nil, nil, errors.New("month is required (YYYY-MM)")
	}
	q := url.Values{}
	q.Set("month", args.Month)
	if args.Flat {
		q.Set("flat", "true")
	}
	path := "/api/v1/portfolios/" + url.PathEscape(args.Portfolio) + "/reservation-promotions"
	body, err := s.doGET(ctx, path, q)
	if err != nil {
		return nil, nil, err
	}
	return nil, body, nil
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}

	s := newServer()

	impl := &mcp.Implementation{Name: "pacer-mcp", Version: version}
	srv := mcp.NewServer(impl, &mcp.ServerOptions{Instructions: serverInstructions})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "health_check",
		Description: "Check connectivity to the pacer/core API and report config status. " +
			"Run this first if any other tool returns an auth error (401/403) or if the " +
			"user reports the server isn't working. Returns the configured core URL, " +
			"whether a PAT is set, and whether the API is reachable. A 401 here means the " +
			"PAT is missing/expired — ask the user for a new one rather than retrying.",
	}, s.healthCheck)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "guesty_pricing_config",
		Description: "Per-unit PMS pricing config for a portfolio, sourced from Guesty: " +
			"base price, cleaning fee, weekend pricing, min/max nights, weekly/monthly " +
			"factors, extra-person fee, security deposit, attached promotion IDs " +
			"(opaque — Guesty has no public catalog endpoint to resolve them), " +
			"per-channel settings, last-synced timestamp.\n\n" +
			"USE WHEN: the question is about what a unit is *configured* to charge or " +
			"what restrictions/discounts are set up — pricing intent, not actual revenue.\n\n" +
			"DO NOT USE FOR: actual reservation prices or discounts applied (use " +
			"guesty_reservation_promotions instead). Note that weeklyPriceFactor and " +
			"monthlyPriceFactor are implicit discounts — they don't appear as invoice " +
			"lines, so absence of a promo on a long stay does NOT mean full price was " +
			"charged.\n\n" +
			"ARG: portfolio is a portfolio name or ID. If the user hasn't named one, ask " +
			"— do not guess.",
	}, s.guestyPricingConfig)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "guesty_reservation_promotions",
		Description: "Channel-applied promotions (Airbnb, Vrbo, etc.) for a portfolio's " +
			"reservations in a given month. By default returns one row per " +
			"(reservation × promo line); pass flat=true for one row per reservation " +
			"with aggregated promo_titles[] and total_discount.\n\n" +
			"USE WHEN: the question is about which reservations got which channel " +
			"promo, totals of promo discounts per month, or 'promo name if applied' " +
			"per booking. Use flat=true for monthly export-style summaries; default " +
			"(flat=false) for itemized analysis of each promo line.\n\n" +
			"CAVEATS: Only channel-sent promos that produced an invoice line are " +
			"returned. Airbnb's internal promo SKU IDs are not preserved — only the " +
			"descriptive title (e.g. 'Weekly discount', 'New listing promotion'). " +
			"Implicit discounts from guesty_pricing_config's weeklyPriceFactor / " +
			"monthlyPriceFactor will NOT appear here; a reservation with no row may " +
			"still have been discounted via those factors.\n\n" +
			"ARGS: portfolio = name or ID (ask the user if unspecified). month = " +
			"YYYY-MM (e.g. '2026-05'). flat = optional bool.",
	}, s.guestyReservationPromotions)

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
